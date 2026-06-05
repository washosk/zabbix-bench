package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/washosk/zabbix-bench/internal/benchmark"
	"github.com/washosk/zabbix-bench/internal/config"
	"github.com/washosk/zabbix-bench/internal/zabbix"
)

var Version = "2.0.0"

func PrintStartupSummary(mode string, plan *config.RuntimePlan, warnings int) {
	durationLabel := plan.Duration.String()
	if plan.Duration == 0 {
		durationLabel = "until interrupted"
	}

	fmt.Println("╔═════════════════════════════════════════════════════════╗")
	fmt.Printf("║ %-56s║\n", fmt.Sprintf("RUN MODE: %s", mode))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Auth:    %-47s║\n", plan.AuthMode)
	fmt.Printf("║ API:     %-47s║\n", plan.APIURL)
	fmt.Printf("║ Trapper: %-47s║\n", plan.TrapperAddrLabel)
	fmt.Printf("║ Group:   %-47s║\n", plan.GroupName)
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Hosts:   %-7d | Senders: %-26d║\n", plan.HostsCount, plan.SendersCount)
	fmt.Printf("║ Metrics: %-7d | Batch:   %-26d║\n", plan.MetricsPerHost, plan.EffectiveBatchSize)
	fmt.Printf("║ Rate:    %-47s║\n", plan.RateMode)
	fmt.Printf("║ Duration: %-46s║\n", durationLabel)
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	fmt.Printf("║ Setup:   %-7v | Cleanup: %-26v║\n", plan.SetupEnabled, plan.CleanupEnabled)
	if plan.OutputJSON != "" {
		fmt.Printf("║ Output:  %-47s║\n", plan.OutputJSON)
	}
	fmt.Printf("║ Warnings: %-46d║\n", warnings)
	fmt.Println("╚═════════════════════════════════════════════════════════╝")
	fmt.Println()
}

func PrintValidationReport(res config.ValidationResult) {
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

func main() {
	cfg := config.DefaultConfig()

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
		fmt.Fprintf(os.Stderr, "  -keep-hosts      Prevents cleanup of hosts created by this run\n")
		fmt.Fprintf(os.Stderr, "  -api-key         If provided, overrides username/password authentication\n")
	}

	flag.BoolVar(&showVersion, "version", false, "Print release version and exit")
	flag.BoolVar(&showVersionShort, "v", false, "Print release version and exit")
	flag.StringVar(&cfgFile, "config", "", "YAML configuration file")
	flag.StringVar(&cfg.OutputJSON, "output-json", "", "Output results as JSON to file")
	flag.IntVar(&cfg.NumHosts, "hosts", cfg.NumHosts, "Number of hosts to create")
	flag.StringVar(&cfg.HostPrefix, "prefix", cfg.HostPrefix, "Host prefix")
	flag.IntVar(&cfg.NumSenders, "senders", cfg.NumSenders, "Number of concurrent senders")
	flag.IntVar(&cfg.Rate, "rate", cfg.Rate, "Packets per second per worker (0=flood)")
	flag.StringVar(&cfg.APIURL, "api-url", cfg.APIURL, "Zabbix API URL")
	flag.StringVar(&cfg.User, "user", cfg.User, "Zabbix username")
	flag.StringVar(&cfg.Pass, "pass", "", "Zabbix password (default: $ZABBIX_PASS)")
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

	if cfgFile != "" {
		fileCfg, err := config.LoadConfigFile(cfgFile)
		if err != nil {
			log.Fatalf("❌ Error loading config file: %v", err)
		}

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
		if !explicit["dry-run"] {
			cfg.DryRun = fileCfg.DryRun
		}
		if !explicit["validate-only"] {
			cfg.ValidateOnly = fileCfg.ValidateOnly
		}
		if !explicit["profile"] {
			cfg.Profile = fileCfg.Profile
		}
	}

	explicit := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	config.ApplyProfile(&cfg, explicit)

	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("ZABBIX_API_KEY")
	}
	if cfg.Pass == "" {
		cfg.Pass = os.Getenv("ZABBIX_PASS")
	}

	vRes := config.ValidateConfig(cfg)
	if len(vRes.Errors) > 0 {
		PrintValidationReport(vRes)
		os.Exit(1)
	}

	plan := config.BuildRuntimePlan(cfg)

	cfg.TrapperAddr = plan.TrapperAddr

	if cfg.DryRun {
		PrintStartupSummary("DRY RUN", plan, len(vRes.Warnings))
		PrintValidationReport(vRes)
		os.Exit(0)
	}

	if cfg.ValidateOnly {
		PrintStartupSummary("VALIDATION ONLY", plan, len(vRes.Warnings))
		PrintValidationReport(vRes)

		fmt.Println("🚀 Performing connectivity checks...")

		if _, err := zabbix.Login(cfg.APIURL, cfg.User, cfg.Pass, cfg.APIKey); err != nil {
			fmt.Printf("❌ API Connectivity/Auth: FAILED - %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✅ API Connectivity/Auth: SUCCESS")

		conn, err := net.DialTimeout("tcp", plan.TrapperAddr, 5*time.Second)
		if err != nil {
			fmt.Printf("❌ Trapper Connectivity: FAILED - %v\n", err)
			fmt.Println("   (Check firewall, port, and trapper address)")
			os.Exit(1)
		}
		_ = conn.Close()
		fmt.Println("✅ Trapper Connectivity: SUCCESS")

		fmt.Println("\n✨ Pre-flight checks passed successfully.")
		os.Exit(0)
	}

	PrintStartupSummary("BENCHMARK", plan, len(vRes.Warnings))

	sender := zabbix.NewTrapperSender(plan.TrapperAddr)
	bm := benchmark.NewBenchmarker(cfg, sender)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		signal.Reset(os.Interrupt, syscall.SIGTERM)
		fmt.Println()
		log.Printf("Interrupt received. Stopping benchmark (Ctrl+C again to force quit)...")
		cancel()
	}()

	if cfg.Duration > 0 {
		go func() {
			select {
			case <-time.After(cfg.Duration):
				log.Printf("Duration %s reached. Stopping...", cfg.Duration)
				cancel()
			case <-ctx.Done():
			}
		}()
	}

	api, err := zabbix.Login(cfg.APIURL, cfg.User, cfg.Pass, cfg.APIKey)
	if err != nil {
		log.Printf("❌ API Login failed: %v", err)
		os.Exit(1)
	}
	bm.SetAPI(api)

	if cfg.SkipSetup {
		if err := bm.LoadExistingHosts(); err != nil {
			log.Printf("❌ Setup failed: %v", err)
			bm.Cleanup()
			os.Exit(1)
		}
	} else {
		if err := bm.Setup(ctx); err != nil {
			log.Printf("❌ Setup failed: %v", err)
			bm.Cleanup()
			os.Exit(1)
		}
	}

	bm.Run(ctx)

	result := bm.GenerateResult()
	bm.PrintSummary(result)

	if cfg.OutputJSON != "" {
		bm.ExportJSON(result)
	}

	if !cfg.KeepHosts {
		bm.Cleanup()
	}
}
