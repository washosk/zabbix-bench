# Changelog

All notable changes to zabbix-bench are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.6.1] - 2026-04-17

### Fixed

- **Security — Weak RNG Suppression (G404)**: Replaced `//nolint:gosec` with native gosec `// #nosec G404` annotations. GitHub code scanning runs gosec directly, not through golangci-lint, so the previous directives had no effect.
- **Security — Integer Overflow (G115)**: Eliminated remaining `int → byte` cast in char value generation by indexing into a `const` alphabet string (`benchAlpha[n:n+1]`), removing the numeric conversion entirely.

---

## [1.6.0] - 2026-04-17

### Added

- **Docker Support**: Multi-stage `Dockerfile` producing a ~6 MB Alpine image. Includes a `.dockerignore` to keep build context lean.
- **Docker Documentation**: Usage examples added to `README.md` and `QUICKSTART.md` covering basic runs, environment variable auth, volume-mounted JSON output, YAML config files, and the `--add-host` pattern for host-local Zabbix deployments.

### Fixed

- **Security — File Permissions (G306)**: JSON output file now written with mode `0600` instead of `0644`.
- **Security — Path Traversal (G304)**: Config file path sanitized with `filepath.Clean()` before reading.
- **Security — Integer Overflow (G115)**: Removed unsafe `int(uint(idx) % uint(poolSize))` pattern in worker loop; replaced with direct `idx % poolSize`. Fixed char value cast from `rune` to `byte` to avoid int→int32 truncation.
- **Code Quality — Weak RNG Annotation (G404)**: Annotated intentional `math/rand` usage in benchmark data generation to suppress false-positive security warnings; non-crypto randomness is correct for synthetic metric values.

---

## [1.5.0] - 2026-04-16

### Added

- **Performance Profiles**: Introduced `--profile` (`light`, `balanced`, `flood`) for rapid testing with sensible defaults.
- **Dry Run Mode**: New `--dry-run` flag to preview execution plans and inferred parameters without making network changes.
- **Validate-Only Mode**: New `--validate-only` flag to perform active API and Trapper connectivity pre-flight checks.
- **Startup Summary Reports**: High-visibility console reports providing full transparency of the runtime plan before benchmark execution.
- **Centralized Validation Engine**: Robust pre-flight configuration checks for numeric sanity, auth validity, and operational safety.

### Fixed

- **Config Precedence**: Ensured consistent merging where CLI flags correctly override YAML and Profile values.
- **Trapper Address Labels**: Fixed a parsing issue where display labels (e.g., "(default)") interfered with network dialing.

---

## [1.4.1] - 2026-04-16

### Added

- **Parallel Execution Breakdown**: Renamed the per-worker statistics section for better technical clarity.
- **Aligned Worker Metrics**: Worker statistics in the console output are now padded and aligned (e.g., `Worker #00`), improving readability at high sender counts.

---

## [1.4.0] - 2026-04-12

### Added

- **YAML Duration Parsing**: Config files now accept Go duration strings (e.g. `duration: "30s"`, `duration: "2m"`). Previously duration was CLI-only.
- **Input Validation**: `-hosts` and `-metrics-per-host` are now validated on startup. Zero or negative values exit with a clear error instead of producing empty results.
- **Zabbix Response Validation**: The Trapper sender now parses response bodies and reports an error when Zabbix rejects data, instead of silently counting rejections as successes.
- **`--skip-setup` Cleanup Support**: When using `--skip-setup`, the tool now queries the API for the host group and host IDs so that cleanup works correctly without `--keep-hosts`.

### Changed

- **Counter Naming**: Internal counters and JSON output fields renamed for clarity. `total_batches` is now `total_host_sends` (counts host-sends, not packets). `packets_sent` is now `total_packets`. Progress log and summary report labels updated to match.
- **Batch Size Logic**: `-batch-metrics` now constrains `-batch-hosts` (uses the smaller of the two) instead of silently replacing it.
- **Summary Report**: Rewrote the box-drawing output to use a helper that pads every line to the same width. All lines now align correctly regardless of value length.
- **Config Defaults**: Extracted into a single `defaultConfig()` function, eliminating a duplicated block between `loadConfigFile()` and `main()`.

### Removed

- **TCP Connection Pool (`-pool-size`)**: Removed entirely. Zabbix Trapper closes connections after each response, making pooled connections stale on reuse. This caused 50% error rates when enabled. Each send now uses a fresh connection with `defer conn.Close()` for clean resource handling.
- **Redundant `contains()` wrapper**: Inlined `strings.Contains` at call sites.

### Fixed

- **Retry Double-Send**: The old pooled sender could retry a write after data was already sent, causing duplicate metrics. The new sender uses one connection per request with no retry ambiguity.
- **`MetricsPerHost=0` Mismatch**: The worker defaulted to 6 metrics internally while `GenerateResult` used the raw config value (0), producing zero throughput in reports. Now validated at startup.

---

## [1.3.4] - 2026-04-11

### Fixed

- **Latency Slice Cap**: Latency samples are now capped at 1,000,000 entries to prevent unbounded memory growth at high throughput.
- **Connection Pool Cleanup**: TCP connections sitting idle in the pool are now drained and closed after the benchmark finishes, eliminating file descriptor leaks.
- **Division by Zero in Throughput**: Elapsed time is now guarded with a 1ms minimum in both the progress ticker and final result, preventing `+Inf` VPS on very short runs.
- **Signal Handler Goroutine Leak**: The signal handler goroutine now exits cleanly when the benchmark finishes normally via duration timer, instead of blocking on the signal channel indefinitely.
- **Global Mutex Contention**: Replaced the single shared `workerMu` with a per-worker mutex slice, eliminating lock contention across 200+ concurrent senders.

---

## [1.3.3] - 2026-04-11

### Fixed

- **JSON Marshal Error**: `SendMetrics` now returns an error instead of silently sending a malformed packet when `json.Marshal` fails.
- **Connection Deadline**: `SetDeadline` errors on TCP connections are now caught and returned immediately instead of being ignored.
- **Idle Workers in Results**: Worker stats with zero packets sent are no longer included in the benchmark report or JSON output.
- **Min Latency Tracking**: `MinLatencyMs` is now initialized to `math.MaxInt64` per worker, eliminating ambiguity between an unset value and an actual 0ms latency.

---

## [1.3.2] - 2026-04-10

### Added

- **Metric-based Batching**: Introduced `-batch-metrics` to decouple batching from host count, allowing high-density payloads.
- **Connection Pooling** (removed in 1.4.0): Added `-pool-size` flag for TCP connection reuse. Later found incompatible with Zabbix Trapper's per-request connection model.

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

## [1.2.1] - 2026-04-10

### Fixed

- **CI Permissions**: Added write permissions to the release job to allow artifact uploads.
- **Release Workflow**: Fixed CI trigger to fire correctly on version tags.
- **Windows Build**: Resolved Windows CI build failure introduced in v1.2.0.

---

## [1.2.0] - 2026-04-10

### Fixed

- **VPS Calculations**: Corrected throughput calculations that were producing incorrect values per second.
- **Config Loading**: Fixed YAML config file loading to properly merge with CLI flag overrides.
- **Shutdown Behavior**: Resolved a race condition causing ungraceful shutdown on interrupt.

---

## [1.1.0] - 2026-04-10

### Fixed

- **Dynamic Item Creation**: Items are now created to match the configured `-metrics-per-host` value instead of always defaulting to 6.
- **Metrics Key Naming**: Item keys now use the same `test.metric.{index}.{type}` convention as sent metrics, preventing silent data rejection by Zabbix.

---

## [1.0.9] - 2026-04-10

### Added

- **Auto-detect Trapper Address**: Trapper address is now automatically derived from the API URL when `-trapper-addr` is not explicitly set.

---

## [1.0.8] - 2026-04-10

### Added

- **Configurable Metrics Per Host**: Introduced `-metrics-per-host` flag for deeper and more flexible stress testing configurations.

---

## [1.0.7] - 2026-04-10

### Fixed

- **Panic Recovery**: Added recovery handler for malformed server responses in the Zabbix sender to prevent full process crashes.

---

## [1.0.6] - 2026-04-10

### Fixed

- **Duration Calculation**: Corrected elapsed time computation used for throughput reporting in both terminal output and JSON export.
- **Throughput in JSON**: JSON export now reflects accurate VPS values consistent with the terminal summary.

---

## [1.0.5] - 2026-04-10

### Fixed

- **CI Security Scan**: Switched gosec report to SARIF format for compatibility with GitHub code scanning.

---

## [1.0.4] - 2026-04-10

### Fixed

- **Markdown Linting**: Resolved all markdown lint errors in documentation files.

---

## [1.0.3] - 2026-04-10

### Fixed

- **GitHub Actions**: Updated workflow actions to versions compatible with Node.js 24 runtime.

---

## [1.0.2] - 2026-04-10

### Fixed

- **GitHub Actions**: Opted into Node.js 24 for GitHub Actions runner compatibility.

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

### Planned

- Custom item type configuration
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
| --- | --- | --- | --- |
| 1.4.1 | 2026-04-16 | Stable | Improved worker metrics naming and alignment |
 | 1.4.0 | 2026-04-12 | Stable | Remove pool, add validation, fix naming, YAML duration |
| 1.3.4 | 2026-04-11 | Stable | Performance and stability fixes |
| 1.3.3 | 2026-04-11 | Stable | Bug fixes |
| 1.3.2 | 2026-04-10 | Stable | Metric-based batching |
| 1.3.1 | 2026-04-10 | Stable | Version flag |
| 1.3.0 | 2026-04-10 | Stable | API engine swap |
| 1.2.2 | 2026-04-10 | Stable | Stability fixes |
| 1.2.1 | 2026-04-10 | Stable | CI/CD fixes |
| 1.2.0 | 2026-04-10 | Stable | VPS and config fixes |
| 1.1.0 | 2026-04-10 | Stable | Dynamic item creation |
| 1.0.9 | 2026-04-10 | Stable | Auto-detect trapper address |
| 1.0.8 | 2026-04-10 | Stable | Configurable metrics per host |
| 1.0.7 | 2026-04-10 | Stable | Panic recovery |
| 1.0.6 | 2026-04-10 | Stable | Duration and throughput fixes |
| 1.0.5 | 2026-04-10 | Stable | CI security scan fix |
| 1.0.4 | 2026-04-10 | Stable | Markdown linting |
| 1.0.3 | 2026-04-10 | Stable | GitHub Actions update |
| 1.0.2 | 2026-04-10 | Stable | GitHub Actions Node.js 24 |
| 1.0.1 | 2026-04-10 | Stable | Item creation fix |
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
