package config

import (
	"testing"
	"time"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name             string
		cfg              Config
		expectedErrors   int
		expectedWarnings int
	}{
		{
			name: "Valid minimal config",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k", HostPrefix: "b-",
			},
			expectedErrors: 0, expectedWarnings: 0,
		},
		{
			name: "Numeric sanity checks",
			cfg: Config{
				NumHosts: 0, NumSenders: 0, MetricsPerHost: 0, BatchHosts: 0, MaxBatchSize: 0,
			},
			expectedErrors: 8, expectedWarnings: 0,
		},
		{
			name: "Missing auth",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", HostPrefix: "b-",
			},
			expectedErrors: 1, expectedWarnings: 0,
		},
		{
			name: "Invalid API URL suffix",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h", APIKey: "k", HostPrefix: "b-",
			},
			expectedErrors: 0, expectedWarnings: 1,
		},
		{
			name: "Senders > Hosts warning",
			cfg: Config{
				NumHosts: 1, NumSenders: 2, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k", HostPrefix: "b-",
			},
			expectedErrors: 0, expectedWarnings: 1,
		},
		{
			name: "Risky cleanup warning",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k", HostPrefix: "b-",
				GroupName: "Benchmark-Group", KeepHosts: false,
			},
			expectedErrors: 0, expectedWarnings: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ValidateConfig(tt.cfg)
			if len(res.Errors) != tt.expectedErrors {
				t.Errorf("expected %d errors, got %d: %v", tt.expectedErrors, len(res.Errors), res.Errors)
			}
			if len(res.Warnings) != tt.expectedWarnings {
				t.Errorf("expected %d warnings, got %d: %v", tt.expectedWarnings, len(res.Warnings), res.Warnings)
			}
		})
	}
}

func TestBuildRuntimePlan(t *testing.T) {
	cfg := Config{
		NumHosts: 10, NumSenders: 5, MetricsPerHost: 6, BatchHosts: 50, MaxBatchSize: 500,
		APIURL: "https://zabbix.example.com/api_jsonrpc.php", APIKey: "abc",
		Duration: 30 * time.Second, Rate: 0,
	}

	plan := BuildRuntimePlan(cfg)

	if plan.AuthMode != "API Token" {
		t.Errorf("expected AuthMode 'API Token', got %q", plan.AuthMode)
	}
	if plan.TrapperAddrLabel != "zabbix.example.com:10051 (inferred)" {
		t.Errorf("expected inferred trapper addr label, got %q", plan.TrapperAddrLabel)
	}
	if plan.EffectiveBatchSize != 50 {
		t.Errorf("expected BatchSize 50, got %d", plan.EffectiveBatchSize)
	}

	cfg.MaxBatchSize = 10
	plan = BuildRuntimePlan(cfg)
	if plan.EffectiveBatchSize != 1 {
		t.Errorf("expected BatchSize 1 due to MaxBatchSize=10 and MetricsPerHost=6, got %d", plan.EffectiveBatchSize)
	}
}

func TestApplyProfile(t *testing.T) {
	explicit := map[string]bool{}

	cfg := DefaultConfig()
	cfg.Profile = "flood"
	ApplyProfile(&cfg, explicit)

	if cfg.NumHosts != 300 {
		t.Errorf("Profile 'flood' should set hosts=300, got %d", cfg.NumHosts)
	}
	if cfg.NumSenders != 200 {
		t.Errorf("Profile 'flood' should set senders=200, got %d", cfg.NumSenders)
	}

	cfg = DefaultConfig()
	cfg.Profile = "light"
	cfg.NumHosts = 42
	explicit["hosts"] = true
	ApplyProfile(&cfg, explicit)

	if cfg.NumHosts != 42 {
		t.Errorf("CLI override should preserve hosts=42, got %d", cfg.NumHosts)
	}
	if cfg.NumSenders != 10 {
		t.Errorf("Profile 'light' should still set senders=10, got %d", cfg.NumSenders)
	}
}

func TestYAMLOverridePrecedence(t *testing.T) {
	fileCfg := Config{NumHosts: 100}
	cfg := Config{NumHosts: 50}

	explicit := map[string]bool{"hosts": true}

	if !explicit["hosts"] {
		cfg.NumHosts = fileCfg.NumHosts
	}

	if cfg.NumHosts != 50 {
		t.Errorf("Explicit CLI flag should win over YAML, expected 50 got %d", cfg.NumHosts)
	}

	explicit = map[string]bool{}
	if !explicit["hosts"] {
		cfg.NumHosts = fileCfg.NumHosts
	}
	if cfg.NumHosts != 100 {
		t.Errorf("YAML should win over default if not explicit, expected 100 got %d", cfg.NumHosts)
	}
}
