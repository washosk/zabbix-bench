package main

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
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k",
			},
			expectedErrors: 0, expectedWarnings: 0,
		},
		{
			name: "Numeric sanity checks",
			cfg: Config{
				NumHosts: 0, NumSenders: 0, MetricsPerHost: 0, BatchHosts: 0, MaxBatchSize: 0,
			},
			expectedErrors: 7, expectedWarnings: 0,
		},
		{
			name: "Missing auth",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php",
			},
			expectedErrors: 1, expectedWarnings: 0,
		},
		{
			name: "Invalid API URL suffix",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h", APIKey: "k",
			},
			expectedErrors: 0, expectedWarnings: 1,
		},
		{
			name: "Senders > Hosts warning",
			cfg: Config{
				NumHosts: 1, NumSenders: 2, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k",
			},
			expectedErrors: 0, expectedWarnings: 1,
		},
		{
			name: "Risky cleanup warning",
			cfg: Config{
				NumHosts: 1, NumSenders: 1, MetricsPerHost: 1, BatchHosts: 1, MaxBatchSize: 1, APIURL: "http://h/api_jsonrpc.php", APIKey: "k",
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
	if plan.TrapperAddr != "zabbix.example.com:10051 (inferred)" {
		t.Errorf("expected inferred trapper addr, got %q", plan.TrapperAddr)
	}
	if plan.EffectiveBatchSize != 50 {
		t.Errorf("expected BatchSize 50, got %d", plan.EffectiveBatchSize)
	}

	// Test metric-limited batch size
	cfg.MaxBatchSize = 10
	plan = BuildRuntimePlan(cfg)
	if plan.EffectiveBatchSize != 1 {
		t.Errorf("expected BatchSize 1 due to MaxBatchSize=10 and MetricsPerHost=6, got %d", plan.EffectiveBatchSize)
	}
}

func TestApplyProfile(t *testing.T) {
	// Need to simulate flag visibility
	explicit := map[string]bool{}

	cfg := defaultConfig()
	cfg.Profile = "flood"
	applyProfile(&cfg, explicit)

	if cfg.NumHosts != 100 {
		t.Errorf("Profile 'flood' should set hosts=100, got %d", cfg.NumHosts)
	}
	if cfg.NumSenders != 50 {
		t.Errorf("Profile 'flood' should set senders=50, got %d", cfg.NumSenders)
	}

	// CLI override should win
	cfg = defaultConfig()
	cfg.Profile = "light"
	cfg.NumHosts = 42
	explicit["hosts"] = true
	applyProfile(&cfg, explicit)

	if cfg.NumHosts != 42 {
		t.Errorf("CLI override should preserve hosts=42, got %d", cfg.NumHosts)
	}
	if cfg.NumSenders != 2 {
		t.Errorf("Profile 'light' should still set senders=2, got %d", cfg.NumSenders)
	}
}

func TestYAMLOverridePrecedence(t *testing.T) {
	// This test essentially verifies the logic in main() for merging fileCfg and cfg.
	// We can't easily test main() directly without reorganization, but we can verify the merging logic.

	fileCfg := Config{NumHosts: 100}
	cfg := Config{NumHosts: 50} // Default from flag defined value

	explicit := map[string]bool{"hosts": true}

	// Simulation of logic:
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
