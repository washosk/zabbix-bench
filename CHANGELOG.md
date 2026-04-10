# Changelog

All notable changes to zabbix-bench are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.3.1] - 2026-04-10

### Added

- **Version Flag**: Added `-v` and `--version` flags to print the release version.

---

## [1.3.0] - 2026-04-10

### Added

- **API Engine Swap**: Migrated the legacy `claranet/go-zabbix-api` engine wrapper to `kgeroczi/go-zabbix-api` to ensure seamless structural integration with modern Zabbix 7.0+ API features.

### Changed

- **Token Safety**: Direct API Token binding using native `.Token()` logic replaces unsafe attribute coupling.
- **Interfaces Casting**: Strong-typed integer mapping on API interface parameters ensures compliance with modern strict validation policies on Zabbix hosts.

### Fixed

- **Host Creation Subsystem**: Completely circumvented an internal JSON type-casting library defect limiting host arrays by converting generation directly into native REST Map interfaces.

---

## [1.2.2] - 2026-04-10

### Fixed

- **Execution Safety**: Resolved index wrap-around `ValuePool` overflow leading to memory panics on 32-bit archs.
- **Panic Protection**: Blocked instantaneous crash when `-senders 0` was configured.
- **Timing Robustness**: Fixed rate-limiter panic when parsing inputs >1000 VPS.
- **Runtime Recovery**: Handled uninterruptible signal blocks causing terminal lockups on failed cleanup.
- **Auth Integrity**: Environment variables (`ZABBIX_PASS`) are now securely registered and processed over defaults.
- **Setup Speed**: Vastly optimized the item creation block into grouped API matrices, dropping network payload time exponentially.
- **Latency Compute**: Bypassed O(N) evaluation bounds during generation stats, scaling direct O(1) reads pre-sort.

---

## [1.0.1] - 2026-04-10

### Fixed

- Critical bug in metrics-per-host feature: item creation now dynamically matches the configured MetricsPerHost setting instead of always creating 6 hardcoded items. This fixes silent data loss where metrics were sent to non-existent items and rejected by Zabbix without error reporting.
- Item keys are now generated using the same naming convention as metrics (test.metric.{index}.{type}) ensuring proper data ingestion
- Value types for dynamically created items now correctly map to metric types

---

## [1.0.0] - 2026-04-10

### Added

- Initial stable release of zabbix-bench
- High-performance Zabbix NVPS benchmark tool
- Flood mode (`-rate 0`) for unlimited throughput testing
- Rate limiting mode with configurable batches/second
- Latency percentiles (P50, P95, P99) for performance analysis
- Per-worker statistics showing individual sender performance
- Error categorization (timeout, connection closed, network, other)
- JSON output export (`-output-json`) for analysis and CI/CD
- YAML configuration file support (`-config`) for reusable setups
- API token authentication support (`-api-key`, `ZABBIX_API_KEY` env var)
- Environment variable support for sensitive credentials
- Graceful shutdown with proper resource cleanup
- Real-time progress reporting every 5 seconds
- Duration-based auto-stop (`-duration`)
- Automatic host and item creation via Zabbix API
- Bulk Trapper packet support for maximum throughput
- Pre-generated value pool to eliminate rand() overhead
- Zero-copy atomic counters for stats tracking
- Comprehensive README with examples and tuning guide
- Monitoring guide for Zabbix internal metrics
- MIT license
- GitHub Actions CI/CD workflows
  - Automated testing on Go 1.23 & 1.24
  - Multi-platform builds (Linux, macOS, Windows)
  - Linting and code quality checks
  - Documentation validation
- Distribution guides for package managers
- Contributing guidelines

### Performance

- Peak throughput: 266,828 VPS
- Sustained throughput: 54,178 VPS over 10 minutes
- Average latency: 5ms (P99: 5ms)
- Tested with: 50 hosts, 20 senders, 1.8M packets, 0 errors

### Requirements

- Go 1.24+ (for building)
- Zabbix 5.4+ (for API token support)
- Zabbix API access (Admin or Super Admin)
- Zabbix Trapper port (default: 10051)

### Known Limitations

- Hardcoded 6 metric types per host (Boolean, Unsigned, Float, Text, Character, Log)
- Single-machine benchmarking only (no distributed mode)
- No GUI or web dashboard

---

## Upcoming

### [1.1.0] - Planned

- Custom item type configuration
- Connection pooling for Trapper sender
- Histogram/bucket latency tracking
- Warmup phase option
- Comparison mode between benchmark runs

### [1.2.0] - Planned

- Web UI for real-time monitoring
- Extended metrics collection
- Database performance profiling
- Additional output formats (CSV, HTML)

### [2.0.0] - Future

- Distributed benchmarking (multiple clients)
- Python/Bash ports
- Custom metric generators
- Cloud deployment templates

---

## How to upgrade

Each release provides pre-built binaries for:

- Linux (x86_64, ARM64)
- macOS (Intel, Apple Silicon)
- Windows (x86_64)

Download from [Releases](https://github.com/washosk/zabbix-bench/releases)

Or update via package manager:

```bash
# Homebrew
brew upgrade zabbix-bench

# AUR
yay -S zabbix-bench --needed

# Go
go install github.com/washosk/zabbix-bench@latest
```

---

## Version history reference

| Version | Release Date | Status | Notes |
|---------|--------------|--------|-------|
| 1.0.0 | 2026-04-10 | Stable | Initial release |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for how to contribute changes.

---

## Changelog maintenance

- Entries are grouped by type: Added, Changed, Deprecated, Removed, Fixed, Security
- Each version has a release date
- Links to tags and comparison diffs are included
- Breaking changes are clearly marked
