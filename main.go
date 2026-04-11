package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	zabbix "github.com/chmller/go-zabbix-sender"
	zabbixapi "github.com/kgeroczi/go-zabbix-api"
	"gopkg.in/yaml.v3"
)

var Version = "1.3.3"

type Config struct {
	NumHosts       int    `yaml:"hosts"`
	HostPrefix     string `yaml:"prefix"`
	NumSenders     int    `yaml:"senders"`
	Rate           int    `yaml:"rate"`
	APIURL         string `yaml:"api_url"`
	User           string `yaml:"user"`
	Pass           string `yaml:"pass"`
	APIKey         string `yaml:"api_key"`
	TrapperAddr    string `yaml:"trapper_addr"`
	GroupName      string `yaml:"group"`
	Duration       time.Duration
	SkipSetup      bool   `yaml:"skip_setup"`
	KeepHosts      bool   `yaml:"keep_hosts"`
	BatchHosts     int    `yaml:"batch_hosts"`
	MaxBatchSize   int    `yaml:"max_batch_size"` // Maximum total metrics per batch
	PoolSize       int    `yaml:"pool_size"`      // TCP connection pool size
	MetricsPerHost int    `yaml:"metrics_per_host"` // Number of metrics to send per host
	OutputJSON     string // output file for JSON results
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
	ID             int   `json:"worker_id"`
	PacketsSent    int64 `json:"packets_sent"`
	HostsSent      int64 `json:"hosts_sent"`
	ErrorCount     int64 `json:"errors"`
	TotalLatencyMs int64 `json:"total_latency_ms"`
	MinLatencyMs   int64 `json:"min_latency_ms"`
	MaxLatencyMs   int64 `json:"max_latency_ms"`
	AvgLatencyMs   int64 `json:"avg_latency_ms"`
}

type BenchmarkResult struct {
	Duration     float64       `json:"duration_seconds"`
	HostsTested  int           `json:"hosts_tested"`
	TotalBatches int64         `json:"total_batches"`
	TotalValues  int64         `json:"total_values"`
	PacketsSent  int64         `json:"packets_sent"`
	ErrorCount   int64         `json:"error_count"`
	ErrorRate    float64       `json:"error_rate_percent"`
	Throughput   float64       `json:"throughput_vps"`
	AvgLatencyMs int64         `json:"avg_latency_ms"`
	MinLatencyMs int64         `json:"min_latency_ms"`
	MaxLatencyMs int64         `json:"max_latency_ms"`
	P50LatencyMs int64         `json:"p50_latency_ms"`
	P95LatencyMs int64         `json:"p95_latency_ms"`
	P99LatencyMs int64         `json:"p99_latency_ms"`
	ErrorsByType ErrorCategory `json:"errors_by_type"`
	WorkerStats  []WorkerStats `json:"worker_stats"`
	Config       any           `json:"config"`
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
	startTime time.Time

	pooledSender *PooledSender

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

func (bm *Benchmarker) printServerHealth() {
	// Try to get a test item to understand server state
	// Just log that we're connected
	log.Printf("Server: %s (connected via API)", bm.cfg.APIURL)
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
			if latencyMs < stats.MinLatencyMs {
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
	return strings.Contains(s, substr)
}

func (bm *Benchmarker) sortLatencies() {
	sort.Slice(bm.latencies, func(i, j int) bool { return bm.latencies[i] < bm.latencies[j] })
}

func (bm *Benchmarker) calculatePercentile(percent float64) int64 {
	if len(bm.latencies) == 0 {
		return 0
	}
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
		NumHosts:       10,
		HostPrefix:     "bench-",
		NumSenders:     10,
		Rate:           0,
		APIURL:         "http://localhost/zabbix/api_jsonrpc.php",
		User:           "Admin",
		Pass:           "",
		TrapperAddr:    "",
		GroupName:      "Benchmark-Group",
		BatchHosts:     50,
		MaxBatchSize:   5000,
		PoolSize:       0,
		MetricsPerHost: 6,
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
	// Defaults
	cfg := Config{
		NumHosts:       10,
		HostPrefix:     "bench-",
		NumSenders:     10,
		Rate:           0,
		APIURL:         "http://localhost/zabbix/api_jsonrpc.php",
		User:           "Admin",
		Pass:           "",
		TrapperAddr:    "",
		GroupName:      "Benchmark-Group",
		BatchHosts:     50,
		MaxBatchSize:   5000,
		PoolSize:       0,
		MetricsPerHost: 6,
	}

	// First pass: just grab -config path
	var cfgFile string
	var outputJSON string
	var showVersion bool
	var showVersionShort bool

	flag.BoolVar(&showVersion, "version", false, "Print release version and exit")
	flag.BoolVar(&showVersionShort, "v", false, "Print release version and exit")
	flag.StringVar(&cfgFile, "config", "", "YAML configuration file")
	flag.StringVar(&outputJSON, "output-json", "", "Output results as JSON to file")
	flag.IntVar(&cfg.NumHosts, "hosts", cfg.NumHosts, "Number of hosts to create")
	flag.StringVar(&cfg.HostPrefix, "prefix", cfg.HostPrefix, "Host prefix")
	flag.IntVar(&cfg.NumSenders, "senders", cfg.NumSenders, "Number of concurrent senders")
	flag.IntVar(&cfg.Rate, "rate", cfg.Rate, "Batches per second per host (0=flood)")
	flag.StringVar(&cfg.APIURL, "api-url", cfg.APIURL, "Zabbix API URL")
	flag.StringVar(&cfg.User, "user", cfg.User, "Zabbix username")
	flag.StringVar(&cfg.Pass, "pass", "", "Zabbix password (default: $ZABBIX_PASS or \"zabbix\")")
	flag.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "Zabbix API token (default: $ZABBIX_API_KEY; skips user.login)")
	flag.StringVar(&cfg.TrapperAddr, "trapper-addr", cfg.TrapperAddr, "Zabbix Trapper address")
	flag.StringVar(&cfg.GroupName, "group", cfg.GroupName, "Host group name")
	flag.DurationVar(&cfg.Duration, "duration", 0, "Test duration, e.g. 30s, 2m (0 = run until Ctrl+C)")
	flag.BoolVar(&cfg.SkipSetup, "skip-setup", cfg.SkipSetup, "Skip host/item creation (use existing hosts with same prefix)")
	flag.BoolVar(&cfg.KeepHosts, "keep-hosts", cfg.KeepHosts, "Keep hosts after test (skip cleanup)")
	flag.IntVar(&cfg.BatchHosts, "batch-hosts", cfg.BatchHosts, "Number of hosts to pack into a single bulk Trapper packet")
	flag.IntVar(&cfg.MaxBatchSize, "batch-metrics", cfg.MaxBatchSize, "Maximum number of metrics per batch packet")
	flag.IntVar(&cfg.PoolSize, "pool-size", cfg.PoolSize, "TCP Connection Pool size (0 = disabled)")
	flag.IntVar(&cfg.MetricsPerHost, "metrics-per-host", cfg.MetricsPerHost, "Number of metrics to send per host")
	flag.Parse()

	if showVersion || showVersionShort {
		fmt.Printf("zabbix-bench version %s\n", Version)
		os.Exit(0)
	}

	// Load config file first, then CLI flags override
	if cfgFile != "" {
		fileCfg, err := loadConfigFile(cfgFile)
		if err != nil {
			log.Fatalf("Error loading config file: %v", err)
		}
		// Apply file values as base, then re-apply any explicitly set CLI flags
		explicitFlags := make(map[string]bool)
		flag.Visit(func(f *flag.Flag) { explicitFlags[f.Name] = true })

		if !explicitFlags["hosts"] {
			cfg.NumHosts = fileCfg.NumHosts
		}
		if !explicitFlags["prefix"] {
			cfg.HostPrefix = fileCfg.HostPrefix
		}
		if !explicitFlags["senders"] {
			cfg.NumSenders = fileCfg.NumSenders
		}
		if !explicitFlags["rate"] {
			cfg.Rate = fileCfg.Rate
		}
		if !explicitFlags["api-url"] {
			cfg.APIURL = fileCfg.APIURL
		}
		if !explicitFlags["user"] {
			cfg.User = fileCfg.User
		}
		if !explicitFlags["pass"] {
			cfg.Pass = fileCfg.Pass
		}
		if !explicitFlags["api-key"] {
			cfg.APIKey = fileCfg.APIKey
		}
		if !explicitFlags["trapper-addr"] {
			cfg.TrapperAddr = fileCfg.TrapperAddr
		}
		if !explicitFlags["group"] {
			cfg.GroupName = fileCfg.GroupName
		}
		if !explicitFlags["skip-setup"] {
			cfg.SkipSetup = fileCfg.SkipSetup
		}
		if !explicitFlags["keep-hosts"] {
			cfg.KeepHosts = fileCfg.KeepHosts
		}
		if !explicitFlags["batch-hosts"] {
			cfg.BatchHosts = fileCfg.BatchHosts
		}
		if !explicitFlags["batch-metrics"] {
			cfg.MaxBatchSize = fileCfg.MaxBatchSize
		}
		if !explicitFlags["pool-size"] {
			cfg.PoolSize = fileCfg.PoolSize
		}
		if !explicitFlags["metrics-per-host"] {
			cfg.MetricsPerHost = fileCfg.MetricsPerHost
		}
	}

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

	// If trapper address not set, derive it from API URL
	if cfg.TrapperAddr == "" {
		apiURL := cfg.APIURL
		if idx := strings.Index(apiURL, "://"); idx >= 0 {
			apiURL = apiURL[idx+3:]
		}
		var host string
		if idx := strings.IndexAny(apiURL, "/:"); idx >= 0 {
			host = apiURL[:idx]
		} else {
			host = apiURL
		}
		if host != "" && host != "localhost" && host != "127.0.0.1" {
			cfg.TrapperAddr = host + ":10051"
			log.Printf("Auto-detected Trapper address from API URL: %s", cfg.TrapperAddr)
		} else {
			cfg.TrapperAddr = "127.0.0.1:10051"
		}
	}

	if cfg.Rate < 0 {
		log.Fatalf("-rate must be >= 0 (0 = flood mode)")
	}
	if cfg.NumSenders <= 0 {
		log.Fatalf("-senders must be > 0")
	}

	bm := &Benchmarker{
		cfg:          cfg,
		pool:         newValuePool(1024),
		done:         make(chan struct{}),
		workerStats:  make(map[int]*WorkerStats),
		latencies:    make([]int64, 0, 100000),
		pooledSender: NewPooledSender(cfg.TrapperAddr, cfg.PoolSize),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		fmt.Println()
		log.Printf("Interrupt received. Stopping benchmark (Ctrl+C again to force quit)...")
		bm.Stop()
	}()

	if cfg.SkipSetup {
		if err := bm.loadExistingHosts(); err != nil {
			log.Printf("Setup failed: %v", err)
			bm.Cleanup()
			os.Exit(1)
		}
	} else {
		if err := bm.Setup(); err != nil {
			log.Printf("Setup failed: %v", err)
			bm.Cleanup()
			os.Exit(1)
		}
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
	bm.PrintSummary(result, cfg.MetricsPerHost)

	if cfg.OutputJSON != "" {
		bm.ExportJSON(result)
	}

	if !cfg.KeepHosts {
		bm.Cleanup()
	}
}

type PooledSender struct {
	addr    string
	pool    chan net.Conn
	timeout time.Duration
}

func NewPooledSender(addr string, poolSize int) *PooledSender {
	var pool chan net.Conn
	if poolSize > 0 {
		pool = make(chan net.Conn, poolSize)
	}
	return &PooledSender{
		addr:    addr,
		pool:    pool,
		timeout: 15 * time.Second,
	}
}

func (s *PooledSender) getConn() (net.Conn, bool, error) {
	if s.pool != nil {
		select {
		case conn := <-s.pool:
			return conn, true, nil
		default:
		}
	}
	conn, err := net.DialTimeout("tcp", s.addr, s.timeout)
	return conn, false, err
}

func (s *PooledSender) putConn(conn net.Conn) {
	if s.pool != nil {
		select {
		case s.pool <- conn:
			return
		default:
		}
	}
	conn.Close()
}

func (s *PooledSender) SendMetrics(metrics []*zabbix.Metric) error {
	packet := zabbix.NewPacket(metrics, false)
	dataPacket, err := json.Marshal(packet)
	if err != nil {
		return fmt.Errorf("marshal packet error: %v", err)
	}

	buffer := append([]byte("ZBXD\x01"), packet.DataLen()...)
	buffer = append(buffer, dataPacket...)

	conn, isPooled, err := s.getConn()
	if err != nil {
		return err
	}

	for {
		if err = conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
			conn.Close()
			return fmt.Errorf("set deadline error: %v", err)
		}

		_, err = conn.Write(buffer)
		if err != nil {
			conn.Close()
			if isPooled {
				// Retry once with a new connection if the pooled one was stale
				conn, isPooled, err = s.getConn()
				if err != nil {
					return err
				}
				isPooled = false // It's definitely new now (or we're loop-breaking)
				continue
			}
			return fmt.Errorf("write error: %v", err)
		}

		header := make([]byte, 13)
		_, err = io.ReadFull(conn, header)
		if err != nil {
			conn.Close()
			if isPooled {
				// Retry once
				conn, isPooled, err = s.getConn()
				if err != nil {
					return err
				}
				isPooled = false
				continue
			}
			return fmt.Errorf("read header error: %v", err)
		}

		if !bytes.Equal(header[:5], []byte("ZBXD\x01")) {
			conn.Close()
			return fmt.Errorf("invalid header")
		}

		dataLen := binary.LittleEndian.Uint64(header[5:13])
		if dataLen > 100*1024*1024 {
			conn.Close()
			return fmt.Errorf("response too large")
		}

		data := make([]byte, dataLen)
		_, err = io.ReadFull(conn, data)
		if err != nil {
			conn.Close()
			return fmt.Errorf("read data error: %v", err)
		}

		s.putConn(conn)
		return nil
	}
}

func (bm *Benchmarker) login() error {
	var err error
	bm.api, err = zabbixapi.NewAPI(zabbixapi.Config{Url: bm.cfg.APIURL})
	if err != nil {
		return fmt.Errorf("error initializing API: %v", err)
	}

	if bm.cfg.APIKey != "" {
		_, err = bm.api.Token(bm.cfg.APIKey)
		if err != nil {
			return fmt.Errorf("error injecting api key: %v", err)
		}
		log.Printf("Using API token for authentication.")
		return nil
	}

	_, err = bm.api.Login(bm.cfg.User, bm.cfg.Pass)
	if err != nil {
		return fmt.Errorf("error logging into Zabbix API: %v", err)
	}
	log.Printf("Logged into Zabbix API (user: %s).", bm.cfg.User)

	// Query and display server health
	bm.printServerHealth()
	return nil
}

func (bm *Benchmarker) Setup() error {
	log.Printf("=== SETUP PHASE ===")
	if err := bm.login(); err != nil {
		return err
	}

	var err error
	bm.groupID, err = bm.ensureHostGroup(bm.cfg.GroupName)
	if err != nil {
		return err
	}
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
	return nil
}

func (bm *Benchmarker) loadExistingHosts() error {
	log.Printf("=== SKIP SETUP: Loading existing hosts with prefix '%s' ===", bm.cfg.HostPrefix)
	if err := bm.login(); err != nil {
		return err
	}

	for i := 0; i < bm.cfg.NumHosts; i++ {
		bm.hostNames = append(bm.hostNames, fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1))
	}
	log.Printf("Loaded %d host names (no API verification).", len(bm.hostNames))
	return nil
}

func (bm *Benchmarker) Run() {
	log.Printf("=== BENCHMARK PHASE ===")
	floodMode := bm.cfg.Rate == 0
	log.Printf("Hosts: %d | Senders: %d | Batch: %d | Flood: %v | Duration: %v",
		len(bm.hostNames), bm.cfg.NumSenders, bm.cfg.BatchHosts, floodMode, bm.cfg.Duration)

	// Initialize per-worker stats
	for i := 0; i < bm.cfg.NumSenders; i++ {
		bm.workerStats[i] = &WorkerStats{ID: i, MinLatencyMs: math.MaxInt64}
	}

	var wg sync.WaitGroup
	hostsPerWorker := (len(bm.hostNames) + bm.cfg.NumSenders - 1) / bm.cfg.NumSenders
	bm.startTime = time.Now()

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
				elapsed := time.Since(bm.startTime).Seconds()
				mph := int64(bm.cfg.MetricsPerHost)
				vps := float64(batches*mph) / elapsed
				intervalVPS := float64((batches-lastBatches)*mph) / 5.0
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
	poolSize := len(bm.pool.bools)
	idx := rand.Intn(poolSize)

	sendBatch := func(hostSlice []string) {
		metricsPerHost := bm.cfg.MetricsPerHost
		if metricsPerHost <= 0 {
			metricsPerHost = 6
		}
		metrics := make([]*zabbix.Metric, 0, len(hostSlice)*metricsPerHost)

		metricTypes := []string{"bool", "unsigned", "float", "text", "char", "log"}

		for _, host := range hostSlice {
			i := int(uint(idx) % uint(poolSize))
			idx++

			// Generate configurable number of metrics per host
			for m := 0; m < metricsPerHost; m++ {
				metricType := metricTypes[m%len(metricTypes)]
				metricKey := fmt.Sprintf("test.metric.%d.%s", m, metricType)

				var value string
				switch metricType {
				case "bool":
					value = bm.pool.bools[i]
				case "unsigned":
					value = bm.pool.uints[i]
				case "float":
					value = bm.pool.floats[i]
				case "text":
					value = fmt.Sprintf("Benchmark text value %d", m)
				case "char":
					value = bm.pool.chars[i]
				case "log":
					value = fmt.Sprintf("Benchmark log entry %d", m)
				}

				metrics = append(metrics, zabbix.NewMetric(host, metricKey, value, false))
			}
		}

		t0 := time.Now()
		var err error

		// Recover from panics in the zabbix sender library
		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("sender panic: %v", r)
				}
			}()
			err = bm.pooledSender.SendMetrics(metrics)
		}()

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
	if bm.cfg.MaxBatchSize > 0 && bm.cfg.MetricsPerHost > 0 {
		hostsFit := bm.cfg.MaxBatchSize / bm.cfg.MetricsPerHost
		if hostsFit > 0 {
			batchSize = hostsFit
		}
	}

	if batchSize <= 0 || batchSize > len(hosts) {
		batchSize = len(hosts)
	}

	sendAll := func() {
		for i := 0; i < len(hosts); i += batchSize {
			if bm.stopped() {
				return
			}
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

	dur := time.Second / time.Duration(bm.cfg.Rate)
	if dur <= 0 {
		dur = time.Microsecond
	}
	ticker := time.NewTicker(dur)
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
	mph := int64(bm.cfg.MetricsPerHost)
	values := batches * mph

	bm.sortLatencies()
	var minLat, maxLat int64
	if len(bm.latencies) > 0 {
		minLat = bm.latencies[0]
		maxLat = bm.latencies[len(bm.latencies)-1]
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
				workerStats = append(workerStats, *stats)
			}
		}
	}
	bm.workerMu.Unlock()

	elapsed := time.Since(bm.startTime).Seconds()
	return BenchmarkResult{
		Duration:     elapsed,
		HostsTested:  len(bm.hostNames),
		TotalBatches: batches,
		TotalValues:  values,
		PacketsSent:  packets,
		ErrorCount:   errs,
		ErrorRate:    errRate,
		Throughput:   float64(values) / elapsed,
		AvgLatencyMs: avgLatency,
		MinLatencyMs: minLat,
		MaxLatencyMs: maxLat,
		P50LatencyMs: bm.calculatePercentile(50),
		P95LatencyMs: bm.calculatePercentile(95),
		P99LatencyMs: bm.calculatePercentile(99),
		ErrorsByType: ErrorCategory{
			Timeout: int(atomic.LoadInt64(&bm.errorTimeout)),
			Closed:  int(atomic.LoadInt64(&bm.errorClosed)),
			Network: int(atomic.LoadInt64(&bm.errorNetwork)),
			Other:   int(atomic.LoadInt64(&bm.errorOther)),
			Total:   int(errs),
		},
		WorkerStats: workerStats,
		Config: map[string]any{
			"hosts":            bm.cfg.NumHosts,
			"senders":          bm.cfg.NumSenders,
			"metrics_per_host": bm.cfg.MetricsPerHost,
			"batch_hosts":      bm.cfg.BatchHosts,
			"rate":             bm.cfg.Rate,
			"trapper_addr":     bm.cfg.TrapperAddr,
		},
	}
}

func (bm *Benchmarker) PrintSummary(result BenchmarkResult, metricsPerHost int) {
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
				workerVPS := float64(ws.HostsSent*int64(metricsPerHost)) / result.Duration
				fmt.Printf("║    Worker %d: %d packets, %d hosts, %d errors, %.0f VPS║\n",
					ws.ID, ws.PacketsSent, ws.HostsSent, ws.ErrorCount, workerVPS)
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

func (bm *Benchmarker) ensureHostGroup(name string) (string, error) {
	groups, err := bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	if err == nil && len(groups) > 0 {
		return groups[0].GroupID, nil
	}
	if err := bm.api.HostGroupsCreate(zabbixapi.HostGroups{{Name: name}}); err != nil {
		return "", fmt.Errorf("failed to create host group: %v", err)
	}
	groups, err = bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	if err != nil || len(groups) == 0 {
		return "", fmt.Errorf("failed to retrieve host group after creation: %v", err)
	}
	return groups[0].GroupID, nil
}

func (bm *Benchmarker) createHostWithItems(hostName string) string {
	var result struct {
		HostIDs []string `json:"hostids"`
	}
	if err := bm.api.CallWithErrorParse("host.create", map[string]interface{}{
		"host":   hostName,
		"name":   hostName,
		"groups": []map[string]string{{"groupid": bm.groupID}},
		"interfaces": []map[string]interface{}{{
			"type": 1, "main": 1, "useip": 1, "ip": "127.0.0.1", "dns": "", "port": "10050",
		}},
	}, &result); err != nil {
		log.Printf("Warning: HostsCreate for %s: %v", hostName, err)
	}

	hosts, err := bm.api.HostsGet(zabbixapi.Params{"filter": map[string]string{"host": hostName}})
	if err != nil || len(hosts) == 0 {
		log.Printf("Could not get hostID for %s", hostName)
		return ""
	}
	hostID := hosts[0].HostID

	// Map metric types to Zabbix value_type (0=float, 1=char, 2=log, 3=numeric, 4=text)
	metricTypeMap := map[string]int{
		"bool":     3,
		"unsigned": 3,
		"float":    0,
		"text":     4,
		"char":     1,
		"log":      2,
	}

	metricTypes := []string{"bool", "unsigned", "float", "text", "char", "log"}

	// Create items matching the metric names generated in sendBatch()
	var items []map[string]any
	for m := 0; m < bm.cfg.MetricsPerHost; m++ {
		metricType := metricTypes[m%len(metricTypes)]
		itemKey := fmt.Sprintf("test.metric.%d.%s", m, metricType)
		itemName := fmt.Sprintf("Metric %d (%s)", m, metricType)

		items = append(items, map[string]any{
			"name":       itemName,
			"key_":       itemKey,
			"hostid":     hostID,
			"type":       2, // Trapper type
			"value_type": metricTypeMap[metricType],
		})
	}

	if len(items) > 0 {
		if err := bm.api.CallWithErrorParse("item.create", items, nil); err != nil {
			log.Printf("Warning: item.create batch on %s: %v", hostName, err)
		}
	}
	return hostID
}
