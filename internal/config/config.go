package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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
	DurationStr    string        `yaml:"duration"`
	Duration       time.Duration `yaml:"-"`
	SkipSetup      bool          `yaml:"skip_setup"`
	KeepHosts      bool          `yaml:"keep_hosts"`
	BatchHosts     int           `yaml:"batch_hosts"`
	MaxBatchSize   int           `yaml:"max_batch_size"`
	MetricsPerHost int           `yaml:"metrics_per_host"`
	OutputJSON     string        `yaml:"output_json"`
	DryRun         bool          `yaml:"dry_run"`
	ValidateOnly   bool          `yaml:"validate_only"`
	Profile        string        `yaml:"profile"`
}

func DefaultConfig() Config {
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

func LoadConfigFile(path string) (Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}

	if cfg.DurationStr != "" {
		d, err := time.ParseDuration(cfg.DurationStr)
		if err != nil {
			return cfg, fmt.Errorf("invalid duration %q: %w", cfg.DurationStr, err)
		}
		cfg.Duration = d
	}

	return cfg, nil
}

func ApplyProfile(cfg *Config, explicitFlags map[string]bool) {
	if cfg.Profile == "" {
		return
	}

	type profileDefaults struct {
		hosts   int
		senders int
		rate    int
	}

	profiles := map[string]profileDefaults{
		"light":    {hosts: 25, senders: 10, rate: 1},
		"balanced": {hosts: 100, senders: 50, rate: 0},
		"flood":    {hosts: 300, senders: 200, rate: 0},
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

	if cfg.NumHosts <= 0 {
		res.Errors = append(res.Errors, "hosts must be > 0")
	}
	if strings.TrimSpace(cfg.HostPrefix) == "" {
		res.Errors = append(res.Errors, "prefix must not be empty")
	}
	if cfg.NumSenders <= 0 {
		res.Errors = append(res.Errors, "senders must be > 0")
	}
	if cfg.MetricsPerHost <= 0 {
		res.Errors = append(res.Errors, "metrics-per-host / metrics_per_host must be > 0")
	}
	if cfg.BatchHosts <= 0 {
		res.Errors = append(res.Errors, "batch-hosts / batch_hosts must be > 0")
	}
	if cfg.MaxBatchSize <= 0 {
		res.Errors = append(res.Errors, "batch-metrics / max_batch_size must be > 0")
	}
	if cfg.Rate < 0 {
		res.Errors = append(res.Errors, "rate must be >= 0")
	}

	if cfg.MetricsPerHost > 0 && cfg.MaxBatchSize > 0 && cfg.MaxBatchSize < cfg.MetricsPerHost {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"batch-metrics / max_batch_size (%d) is smaller than metrics-per-host / metrics_per_host (%d); effective host batch size will be 1",
			cfg.MaxBatchSize,
			cfg.MetricsPerHost,
		))
	}

	if cfg.NumSenders > cfg.NumHosts {
		res.Warnings = append(res.Warnings, fmt.Sprintf("senders (%d) exceeds host count (%d); some workers will be idle", cfg.NumSenders, cfg.NumHosts))
	}

	if cfg.SkipSetup {
		res.Warnings = append(res.Warnings, "skip_setup is enabled; cleanup will not delete pre-existing hosts")
	}

	if !cfg.KeepHosts && cfg.GroupName == "Benchmark-Group" {
		res.Warnings = append(res.Warnings, "cleanup is enabled with the default group name 'Benchmark-Group'; only hosts created by this run will be deleted")
	}

	if cfg.APIURL == "" {
		res.Errors = append(res.Errors, "api_url is required")
	} else if !strings.HasSuffix(cfg.APIURL, "/api_jsonrpc.php") {
		res.Warnings = append(res.Warnings, "api_url does not end with /api_jsonrpc.php; check if this is correct")
	}

	if cfg.APIKey == "" && (cfg.User == "" || cfg.Pass == "") {
		res.Errors = append(res.Errors, "authentication requires either api_key or both user and password")
	}

	if cfg.OutputJSON != "" {
		dir := filepath.Dir(cfg.OutputJSON)
		if dir != "." && dir != "" {
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
	TrapperAddrLabel   string
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

	if cfg.APIKey != "" {
		plan.AuthMode = "API Token"
	} else {
		plan.AuthMode = fmt.Sprintf("User/Pass (user: %s)", cfg.User)
	}

	if cfg.TrapperAddr != "" {
		plan.TrapperAddr = cfg.TrapperAddr
		plan.TrapperAddrLabel = cfg.TrapperAddr
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
			plan.TrapperAddr = host + ":10051"
			plan.TrapperAddrLabel = plan.TrapperAddr + " (inferred)"
		} else {
			plan.TrapperAddr = "127.0.0.1:10051"
			plan.TrapperAddrLabel = plan.TrapperAddr + " (default)"
		}
	}

	effectiveBatch := cfg.BatchHosts
	if cfg.MaxBatchSize > 0 && cfg.MetricsPerHost > 0 {
		hostsFit := cfg.MaxBatchSize / cfg.MetricsPerHost
		if hostsFit > 0 && hostsFit < effectiveBatch {
			effectiveBatch = hostsFit
		}
	}
	if effectiveBatch <= 0 {
		effectiveBatch = 1
	}
	plan.EffectiveBatchSize = effectiveBatch

	if cfg.Rate == 0 {
		plan.RateMode = "Flood (as fast as possible)"
	} else {
		plan.RateMode = fmt.Sprintf("Fixed (%d packets/sec per worker)", cfg.Rate)
	}

	return plan
}
