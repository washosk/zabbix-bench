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

var Version = "1.4.1"

const maxLatencySamples = 1_000_000

type Config struct {
	NumHosts       int           `yaml:"hosts"`
	HostPrefix     string        `yaml:"prefix"`
	NumSenders     int           `yaml:"senders"`
	Rate           int           `yaml:"rate"`
	APIURL         string        `yaml:"api_url"`
	User           string        `yaml:"user"`
	Pass           string        `yaml:"pass"`
	APIKey         string        `yaml:"api_key"`
	TrapperAddr    string        `yaml:"trapper_addr"`
	GroupName      string        `yaml:"group"`
	DurationStr    string        `yaml:"duration"` // e.g. "30s", "2m"
	Duration       time.Duration `yaml:"-"`
	SkipSetup      bool          `yaml:"skip_setup"`
	KeepHosts      bool          `yaml:"keep_hosts"`
	BatchHosts     int           `yaml:"batch_hosts"`
	MaxBatchSize   int           `yaml:"max_batch_size"`   // Maximum total metrics per batch
	MetricsPerHost int           `yaml:"metrics_per_host"` // Number of metrics to send per host
	OutputJSON     string        `yaml:"output_json"`      // output file for JSON results
	DryRun         bool          `yaml:"dry_run"`
	ValidateOnly   bool          `yaml:"validate_only"`
	Profile        string        `yaml:"profile"`
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
	Duration       float64       `json:"duration_seconds"`
	HostsTested    int           `json:"hosts_tested"`
	TotalHostsSent int64         `json:"total_host_sends"`
	TotalValues    int64         `json:"total_values"`
	TotalPackets   int64         `json:"total_packets"`
	ErrorCount     int64         `json:"error_count"`
	ErrorRate      float64       `json:"error_rate_percent"`
	Throughput     float64       `json:"throughput_vps"`
	AvgLatencyMs   int64         `json:"avg_latency_ms"`
	MinLatencyMs   int64         `json:"min_latency_ms"`
	MaxLatencyMs   int64         `json:"max_latency_ms"`
	P50LatencyMs   int64         `json:"p50_latency_ms"`
	P95LatencyMs   int64         `json:"p95_latency_ms"`
	P99LatencyMs   int64         `json:"p99_latency_ms"`
	ErrorsByType   ErrorCategory `json:"errors_by_type"`
	WorkerStats    []WorkerStats `json:"worker_stats"`
	Config         any           `json:"config"`
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

	sender *TrapperSender

	// Per-worker stats (indexed by workerID; per-worker mutex to avoid global contention)
	workerStats []*WorkerStats
	workerMu    []sync.Mutex

	// Global atomic counters
	totalHostsSent int64
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
	if len(bm.latencies) < maxLatencySamples {
		bm.latencies = append(bm.latencies, latencyMs)
	}
	bm.latenciesMu.Unlock()

	if workerID >= 0 && workerID < len(bm.workerStats) {
		bm.workerMu[workerID].Lock()
		stats := bm.workerStats[workerID]
		stats.TotalLatencyMs += latencyMs
		if latencyMs < stats.MinLatencyMs {
			stats.MinLatencyMs = latencyMs
		}
		if latencyMs > stats.MaxLatencyMs {
			stats.MaxLatencyMs = latencyMs
		}
		bm.workerMu[workerID].Unlock()
	}
}

func (bm *Benchmarker) recordError(err error, workerID int) {
	atomic.AddInt64(&bm.totalErrors, 1)

	if workerID >= 0 && workerID < len(bm.workerStats) {
		bm.workerMu[workerID].Lock()
		bm.workerStats[workerID].ErrorCount++
		bm.workerMu[workerID].Unlock()
	}

	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "Timeout"):
		atomic.AddInt64(&bm.errorTimeout, 1)
	case strings.Contains(errStr, "closed") || strings.Contains(errStr, "EOF"):
		atomic.AddInt64(&bm.errorClosed, 1)
	case strings.Contains(errStr, "connection") || strings.Contains(errStr, "network"):
		atomic.AddInt64(&bm.errorNetwork, 1)
	default:
		atomic.AddInt64(&bm.errorOther, 1)
	}
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

func defaultConfig() Config {
	return Config{
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
		MetricsPerHost: 6,
	}
}

func loadConfigFile(path string) (Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	if cfg.DurationStr != "" {
		d, err := time.ParseDuration(cfg.DurationStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid duration %q: %v", cfg.DurationStr, err)
		}
		cfg.Duration = d
	}

	return cfg, nil
}

func applyProfile(cfg *Config, explicitFlags map[string]bool) {
	if cfg.Profile == "" {
		return
	}

	type profileDefaults struct {
		hosts   int
		senders int
		rate    int
	}

	profiles := map[string]profileDefaults{
		"light":    {hosts: 5, senders: 2, rate: 1},
		"balanced": {hosts: 25, senders: 10, rate: 0},
		"flood":    {hosts: 100, senders: 50, rate: 0},
	}

	p, ok := profiles[strings.ToLower(cfg.Profile)]
	if !ok {
		return
	}

	if !explicitFlags["hosts"] {
		cfg.NumHosts = p.hosts
	}
	if !explicitFlags["senders"] {
		cfg.NumSenders = p.senders
	}
	if !explicitFlags["rate"] {
		cfg.Rate = p.rate
	}
}

type ValidationResult struct {
	Warnings []string
	Errors   []string
}

func ValidateConfig(cfg Config) ValidationResult {
	res := ValidationResult{}

	// Numeric sanity checks
	if cfg.NumHosts <= 0 {
		res.Errors = append(res.Errors, "hosts must be > 0")
	}
	if cfg.NumSenders <= 0 {
		res.Errors = append(res.Errors, "senders must be > 0")
	}
	if cfg.MetricsPerHost <= 0 {
		res.Errors = append(res.Errors, "metrics_per_host must be > 0")
	}
	if cfg.BatchHosts <= 0 {
		res.Errors = append(res.Errors, "batch_hosts must be > 0")
	}
	if cfg.MaxBatchSize <= 0 {
		res.Errors = append(res.Errors, "batch_metrics must be > 0")
	}
	if cfg.Rate < 0 {
		res.Errors = append(res.Errors, "rate must be >= 0")
	}

	// Cross-field validation
	if cfg.MaxBatchSize < cfg.MetricsPerHost {
		res.Warnings = append(res.Warnings, fmt.Sprintf("batch_metrics (%d) is smaller than metrics_per_host (%d); effective host batch size will be 1", cfg.MaxBatchSize, cfg.MetricsPerHost))
	}

	if cfg.NumSenders > cfg.NumHosts {
		res.Warnings = append(res.Warnings, fmt.Sprintf("senders (%d) exceeds host count (%d); some workers will be idle", cfg.NumSenders, cfg.NumHosts))
	}

	if cfg.SkipSetup {
		res.Warnings = append(res.Warnings, "skip_setup is enabled; ensure hosts and items already exist with the correct prefix")
	}

	if !cfg.KeepHosts && cfg.GroupName == "Benchmark-Group" {
		res.Warnings = append(res.Warnings, "cleanup is enabled with the default group name 'Benchmark-Group'; this may be risky in shared environments")
	}

	// API URL validation
	if cfg.APIURL == "" {
		res.Errors = append(res.Errors, "api_url is required")
	} else if !strings.HasSuffix(cfg.APIURL, "/api_jsonrpc.php") {
		res.Warnings = append(res.Warnings, "api_url does not end with /api_jsonrpc.php; check if this is correct")
	}

	// Auth validation
	if cfg.APIKey == "" && (cfg.User == "" || cfg.Pass == "") {
		res.Errors = append(res.Errors, "authentication requires either api_key or both user and password")
	}

	// JSON path validation
	if cfg.OutputJSON != "" {
		dir := os.Args[0] // fallback
		if idx := strings.LastIndex(cfg.OutputJSON, "/"); idx >= 0 {
			dir = cfg.OutputJSON[:idx]
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				res.Errors = append(res.Errors, fmt.Sprintf("parent directory for output_json does not exist: %s", dir))
			}
		}
	}

	return res
}

type RuntimePlan struct {
	AuthMode           string
	APIURL             string
	TrapperAddr        string
	GroupName          string
	HostsCount         int
	SendersCount       int
	MetricsPerHost     int
	BatchHosts         int
	BatchMetrics       int
	EffectiveBatchSize int
	Duration           time.Duration
	RateMode           string
	SetupEnabled       bool
	CleanupEnabled     bool
	KeepHosts          bool
	OutputJSON         string
}

func BuildRuntimePlan(cfg Config) *RuntimePlan {
	plan := &RuntimePlan{
		APIURL:         cfg.APIURL,
		GroupName:      cfg.GroupName,
		HostsCount:     cfg.NumHosts,
		SendersCount:   cfg.NumSenders,
		MetricsPerHost: cfg.MetricsPerHost,
		BatchHosts:     cfg.BatchHosts,
		BatchMetrics:   cfg.MaxBatchSize,
		Duration:       cfg.Duration,
		SetupEnabled:   !cfg.SkipSetup,
		CleanupEnabled: !cfg.KeepHosts,
		KeepHosts:      cfg.KeepHosts,
		OutputJSON:     cfg.OutputJSON,
	}

	// Auth Mode
	if cfg.APIKey != "" {
		plan.AuthMode = "API Token"
	} else {
		plan.AuthMode = fmt.Sprintf("User/Pass (user: %s)", cfg.User)
	}

	// Trapper Address
	if cfg.TrapperAddr != "" {
		plan.TrapperAddr = cfg.TrapperAddr
	} else {
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
			plan.TrapperAddr = host + ":10051 (inferred)"
		} else {
			plan.TrapperAddr = "127.0.0.1:10051 (default)"
		}
	}

	// Effective Batch Size
	effectiveBatch := cfg.BatchHosts
	if cfg.MaxBatchSize > 0 {
		hostsFit := cfg.MaxBatchSize / cfg.MetricsPerHost
		if hostsFit > 0 && hostsFit < effectiveBatch {
			effectiveBatch = hostsFit
		}
	}
	plan.EffectiveBatchSize = effectiveBatch

	// Rate Mode
	if cfg.Rate == 0 {
		plan.RateMode = "Flood (as fast as possible)"
	} else {
		plan.RateMode = fmt.Sprintf("Fixed (%d batches/sec per worker)", cfg.Rate)
	}

	return plan
}

func PrintValidationReport(res ValidationResult) {
	if len(res.Warnings) > 0 {
		fmt.Println("⚠️  Configuration Warnings:")
		for _, w := range res.Warnings {
			fmt.Printf("   - %s\n", w)
		}
		fmt.Println()
	}
	if len(res.Errors) > 0 {
		fmt.Println("❌ Validation Errors:")
		for _, e := range res.Errors {
			fmt.Printf("   - %s\n", e)
		}
		fmt.Println()
	}
}

func PrintStartupSummary(mode string, plan *RuntimePlan, warnings int) {
	fmt.Println("╔═════════════════════════════════════════════════════════╗")
	fmt.Printf("║ %-56s║\n", fmt.Sprintf("RUN MODE: %s", strings.ToUpper(mode)))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Auth:    %-47s║\n", plan.AuthMode)
	fmt.Printf("║ API:     %-47s║\n", plan.APIURL)
	fmt.Printf("║ Trapper: %-47s║\n", plan.TrapperAddr)
	fmt.Printf("║ Group:   %-47s║\n", plan.GroupName)
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Hosts:   %-7d | Senders: %-26d║\n", plan.HostsCount, plan.SendersCount)
	fmt.Printf("║ Metrics: %-7d | Batch:   %-26d║\n", plan.MetricsPerHost, plan.BatchHosts)
	fmt.Printf("║ Rate:    %-47s║\n", plan.RateMode)
	fmt.Printf("║ Duration: %-46s║\n", plan.Duration)
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Setup:   %-7v | Cleanup: %-26v║\n", plan.SetupEnabled, plan.CleanupEnabled)
	if plan.OutputJSON != "" {
		fmt.Printf("║ Output:  %-47s║\n", plan.OutputJSON)
	}
	fmt.Printf("║ Warnings: %-46d║\n", warnings)
	fmt.Println("╚═════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func main() {
	cfg := defaultConfig()

	var cfgFile string
	var showVersion bool
	var showVersionShort bool

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of zabbix-bench (version %s):\n\n", Version)
		fmt.Fprintf(os.Stderr, "Example: zabbix-bench -api-url http://zabbix/api_jsonrpc.php -api-key your-token -hosts 50 -duration 1m\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nOperational Modes:\n")
		fmt.Fprintf(os.Stderr, "  -dry-run         Show what would happen without making any changes or sending metrics\n")
		fmt.Fprintf(os.Stderr, "  -validate-only   Perform pre-flight checks (API, Auth, Trapper) and exit\n")
		fmt.Fprintf(os.Stderr, "  -profile str     Pre-set benchmarking profiles: [light, balanced, flood]\n")
		fmt.Fprintf(os.Stderr, "\nNotes:\n")
		fmt.Fprintf(os.Stderr, "  -rate 0          Enables flood mode (send as fast as possible)\n")
		fmt.Fprintf(os.Stderr, "  -skip-setup      Assumes hosts and trapper items already exist with the configured prefix\n")
		fmt.Fprintf(os.Stderr, "  -keep-hosts      Prevents the automated cleanup of created hosts/groups\n")
		fmt.Fprintf(os.Stderr, "  -api-key         If provided, overrides username/password authentication\n")
	}

	flag.BoolVar(&showVersion, "version", false, "Print release version and exit")
	flag.BoolVar(&showVersionShort, "v", false, "Print release version and exit")
	flag.StringVar(&cfgFile, "config", "", "YAML configuration file")
	flag.StringVar(&cfg.OutputJSON, "output-json", "", "Output results as JSON to file")
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
	flag.IntVar(&cfg.MetricsPerHost, "metrics-per-host", cfg.MetricsPerHost, "Number of metrics to send per host")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Show execution plan and exit")
	flag.BoolVar(&cfg.ValidateOnly, "validate-only", false, "Perform pre-flight checks and exit")
	flag.StringVar(&cfg.Profile, "profile", "", "Use a benchmarking profile (light, balanced, flood)")
	flag.Parse()

	if showVersion || showVersionShort {
		fmt.Printf("zabbix-bench version %s\n", Version)
		os.Exit(0)
	}

	// 1. Load config file if provided
	if cfgFile != "" {
		fileCfg, err := loadConfigFile(cfgFile)
		if err != nil {
			log.Fatalf("❌ Error loading config file: %v", err)
		}

		// Track which flags were explicitly set via CLI to handle overrides correctly
		explicit := make(map[string]bool)
		flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

		if !explicit["hosts"] {
			cfg.NumHosts = fileCfg.NumHosts
		}
		if !explicit["prefix"] {
			cfg.HostPrefix = fileCfg.HostPrefix
		}
		if !explicit["senders"] {
			cfg.NumSenders = fileCfg.NumSenders
		}
		if !explicit["rate"] {
			cfg.Rate = fileCfg.Rate
		}
		if !explicit["api-url"] {
			cfg.APIURL = fileCfg.APIURL
		}
		if !explicit["user"] {
			cfg.User = fileCfg.User
		}
		if !explicit["pass"] && fileCfg.Pass != "" {
			cfg.Pass = fileCfg.Pass
		}
		if !explicit["api-key"] && fileCfg.APIKey != "" {
			cfg.APIKey = fileCfg.APIKey
		}
		if !explicit["trapper-addr"] {
			cfg.TrapperAddr = fileCfg.TrapperAddr
		}
		if !explicit["group"] {
			cfg.GroupName = fileCfg.GroupName
		}
		if !explicit["skip-setup"] {
			cfg.SkipSetup = fileCfg.SkipSetup
		}
		if !explicit["keep-hosts"] {
			cfg.KeepHosts = fileCfg.KeepHosts
		}
		if !explicit["batch-hosts"] {
			cfg.BatchHosts = fileCfg.BatchHosts
		}
		if !explicit["batch-metrics"] {
			cfg.MaxBatchSize = fileCfg.MaxBatchSize
		}
		if !explicit["metrics-per-host"] {
			cfg.MetricsPerHost = fileCfg.MetricsPerHost
		}
		if !explicit["duration"] && fileCfg.Duration > 0 {
			cfg.Duration = fileCfg.Duration
		}
		if !explicit["output-json"] && fileCfg.OutputJSON != "" {
			cfg.OutputJSON = fileCfg.OutputJSON
		}
	}

	// 2. Apply Profile defaults to fields not explicitly set
	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	applyProfile(&cfg, explicit)

	// 3. Apply Environment Variables for Auth
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("ZABBIX_API_KEY")
	}
	if cfg.Pass == "" {
		cfg.Pass = os.Getenv("ZABBIX_PASS")
		if cfg.Pass == "" {
			cfg.Pass = "zabbix"
		}
	}

	// 4. Validate
	vRes := ValidateConfig(cfg)
	if len(vRes.Errors) > 0 {
		PrintValidationReport(vRes)
		os.Exit(1)
	}

	// 5. Build Runtime Plan
	plan := BuildRuntimePlan(cfg)

	// 6. Handle Special Modes
	if cfg.DryRun {
		PrintStartupSummary("Dry Run", plan, len(vRes.Warnings))
		PrintValidationReport(vRes)
		os.Exit(0)
	}

	if cfg.ValidateOnly {
		PrintStartupSummary("Validation Only", plan, len(vRes.Warnings))
		PrintValidationReport(vRes)
		bm := &Benchmarker{cfg: cfg, sender: NewTrapperSender(plan.TrapperAddr)}
		fmt.Println("🚀 Performing connectivity checks...")

		if err := bm.login(); err != nil {
			fmt.Printf("❌ API Connectivity/Auth: FAILED - %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ API Connectivity/Auth: SUCCESS")

		// Test trapper connection
		conn, err := net.DialTimeout("tcp", plan.TrapperAddr, 5*time.Second)
		if err != nil {
			fmt.Printf("❌ Trapper Connectivity: FAILED - %v\n", err)
			fmt.Println("   (Check firewall, port, and trapper address)")
			os.Exit(1)
		}
		conn.Close()
		fmt.Println("✅ Trapper Connectivity: SUCCESS")

		fmt.Println("\n✨ Pre-flight checks passed successfully.")
		os.Exit(0)
	}

	// 7. Normal Execution
	PrintStartupSummary("Benchmark", plan, len(vRes.Warnings))

	bm := &Benchmarker{
		cfg:       cfg,
		pool:      newValuePool(1024),
		done:      make(chan struct{}),
		latencies: make([]int64, 0, 100000),
		sender:    NewTrapperSender(plan.TrapperAddr),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-sigChan:
			signal.Reset(os.Interrupt, syscall.SIGTERM)
			fmt.Println()
			log.Printf("Interrupt received. Stopping benchmark (Ctrl+C again to force quit)...")
			bm.Stop()
		case <-bm.done:
		}
	}()

	if cfg.SkipSetup {
		if err := bm.loadExistingHosts(); err != nil {
			log.Printf("❌ Setup failed: %v", err)
			bm.Cleanup()
			os.Exit(1)
		}
	} else {
		if err := bm.Setup(); err != nil {
			log.Printf("❌ Setup failed: %v", err)
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

type TrapperSender struct {
	addr    string
	timeout time.Duration
}

func NewTrapperSender(addr string) *TrapperSender {
	return &TrapperSender{
		addr:    addr,
		timeout: 15 * time.Second,
	}
}

func (s *TrapperSender) SendMetrics(metrics []*zabbix.Metric) error {
	packet := zabbix.NewPacket(metrics, false)
	dataPacket, err := json.Marshal(packet)
	if err != nil {
		return fmt.Errorf("marshal packet error: %v", err)
	}

	buffer := append([]byte("ZBXD\x01"), packet.DataLen()...)
	buffer = append(buffer, dataPacket...)

	conn, err := net.DialTimeout("tcp", s.addr, s.timeout)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if err = conn.SetDeadline(time.Now().Add(s.timeout)); err != nil {
		return fmt.Errorf("set deadline error: %v", err)
	}

	if _, err = conn.Write(buffer); err != nil {
		return fmt.Errorf("write error: %v", err)
	}

	header := make([]byte, 13)
	if _, err = io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("read header error: %v", err)
	}

	if !bytes.Equal(header[:5], []byte("ZBXD\x01")) {
		return fmt.Errorf("invalid header")
	}

	dataLen := binary.LittleEndian.Uint64(header[5:13])
	if dataLen > 100*1024*1024 {
		return fmt.Errorf("response too large")
	}

	data := make([]byte, dataLen)
	if _, err = io.ReadFull(conn, data); err != nil {
		return fmt.Errorf("read data error: %v", err)
	}

	var resp zabbix.Response
	if jsonErr := json.Unmarshal(data, &resp); jsonErr == nil && resp.Response != "success" {
		return fmt.Errorf("zabbix rejected data: %s", resp.Info)
	}

	return nil
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

	// Look up the host group so cleanup can find it later
	groups, err := bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": bm.cfg.GroupName}})
	if err == nil && len(groups) > 0 {
		bm.groupID = groups[0].GroupID
		log.Printf("Found Host Group: %s (ID: %s)", bm.cfg.GroupName, bm.groupID)
	} else {
		log.Printf("Warning: Host Group '%s' not found; cleanup will be skipped.", bm.cfg.GroupName)
	}

	for i := 0; i < bm.cfg.NumHosts; i++ {
		name := fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1)
		bm.hostNames = append(bm.hostNames, name)
	}

	// Look up host IDs for cleanup
	if bm.groupID != "" {
		hosts, err := bm.api.HostsGet(zabbixapi.Params{
			"groupids": []string{bm.groupID},
			"output":   []string{"hostid"},
		})
		if err == nil {
			for _, h := range hosts {
				bm.hostIDs = append(bm.hostIDs, h.HostID)
			}
		}
	}

	log.Printf("Loaded %d host names (%d host IDs from API).", len(bm.hostNames), len(bm.hostIDs))
	return nil
}

func (bm *Benchmarker) Run() {
	log.Printf("=== BENCHMARK PHASE ===")
	floodMode := bm.cfg.Rate == 0
	log.Printf("Hosts: %d | Senders: %d | Batch: %d | Flood: %v | Duration: %v",
		len(bm.hostNames), bm.cfg.NumSenders, bm.cfg.BatchHosts, floodMode, bm.cfg.Duration)

	// Initialize per-worker stats and per-worker mutexes
	bm.workerStats = make([]*WorkerStats, bm.cfg.NumSenders)
	bm.workerMu = make([]sync.Mutex, bm.cfg.NumSenders)
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
		lastHostsSent := int64(0)
		for {
			select {
			case <-bm.done:
				return
			case <-ticker.C:
				hostsSent := atomic.LoadInt64(&bm.totalHostsSent)
				packets := atomic.LoadInt64(&bm.totalPackets)
				errs := atomic.LoadInt64(&bm.totalErrors)
				elapsed := time.Since(bm.startTime).Seconds()
				if elapsed < 0.001 {
					elapsed = 0.001
				}
				mph := int64(bm.cfg.MetricsPerHost)
				vps := float64(hostsSent*mph) / elapsed
				intervalVPS := float64((hostsSent-lastHostsSent)*mph) / 5.0
				lastHostsSent = hostsSent
				errRate := 0.0
				if packets+errs > 0 {
					errRate = float64(errs) / float64(packets+errs) * 100
				}
				log.Printf("[%6.0fs] %8d hosts | %6d pkts | %10.2f VPS (inst: %.2f) | errors: %d (%.1f%%)",
					elapsed, hostsSent, packets, vps, intervalVPS, errs, errRate)
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
			err = bm.sender.SendMetrics(metrics)
		}()

		latency := time.Since(t0).Milliseconds()

		if err == nil {
			atomic.AddInt64(&bm.totalHostsSent, int64(len(hostSlice)))
			atomic.AddInt64(&bm.totalPackets, 1)
			bm.recordLatency(latency, workerID)

			bm.workerMu[workerID].Lock()
			bm.workerStats[workerID].PacketsSent++
			bm.workerStats[workerID].HostsSent += int64(len(hostSlice))
			bm.workerMu[workerID].Unlock()
		} else {
			bm.recordError(err, workerID)
		}
	}

	batchSize := bm.cfg.BatchHosts
	if bm.cfg.MaxBatchSize > 0 {
		hostsFit := bm.cfg.MaxBatchSize / bm.cfg.MetricsPerHost
		if hostsFit > 0 && hostsFit < batchSize {
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

	hostsSent := atomic.LoadInt64(&bm.totalHostsSent)
	packets := atomic.LoadInt64(&bm.totalPackets)
	errs := atomic.LoadInt64(&bm.totalErrors)
	latTotal := atomic.LoadInt64(&bm.totalLatencyMs)
	mph := int64(bm.cfg.MetricsPerHost)
	values := hostsSent * mph

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

	// Collect worker stats — called after wg.Wait(), no concurrent writes possible
	workerStats := make([]WorkerStats, 0, len(bm.workerStats))
	for _, stats := range bm.workerStats {
		if stats != nil && stats.PacketsSent > 0 {
			stats.AvgLatencyMs = stats.TotalLatencyMs / stats.PacketsSent
			workerStats = append(workerStats, *stats)
		}
	}

	elapsed := time.Since(bm.startTime).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	return BenchmarkResult{
		Duration:       elapsed,
		HostsTested:    len(bm.hostNames),
		TotalHostsSent: hostsSent,
		TotalValues:    values,
		TotalPackets:   packets,
		ErrorCount:     errs,
		ErrorRate:      errRate,
		Throughput:     float64(values) / elapsed,
		AvgLatencyMs:   avgLatency,
		MinLatencyMs:   minLat,
		MaxLatencyMs:   maxLat,
		P50LatencyMs:   bm.calculatePercentile(50),
		P95LatencyMs:   bm.calculatePercentile(95),
		P99LatencyMs:   bm.calculatePercentile(99),
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
	// boxLine prints a line that fits exactly inside the 57-char inner width.
	boxLine := func(content string) {
		fmt.Printf("║ %-56s║\n", content)
	}

	fmt.Println()
	fmt.Println("╔═════════════════════════════════════════════════════════╗")
	boxLine("              BENCHMARK SUMMARY REPORT")
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	boxLine(fmt.Sprintf("Hosts tested:        %d", result.HostsTested))
	boxLine(fmt.Sprintf("Total host sends:    %d", result.TotalHostsSent))
	boxLine(fmt.Sprintf("Total values:        %d", result.TotalValues))
	boxLine(fmt.Sprintf("Total packets:       %d", result.TotalPackets))
	boxLine(fmt.Sprintf("Errors:              %d (%.1f%%)", result.ErrorCount, result.ErrorRate))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	boxLine(fmt.Sprintf("Throughput (VPS):    %.2f", result.Throughput))
	boxLine(fmt.Sprintf("Avg latency:         %d ms", result.AvgLatencyMs))
	boxLine(fmt.Sprintf("Min latency:         %d ms", result.MinLatencyMs))
	boxLine(fmt.Sprintf("Max latency:         %d ms", result.MaxLatencyMs))
	boxLine(fmt.Sprintf("P50 latency:         %d ms", result.P50LatencyMs))
	boxLine(fmt.Sprintf("P95 latency:         %d ms", result.P95LatencyMs))
	boxLine(fmt.Sprintf("P99 latency:         %d ms", result.P99LatencyMs))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	if result.ErrorsByType.Total > 0 {
		boxLine("Error breakdown:")
		boxLine(fmt.Sprintf("  Timeout:           %d", result.ErrorsByType.Timeout))
		boxLine(fmt.Sprintf("  Connection closed: %d", result.ErrorsByType.Closed))
		boxLine(fmt.Sprintf("  Network error:     %d", result.ErrorsByType.Network))
		boxLine(fmt.Sprintf("  Other:             %d", result.ErrorsByType.Other))
		fmt.Println("╠═════════════════════════════════════════════════════════╣")
	}

	if len(result.WorkerStats) > 0 {
		boxLine("PARALLEL EXECUTION BREAKDOWN")
		for _, ws := range result.WorkerStats {
			if ws.PacketsSent > 0 {
				workerVPS := float64(ws.HostsSent*int64(metricsPerHost)) / result.Duration
				boxLine(fmt.Sprintf("  Worker #%02d: %d pkts | %d hosts | %d err | %.0f VPS",
					ws.ID, ws.PacketsSent, ws.HostsSent, ws.ErrorCount, workerVPS))
			}
		}
		fmt.Println("╠═════════════════════════════════════════════════════════╣")
	}

	fmt.Println("╚═════════════════════════════════════════════════════════╝")
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
