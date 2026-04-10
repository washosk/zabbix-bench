package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	zabbix "github.com/chmller/go-zabbix-sender"
	zabbixapi "github.com/claranet/go-zabbix-api"
	"gopkg.in/yaml.v3"
)

type Config struct {
	NumHosts    int    `yaml:"hosts"`
	HostPrefix  string `yaml:"prefix"`
	NumSenders  int    `yaml:"senders"`
	Rate        int    `yaml:"rate"`
	APIURL      string `yaml:"api_url"`
	User        string `yaml:"user"`
	Pass        string `yaml:"pass"`
	APIKey      string `yaml:"api_key"`
	TrapperAddr string `yaml:"trapper_addr"`
	GroupName   string `yaml:"group"`
	Duration    time.Duration
	SkipSetup   bool   `yaml:"skip_setup"`
	KeepHosts   bool   `yaml:"keep_hosts"`
	BatchHosts  int    `yaml:"batch_hosts"`
	OutputJSON  string // output file for JSON results
}

type ValuePool struct {
	bools  []string
	uints  []string
	floats []string
	chars  []string
}

func newValuePool(size int) *ValuePool {
	vp := &ValuePool{}
	for i := 0; i < size; i++ {
		vp.bools = append(vp.bools, fmt.Sprintf("%d", rand.Intn(2)))
		vp.uints = append(vp.uints, fmt.Sprintf("%d", rand.Uint64()))
		vp.floats = append(vp.floats, fmt.Sprintf("%.4f", rand.Float64()*100))
		vp.chars = append(vp.chars, string(rune(65+rand.Intn(26))))
	}
	return vp
}

type ErrorCategory struct {
	Timeout int `json:"timeout"`
	Closed  int `json:"closed"`
	Network int `json:"network"`
	Other   int `json:"other"`
	Total   int `json:"total"`
}

type WorkerStats struct {
	ID              int   `json:"worker_id"`
	PacketsSent     int64 `json:"packets_sent"`
	HostsSent       int64 `json:"hosts_sent"`
	ErrorCount      int64 `json:"errors"`
	TotalLatencyMs  int64 `json:"total_latency_ms"`
	MinLatencyMs    int64 `json:"min_latency_ms"`
	MaxLatencyMs    int64 `json:"max_latency_ms"`
	AvgLatencyMs    int64 `json:"avg_latency_ms"`
}

type BenchmarkResult struct {
	Duration           float64        `json:"duration_seconds"`
	HostsTested        int            `json:"hosts_tested"`
	TotalBatches       int64          `json:"total_batches"`
	TotalValues        int64          `json:"total_values"`
	PacketsSent        int64          `json:"packets_sent"`
	ErrorCount         int64          `json:"error_count"`
	ErrorRate          float64        `json:"error_rate_percent"`
	Throughput         float64        `json:"throughput_vps"`
	AvgLatencyMs       int64          `json:"avg_latency_ms"`
	MinLatencyMs       int64          `json:"min_latency_ms"`
	MaxLatencyMs       int64          `json:"max_latency_ms"`
	P50LatencyMs       int64          `json:"p50_latency_ms"`
	P95LatencyMs       int64          `json:"p95_latency_ms"`
	P99LatencyMs       int64          `json:"p99_latency_ms"`
	ErrorsByType       ErrorCategory  `json:"errors_by_type"`
	WorkerStats        []WorkerStats  `json:"worker_stats"`
	Config             any            `json:"config"`
}

type Benchmarker struct {
	cfg       Config
	api       *zabbixapi.API
	hostIDs   []string
	hostNames []string
	groupID   string
	mu        sync.Mutex
	pool      *ValuePool
	done      chan struct{}
	stopOnce  sync.Once

	// Per-worker stats
	workerStats map[int]*WorkerStats
	workerMu    sync.Mutex

	// Global atomic counters
	totalBatches   int64
	totalPackets   int64
	totalErrors    int64
	totalLatencyMs int64

	// Latency tracking for percentiles
	latencies   []int64
	latenciesMu sync.Mutex

	// Error categorization
	errorTimeout int64
	errorClosed  int64
	errorNetwork int64
	errorOther   int64
}

func (bm *Benchmarker) recordLatency(latencyMs int64, workerID int) {
	atomic.AddInt64(&bm.totalLatencyMs, latencyMs)
	bm.latenciesMu.Lock()
	bm.latencies = append(bm.latencies, latencyMs)
	bm.latenciesMu.Unlock()

	if workerID >= 0 {
		bm.workerMu.Lock()
		if stats, ok := bm.workerStats[workerID]; ok {
			stats.TotalLatencyMs += latencyMs
			if latencyMs < stats.MinLatencyMs || stats.MinLatencyMs == 0 {
				stats.MinLatencyMs = latencyMs
			}
			if latencyMs > stats.MaxLatencyMs {
				stats.MaxLatencyMs = latencyMs
			}
		}
		bm.workerMu.Unlock()
	}
}

func (bm *Benchmarker) recordError(err error, workerID int) {
	atomic.AddInt64(&bm.totalErrors, 1)

	if workerID >= 0 {
		bm.workerMu.Lock()
		if stats, ok := bm.workerStats[workerID]; ok {
			stats.ErrorCount++
		}
		bm.workerMu.Unlock()
	}

	errStr := err.Error()
	switch {
	case contains(errStr, "timeout") || contains(errStr, "Timeout"):
		atomic.AddInt64(&bm.errorTimeout, 1)
	case contains(errStr, "closed") || contains(errStr, "EOF"):
		atomic.AddInt64(&bm.errorClosed, 1)
	case contains(errStr, "connection") || contains(errStr, "network"):
		atomic.AddInt64(&bm.errorNetwork, 1)
	default:
		atomic.AddInt64(&bm.errorOther, 1)
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (bm *Benchmarker) calculatePercentile(percent float64) int64 {
	if len(bm.latencies) == 0 {
		return 0
	}
	sort.Slice(bm.latencies, func(i, j int) bool { return bm.latencies[i] < bm.latencies[j] })
	index := int(math.Ceil(float64(len(bm.latencies))*percent/100)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(bm.latencies) {
		index = len(bm.latencies) - 1
	}
	return bm.latencies[index]
}

func (bm *Benchmarker) Stop() {
	bm.stopOnce.Do(func() { close(bm.done) })
}

func (bm *Benchmarker) stopped() bool {
	select {
	case <-bm.done:
		return true
	default:
		return false
	}
}

func loadConfigFile(path string) (Config, error) {
	cfg := Config{
		NumHosts:    10,
		HostPrefix:  "bench-",
		NumSenders:  10,
		Rate:        0,
		APIURL:      "http://localhost/zabbix/api_jsonrpc.php",
		User:        "Admin",
		Pass:        "zabbix",
		TrapperAddr: "127.0.0.1:10051",
		GroupName:   "Benchmark-Group",
		BatchHosts:  50,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	return cfg, nil
}

func main() {
	var cfgFile string
	var outputJSON string

	flag.StringVar(&cfgFile, "config", "", "YAML configuration file")
	flag.StringVar(&outputJSON, "output-json", "", "Output results as JSON to file")

	// Load defaults from file if provided
	var cfg Config
	if cfgFile != "" {
		var err error
		cfg, err = loadConfigFile(cfgFile)
		if err != nil {
			log.Fatalf("Error loading config file: %v", err)
		}
	} else {
		cfg = Config{
			NumHosts:    10,
			HostPrefix:  "bench-",
			NumSenders:  10,
			Rate:        0,
			APIURL:      "http://localhost/zabbix/api_jsonrpc.php",
			User:        "Admin",
			Pass:        "zabbix",
			TrapperAddr: "127.0.0.1:10051",
			GroupName:   "Benchmark-Group",
			BatchHosts:  50,
		}
	}

	// CLI flags override config file
	flag.IntVar(&cfg.NumHosts, "hosts", cfg.NumHosts, "Number of hosts to create")
	flag.StringVar(&cfg.HostPrefix, "prefix", cfg.HostPrefix, "Host prefix")
	flag.IntVar(&cfg.NumSenders, "senders", cfg.NumSenders, "Number of concurrent senders")
	flag.IntVar(&cfg.Rate, "rate", cfg.Rate, "Batches per second per host (0=flood)")
	flag.StringVar(&cfg.APIURL, "api-url", cfg.APIURL, "Zabbix API URL")
	flag.StringVar(&cfg.User, "user", cfg.User, "Zabbix username")
	flag.StringVar(&cfg.Pass, "pass", cfg.Pass, "Zabbix password (default: $ZABBIX_PASS or \"zabbix\")")
	flag.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "Zabbix API token (default: $ZABBIX_API_KEY; skips user.login)")
	flag.StringVar(&cfg.TrapperAddr, "trapper-addr", cfg.TrapperAddr, "Zabbix Trapper address")
	flag.StringVar(&cfg.GroupName, "group", cfg.GroupName, "Host group name")
	flag.DurationVar(&cfg.Duration, "duration", 0, "Test duration, e.g. 30s, 2m (0 = run until Ctrl+C)")
	flag.BoolVar(&cfg.SkipSetup, "skip-setup", cfg.SkipSetup, "Skip host/item creation (use existing hosts with same prefix)")
	flag.BoolVar(&cfg.KeepHosts, "keep-hosts", cfg.KeepHosts, "Keep hosts after test (skip cleanup)")
	flag.IntVar(&cfg.BatchHosts, "batch-hosts", cfg.BatchHosts, "Number of hosts to pack into a single bulk Trapper packet")
	flag.Parse()

	cfg.OutputJSON = outputJSON

	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("ZABBIX_API_KEY")
	}

	if cfg.Pass == "" {
		cfg.Pass = os.Getenv("ZABBIX_PASS")
		if cfg.Pass == "" {
			cfg.Pass = "zabbix"
		}
	}

	if cfg.Rate < 0 {
		log.Fatalf("-rate must be >= 0 (0 = flood mode)")
	}

	bm := &Benchmarker{
		cfg:         cfg,
		pool:        newValuePool(1024),
		done:        make(chan struct{}),
		workerStats: make(map[int]*WorkerStats),
		latencies:   make([]int64, 0, 100000),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println()
		log.Printf("Interrupt received. Stopping benchmark...")
		bm.Stop()
	}()

	if cfg.SkipSetup {
		bm.loadExistingHosts()
	} else {
		bm.Setup()
	}

	if cfg.Duration > 0 {
		go func() {
			select {
			case <-time.After(cfg.Duration):
				log.Printf("Duration %s reached. Stopping...", cfg.Duration)
				bm.Stop()
			case <-bm.done:
			}
		}()
	}

	bm.Run()

	result := bm.GenerateResult()
	bm.PrintSummary(result)

	if cfg.OutputJSON != "" {
		bm.ExportJSON(result)
	}

	if !cfg.KeepHosts {
		bm.Cleanup()
	}
}

func (bm *Benchmarker) login() {
	bm.api = zabbixapi.NewAPI(bm.cfg.APIURL)

	if bm.cfg.APIKey != "" {
		bm.api.Auth = bm.cfg.APIKey
		log.Printf("Using API token for authentication.")
		return
	}

	var token string
	err := bm.api.CallWithErrorParse("user.login", map[string]string{
		"username": bm.cfg.User,
		"password": bm.cfg.Pass,
	}, &token)
	if err != nil {
		log.Fatalf("Error logging into Zabbix API: %v", err)
	}
	bm.api.Auth = token
	log.Printf("Logged into Zabbix API (user: %s).", bm.cfg.User)
}

func (bm *Benchmarker) Setup() {
	log.Printf("=== SETUP PHASE ===")
	bm.login()

	bm.groupID = bm.ensureHostGroup(bm.cfg.GroupName)
	log.Printf("Host Group: %s (ID: %s)", bm.cfg.GroupName, bm.groupID)

	log.Printf("Creating %d hosts in parallel (concurrency=5)...", bm.cfg.NumHosts)
	var wg sync.WaitGroup
	throttle := make(chan struct{}, 5)

	for i := 0; i < bm.cfg.NumHosts; i++ {
		hostName := fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1)
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			throttle <- struct{}{}
			id := bm.createHostWithItems(name)
			if id != "" {
				bm.mu.Lock()
				bm.hostIDs = append(bm.hostIDs, id)
				bm.hostNames = append(bm.hostNames, name)
				bm.mu.Unlock()
			}
			<-throttle
		}(hostName)
	}
	wg.Wait()
	log.Printf("Setup complete. %d/%d hosts ready.", len(bm.hostIDs), bm.cfg.NumHosts)
}

func (bm *Benchmarker) loadExistingHosts() {
	log.Printf("=== SKIP SETUP: Loading existing hosts with prefix '%s' ===", bm.cfg.HostPrefix)
	bm.login()

	for i := 0; i < bm.cfg.NumHosts; i++ {
		bm.hostNames = append(bm.hostNames, fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1))
	}
	log.Printf("Loaded %d host names (no API verification).", len(bm.hostNames))
}

func (bm *Benchmarker) Run() {
	log.Printf("=== BENCHMARK PHASE ===")
	floodMode := bm.cfg.Rate == 0
	log.Printf("Hosts: %d | Senders: %d | Batch: %d | Flood: %v | Duration: %v",
		len(bm.hostNames), bm.cfg.NumSenders, bm.cfg.BatchHosts, floodMode, bm.cfg.Duration)

	// Initialize per-worker stats
	for i := 0; i < bm.cfg.NumSenders; i++ {
		bm.workerStats[i] = &WorkerStats{ID: i}
	}

	var wg sync.WaitGroup
	hostsPerWorker := (len(bm.hostNames) + bm.cfg.NumSenders - 1) / bm.cfg.NumSenders
	startTime := time.Now()

	for i := 0; i < bm.cfg.NumSenders; i++ {
		start := i * hostsPerWorker
		end := (i + 1) * hostsPerWorker
		if start >= len(bm.hostNames) {
			break
		}
		if end > len(bm.hostNames) {
			end = len(bm.hostNames)
		}
		wg.Add(1)
		go func(workerID int, hosts []string) {
			defer wg.Done()
			bm.worker(workerID, hosts)
		}(i, bm.hostNames[start:end])
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		lastBatches := int64(0)
		for {
			select {
			case <-bm.done:
				return
			case <-ticker.C:
				batches := atomic.LoadInt64(&bm.totalBatches)
				packets := atomic.LoadInt64(&bm.totalPackets)
				errs := atomic.LoadInt64(&bm.totalErrors)
				elapsed := time.Since(startTime).Seconds()
				vps := float64(batches*6) / elapsed
				intervalVPS := float64((batches-lastBatches)*6) / 5.0
				lastBatches = batches
				errRate := 0.0
				if packets+errs > 0 {
					errRate = float64(errs) / float64(packets+errs) * 100
				}
				log.Printf("[%6.0fs] %8d batches | %10.2f VPS (inst: %.2f) | errors: %d (%.1f%%)",
					elapsed, batches, vps, intervalVPS, errs, errRate)
			}
		}
	}()

	wg.Wait()
}

func (bm *Benchmarker) worker(workerID int, hosts []string) {
	sender := zabbix.NewSender(bm.cfg.TrapperAddr)
	poolSize := len(bm.pool.bools)
	idx := rand.Intn(poolSize)

	sendBatch := func(hostSlice []string) {
		metrics := make([]*zabbix.Metric, 0, len(hostSlice)*6)
		for _, host := range hostSlice {
			i := idx % poolSize
			idx++
			metrics = append(metrics,
				zabbix.NewMetric(host, "test.bool", bm.pool.bools[i], false),
				zabbix.NewMetric(host, "test.unsigned", bm.pool.uints[i], false),
				zabbix.NewMetric(host, "test.float", bm.pool.floats[i], false),
				zabbix.NewMetric(host, "test.text", "Benchmark text value", false),
				zabbix.NewMetric(host, "test.char", bm.pool.chars[i], false),
				zabbix.NewMetric(host, "test.log", "Benchmark log entry", false),
			)
		}

		t0 := time.Now()
		_, _, _, err := sender.SendMetrics(metrics)
		latency := time.Since(t0).Milliseconds()

		if err == nil {
			atomic.AddInt64(&bm.totalBatches, int64(len(hostSlice)))
			atomic.AddInt64(&bm.totalPackets, 1)
			bm.recordLatency(latency, workerID)

			bm.workerMu.Lock()
			if stats, ok := bm.workerStats[workerID]; ok {
				stats.PacketsSent++
				stats.HostsSent += int64(len(hostSlice))
			}
			bm.workerMu.Unlock()
		} else {
			bm.recordError(err, workerID)
		}
	}

	batchSize := bm.cfg.BatchHosts
	if batchSize <= 0 || batchSize > len(hosts) {
		batchSize = len(hosts)
	}

	sendAll := func() {
		for i := 0; i < len(hosts); i += batchSize {
			end := i + batchSize
			if end > len(hosts) {
				end = len(hosts)
			}
			sendBatch(hosts[i:end])
		}
	}

	if bm.cfg.Rate == 0 {
		for !bm.stopped() {
			sendAll()
		}
		return
	}

	ticker := time.NewTicker(time.Duration(1000/bm.cfg.Rate) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-bm.done:
			return
		case <-ticker.C:
			sendAll()
		}
	}
}

func (bm *Benchmarker) GenerateResult() BenchmarkResult {
	bm.latenciesMu.Lock()
	defer bm.latenciesMu.Unlock()

	batches := atomic.LoadInt64(&bm.totalBatches)
	packets := atomic.LoadInt64(&bm.totalPackets)
	errs := atomic.LoadInt64(&bm.totalErrors)
	latTotal := atomic.LoadInt64(&bm.totalLatencyMs)
	values := batches * 6

	var minLat, maxLat int64
	if len(bm.latencies) > 0 {
		minLat = bm.latencies[0]
		maxLat = bm.latencies[0]
		for _, lat := range bm.latencies {
			if lat < minLat {
				minLat = lat
			}
			if lat > maxLat {
				maxLat = lat
			}
		}
	}

	avgLatency := int64(0)
	if packets > 0 {
		avgLatency = latTotal / packets
	}

	errRate := 0.0
	if packets+errs > 0 {
		errRate = float64(errs) / float64(packets+errs) * 100
	}

	// Collect worker stats
	bm.workerMu.Lock()
	workerStats := make([]WorkerStats, 0, len(bm.workerStats))
	for i := 0; i < len(bm.workerStats); i++ {
		if stats, ok := bm.workerStats[i]; ok {
			if stats.PacketsSent > 0 {
				stats.AvgLatencyMs = stats.TotalLatencyMs / stats.PacketsSent
			}
			workerStats = append(workerStats, *stats)
		}
	}
	bm.workerMu.Unlock()

	return BenchmarkResult{
		Duration:      time.Since(time.Time{}).Seconds(), // Will be set by PrintSummary
		HostsTested:   len(bm.hostNames),
		TotalBatches:  batches,
		TotalValues:   values,
		PacketsSent:   packets,
		ErrorCount:    errs,
		ErrorRate:     errRate,
		Throughput:    float64(values) / time.Since(time.Time{}).Seconds(),
		AvgLatencyMs:  avgLatency,
		MinLatencyMs:  minLat,
		MaxLatencyMs:  maxLat,
		P50LatencyMs:  bm.calculatePercentile(50),
		P95LatencyMs:  bm.calculatePercentile(95),
		P99LatencyMs:  bm.calculatePercentile(99),
		ErrorsByType: ErrorCategory{
			Timeout: int(atomic.LoadInt64(&bm.errorTimeout)),
			Closed:  int(atomic.LoadInt64(&bm.errorClosed)),
			Network: int(atomic.LoadInt64(&bm.errorNetwork)),
			Other:   int(atomic.LoadInt64(&bm.errorOther)),
			Total:   int(errs),
		},
		WorkerStats: workerStats,
	}
}

func (bm *Benchmarker) PrintSummary(result BenchmarkResult) {
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Println("║           BENCHMARK SUMMARY REPORT                    ║")
	fmt.Println("╠════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Hosts tested:        %-35d║\n", result.HostsTested)
	fmt.Printf("║  Total batches:       %-35d║\n", result.TotalBatches)
	fmt.Printf("║  Total values:        %-35d║\n", result.TotalValues)
	fmt.Printf("║  Packets sent:        %-35d║\n", result.PacketsSent)
	fmt.Printf("║  Errors:              %-18d(%.1f%%)║\n", result.ErrorCount, result.ErrorRate)
	fmt.Println("╠════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Throughput (VPS):    %-35.2f║\n", result.Throughput)
	fmt.Printf("║  Avg latency:         %-30dms║\n", result.AvgLatencyMs)
	fmt.Printf("║  Min latency:         %-30dms║\n", result.MinLatencyMs)
	fmt.Printf("║  Max latency:         %-30dms║\n", result.MaxLatencyMs)
	fmt.Printf("║  P50 latency:         %-30dms║\n", result.P50LatencyMs)
	fmt.Printf("║  P95 latency:         %-30dms║\n", result.P95LatencyMs)
	fmt.Printf("║  P99 latency:         %-30dms║\n", result.P99LatencyMs)
	fmt.Println("╠════════════════════════════════════════════════════════╣")
	if result.ErrorsByType.Total > 0 {
		fmt.Printf("║  Error breakdown:                                      ║\n")
		fmt.Printf("║    Timeout:           %-35d║\n", result.ErrorsByType.Timeout)
		fmt.Printf("║    Connection closed: %-35d║\n", result.ErrorsByType.Closed)
		fmt.Printf("║    Network error:     %-35d║\n", result.ErrorsByType.Network)
		fmt.Printf("║    Other:             %-35d║\n", result.ErrorsByType.Other)
		fmt.Println("╠════════════════════════════════════════════════════════╣")
	}

	if len(result.WorkerStats) > 0 {
		fmt.Println("║  Per-worker statistics:                                ║")
		for _, ws := range result.WorkerStats {
			if ws.PacketsSent > 0 {
				fmt.Printf("║    Worker %d: %d packets, %d hosts, %d errors, %.0f VPS║\n",
					ws.ID, ws.PacketsSent, ws.HostsSent, ws.ErrorCount,
					float64(ws.HostsSent*6)/float64(ws.PacketsSent*5)) // rough VPS estimate
			}
		}
		fmt.Println("╠════════════════════════════════════════════════════════╣")
	}

	fmt.Println("╚════════════════════════════════════════════════════════╝")
}

func (bm *Benchmarker) ExportJSON(result BenchmarkResult) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return
	}

	if err := os.WriteFile(bm.cfg.OutputJSON, data, 0644); err != nil {
		log.Printf("Error writing JSON file: %v", err)
		return
	}

	log.Printf("Results exported to %s", bm.cfg.OutputJSON)
}

func (bm *Benchmarker) Cleanup() {
	if bm.api == nil {
		return
	}
	log.Printf("=== CLEANUP PHASE ===")

	if bm.groupID != "" {
		hosts, err := bm.api.HostsGet(zabbixapi.Params{
			"groupids": []string{bm.groupID},
			"output":   []string{"hostid"},
		})
		if err == nil && len(hosts) > 0 {
			allIDs := make([]string, len(hosts))
			for i, h := range hosts {
				allIDs[i] = h.HostID
			}
			log.Printf("Deleting %d hosts in group (queried from Zabbix)...", len(allIDs))
			batchSize := 50
			for i := 0; i < len(allIDs); i += batchSize {
				end := i + batchSize
				if end > len(allIDs) {
					end = len(allIDs)
				}
				if err := bm.api.HostsDeleteByIds(allIDs[i:end]); err != nil {
					log.Printf("Error deleting hosts: %v", err)
				}
			}
		} else if len(bm.hostIDs) > 0 {
			log.Printf("Falling back: deleting %d tracked hosts...", len(bm.hostIDs))
			batchSize := 50
			for i := 0; i < len(bm.hostIDs); i += batchSize {
				end := i + batchSize
				if end > len(bm.hostIDs) {
					end = len(bm.hostIDs)
				}
				_ = bm.api.HostsDeleteByIds(bm.hostIDs[i:end])
			}
		}

		log.Printf("Deleting Host Group '%s'...", bm.cfg.GroupName)
		if err := bm.api.HostGroupsDeleteByIds([]string{bm.groupID}); err != nil {
			log.Printf("Error deleting group: %v", err)
		}
	}
	log.Printf("Cleanup complete.")
}

func (bm *Benchmarker) ensureHostGroup(name string) string {
	groups, err := bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	if err == nil && len(groups) > 0 {
		return groups[0].GroupID
	}
	if err := bm.api.HostGroupsCreate(zabbixapi.HostGroups{{Name: name}}); err != nil {
		log.Fatalf("Failed to create host group: %v", err)
	}
	groups, _ = bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	return groups[0].GroupID
}

func (bm *Benchmarker) createHostWithItems(hostName string) string {
	if err := bm.api.HostsCreate(zabbixapi.Hosts{{
		Host:     hostName,
		GroupIds: zabbixapi.HostGroupIDs{{GroupID: bm.groupID}},
		Interfaces: zabbixapi.HostInterfaces{{
			Type: 1, Main: 1, UseIP: 1, IP: "127.0.0.1", Port: "10050",
		}},
	}}); err != nil {
		log.Printf("Warning: HostsCreate for %s: %v", hostName, err)
	}

	hosts, err := bm.api.HostsGet(zabbixapi.Params{"filter": map[string]string{"host": hostName}})
	if err != nil || len(hosts) == 0 {
		log.Printf("Could not get hostID for %s", hostName)
		return ""
	}
	hostID := hosts[0].HostID

	for _, it := range []struct {
		key, name string
		vtype     int
	}{
		{"test.bool", "Boolean", 3},
		{"test.unsigned", "Unsigned", 3},
		{"test.float", "Float", 0},
		{"test.text", "Text", 4},
		{"test.char", "Character", 1},
		{"test.log", "Log", 2},
	} {
		if err := bm.api.CallWithErrorParse("item.create", map[string]any{
			"name": it.name, "key_": it.key, "hostid": hostID,
			"type": 2, "value_type": it.vtype,
		}, nil); err != nil {
			log.Printf("Warning: item.create %s on %s: %v", it.key, hostName, err)
		}
	}
	return hostID
}
