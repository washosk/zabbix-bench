# Project Context: zabbix-bench

## Overview

`zabbix-bench` is a high-performance benchmarking and stress testing tool for Zabbix 7.0+ monitoring environments. It measures ingest throughput (NVPS — New Values Per Second) and performance under load through the Zabbix Trapper protocol.

Built in Go, the tool automates the complete benchmark lifecycle: automated setup of benchmark hosts/items via Zabbix API, high-volume metric injection via Trapper, real-time performance analytics, and complete cleanup.

## Purpose & Use Cases

### Primary Use Case

Capacity planning and performance testing for Zabbix deployments:

- Measure maximum NVPS (New Values Per Second) a server can sustain
- Test performance impact of tuning changes (compare before/after)
- Validate database-backed ingest paths under stress
- Generate reproducible benchmarks for CI, labs, or internal documentation

### Key Features

- **Automated Setup**: Creates benchmark host groups, hosts, and Trapper items via Zabbix API
- **Scalable Load Generation**: Configurable host count, sender workers, metric density per host
- **Diverse Data Simulation**: Six metric types cycled per host (Boolean, Unsigned, Float, Text, Character, Log)
- **Flood Mode**: High-volume pressure testing with `-rate 0` (no rate limiting)
- **Intelligent Batching**: Bulk Trapper packets with host-based and metric-count batching for protocol efficiency
- **Real-time Analytics**: Latency tracking with P50, P95, P99 percentiles (O(1) efficiency using ring buffer)
- **Detailed Reporting**: Per-worker stats (VPS, packet count, latency, errors), aggregate throughput
- **Advanced Error Tracking**: Categorized errors (timeouts, connection resets, network errors)
- **Extensible Export**: JSON results for integration with Grafana or custom analysis
- **Operational Safety**: `--dry-run` to preview execution, `--validate-only` for connectivity checks

## Architecture

### Three-Phase Execution

#### 1. Setup Phase

- Authenticate to Zabbix API using username/password or API token
- Ensure benchmark host group exists (create if missing)
- Create benchmark hosts: named `{prefix}-{padded-sequence}` e.g., `bench-0001`, `bench-0002`
- Create Trapper items on each host (6 items per host, one per metric type)
- Store host IDs for metric sending and cleanup

**Configuration**:
- `-hosts N` — Number of hosts to create (default 10)
- `-prefix name` — Host name prefix (default "bench")
- `-group name` — Benchmark host group name (default "Benchmark")

#### 2. Benchmark Phase

- Pre-generate pool of random metric values in memory (reduces allocation during sending)
- Spawn concurrent sender workers (default 10)
- Each worker:
  - Connects to Zabbix Trapper (port 10051 or custom via `-trapper-addr`)
  - Loops for configured duration, batching and sending metrics
  - Tracks latency, error categories, and packet counts per worker
  - Maintains atomic counters for thread-safe aggregation
- Main thread:
  - Collects per-second stats from all workers
  - Prints live progress (current VPS, total throughput)
  - Calculates latency percentiles
- Exit on timeout or Ctrl+C gracefully (finalizes stats)

**Configuration**:
- `-duration 30s` — Benchmark duration (default 30s)
- `-senders N` — Concurrent Trapper connections (default 10)
- `-rate N` — Target rate in values/sec (0 = flood mode, unlimited)
- `-metrics-per-host N` — Metrics per host per send (default 6)
- `-batch-hosts N` — Hosts per batch (default 1)
- `-max-batch-size N` — Max total metrics per batch (default 1000)

#### 3. Cleanup Phase

- Unless `-keep-hosts` specified:
  - Delete all hosts in benchmark group by hostID
  - Delete the benchmark group itself
- Optionally export results to JSON (`-output-json file.json`)

**Configuration**:
- `-keep-hosts` — Preserve benchmark hosts after run (useful for inspection)

### Code Structure

```
main.go
├── Config struct               — All configuration parameters
├── ValuePool struct            — Pre-generated random metric values
├── ErrorCategory struct        — Categorized error counts
├── WorkerStats struct          — Per-worker performance metrics
├── BenchmarkResult struct      — Aggregated results and metadata
│
├── main()                       — Entry point, orchestrates 3 phases
├── loadConfig()                 — Parse flags, YAML config, apply defaults
├── validateConfig()             — Verify required params, test API connectivity
├── dryRun()                     — Print execution plan without network impact
├── setupZabbix()               — Create benchmark group, hosts, items
├── runBenchmark()              — Spawn workers, send metrics, collect stats
├── sender()                     — Worker goroutine: connect, batch, send metrics
├── cleanup()                    — Delete benchmark resources
├── exportJSON()                 — Marshal results to JSON file
│
├── latencyPercentile()         — Calculate P50/P95/P99 from samples (O(1))
├── constructBatch()            — Build Trapper packet from metrics
└── getPercentilesFromRing()   — Extract percentiles from ring buffer
```

### Key Data Structures

**Config**: All CLI flags and YAML settings

```go
type Config struct {
    NumHosts       int           // Total hosts to create
    HostPrefix     string        // Host name prefix
    NumSenders     int           // Concurrent Trapper workers
    Rate           int           // Target rate (0=unlimited)
    APIURL         string        // Zabbix API endpoint
    User/Pass      string        // API credentials
    APIKey         string        // API token (alternative to user/pass)
    TrapperAddr    string        // Trapper IP:port (default derived from APIURL)
    GroupName      string        // Benchmark host group
    Duration       time.Duration // Benchmark duration
    SkipSetup      bool          // Use existing hosts
    KeepHosts      bool          // Don't delete hosts after
    MetricsPerHost int           // Metrics per send
    OutputJSON     string        // Results file
    DryRun         bool          // Preview only
    ValidateOnly   bool          // Check API only
    Profile        string        // Preset: light|balanced|flood
}
```

**BenchmarkResult**: Final aggregated statistics

```go
type BenchmarkResult struct {
    Duration        float64       // Benchmark time in seconds
    HostsTested     int           // Number of hosts
    TotalHostsSent  int64         // Total host sends
    TotalValues     int64         // Total metric values sent
    TotalPackets    int64         // Total Trapper packets
    ErrorCount      int64         // Total errors
    ThroughputVPS   float64       // Values per second
    Percentiles     map[string]float64 // P50, P95, P99 latency (ms)
    WorkerStats     []WorkerStats // Per-worker breakdown
}
```

**ValuePool**: Pre-generated metric values (reduces GC pressure)

```go
type ValuePool struct {
    bools  []string // 0 or 1
    uints  []string // Random uint64
    floats []string // Random float64 0-100
    chars  []string // Random letter A-Z
}
```

### Latency Calculation (O(1) Efficiency)

Rather than storing all latency samples and sorting them at the end (O(n log n)), the tool uses a ring buffer approach:

- **Ring buffer size**: 1,000,000 slots (maxLatencySamples constant)
- **On metric send**: Record latency in ring buffer at index `atomic.AddInt64(...) % bufferSize`
- **At end**: Extract P50/P95/P99 directly from samples without sorting
- **Trade-off**: Accuracy for samples > 1M (unlikely in practice), but deterministic O(1) time complexity

This pattern trades perfect accuracy for predictable performance during high-throughput testing.

## Dependencies

### External Packages

- **github.com/chmller/go-zabbix-sender** — Zabbix Trapper protocol (metric sending)
- **github.com/kgeroczi/go-zabbix-api** — Zabbix API client (host/item management)
- **gopkg.in/yaml.v3** — YAML config file parsing

### Standard Library

- `net`, `sync`, `atomic` — Concurrent network I/O
- `encoding/json` — JSON export
- `flag`, `os` — CLI flags and environment

## Configuration Methods (Priority Order)

### 1. CLI Flags (Highest Priority)

```bash
./zabbix-bench \
  -api-url "http://zabbix/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -hosts 100 \
  -duration 60s \
  -output-json /tmp/results.json
```

### 2. YAML Config File (Medium Priority)

Create `zabbix-bench.yaml`:

```yaml
api_url: http://zabbix/api_jsonrpc.php
user: Admin
pass: zabbix
hosts: 100
prefix: bench
senders: 20
duration: 60s
output_json: /tmp/results.json
```

Run: `./zabbix-bench`

### 3. Environment Variables (No Current Implementation)

Could be added for CI/CD, e.g., `ZABBIX_API_URL`, `ZABBIX_USER`, etc.

## Performance Profiles

Preset configurations for common scenarios:

| Profile | Hosts | Senders | Metrics/Host | Rate | Use Case |
| --- | --- | --- | --- | --- | --- |
| `light` | 10 | 5 | 6 | 1000 VPS | Quick smoke test, safe for prod-adjacent |
| `balanced` | 100 | 20 | 12 | 10000 VPS | Typical capacity test |
| `flood` | 1000 | 100 | 20 | 0 (unlimited) | Maximum stress test |

Usage: `-profile flood`

## Safety Mechanisms

### Preventing Accidental Data Loss

1. **Unique group naming**: Use dedicated group names, e.g., `Benchmark-Group-2026-04-21` (not `Benchmark`)
2. **Dry-run mode**: `--dry-run` previews execution without network changes
3. **Keep-hosts flag**: `-keep-hosts` preserves resources after benchmark
4. **Startup confirmation**: Displays host count, group name, and cleanup intent before starting
5. **Aggressive cleanup**: Deletes all hosts in the group (design intent: fail-safe)

### Important Caveats

- `-skip-setup` does not validate that hosts exist before sending; it reconstructs hostnames from prefix/count
- Default cleanup deletes **all hosts returned by Zabbix for the configured group name**, then removes the group
- `kill -9` prevents graceful cleanup (manual cleanup of hosts required)
- If `-trapper-addr` not specified, tool derives it from `-api-url` using same hostname but port 10051

## Error Handling

Errors are categorized during Trapper sends:

| Category | Cause | Example |
| --- | --- | --- |
| **Timeout** | Connection timeout or deadline exceeded | Zabbix overloaded, dropping packets |
| **Closed** | Connection reset by peer (ECONNRESET) | Trapper crash or reload |
| **Network** | DNS, unreachable, etc. | Network partition, wrong IP |
| **Other** | Any other error | Malformed packet, permission denied |

Per-worker error counts tracked; aggregate reported at end. Benchmarks continue despite errors (partial results preserved).

## Testing & Validation

### Validation Flags

- `-validate-only` — Test API connectivity without creating hosts or sending metrics
- `-dry-run` — Print execution plan and derived values; no network calls

### Manual Testing

```bash
# Against local Zabbix
./zabbix-bench \
  -profile light \
  -api-url "http://localhost:8080/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -duration 10s

# With token auth
export ZABBIX_TOKEN="your_api_token_here"
./zabbix-bench \
  -api-url "http://zabbix/api_jsonrpc.php" \
  -api-key "$ZABBIX_TOKEN" \
  -duration 10s

# Preview without changes
./zabbix-bench -profile flood -dry-run
```

### Unit Tests

- `main_test.go` contains current test suite
- Test config parsing, value pool generation, error categorization
- Run: `go test ./...`

### CI/CD Testing

GitHub Actions workflows verify:
- Builds successfully for Linux, macOS, Windows
- Passes `go vet` and `gofmt` checks
- All unit tests pass

## Release Process

1. Update `Version` string in main.go
2. Update CHANGELOG.md with changes
3. Commit: `git commit -m "Release vX.Y.Z"`
4. Tag: `git tag vX.Y.Z`
5. Push: `git push origin vX.Y.Z`
6. GitHub Actions:
   - Builds for all platforms
   - Creates GitHub release
   - Updates Docker image (if Dockerfile present)
7. Manually update:
   - Homebrew formula (if applicable)
   - AUR PKGBUILD (if applicable)

## Distribution

### Package Managers

- **Homebrew** — Install via `brew install zabbix-bench` (requires formula update)
- **AUR** — Arch Linux PKGBUILD (requires PKGBUILD update)
- **Docker** — Pre-built image on GitHub (automatic via Actions)

See `DISTRIBUTION.md` for detailed packaging instructions.

## Current Limitations & Future Work

### Implemented

- ✓ SNMP benchmark support via Zabbix Trapper
- ✓ Automated host/item creation and cleanup
- ✓ Multi-worker concurrent sending
- ✓ JSON export for analysis
- ✓ Dry-run and validation-only modes
- ✓ Performance profiles (light/balanced/flood)

### Planned / High Priority

- [ ] Extended metric tracking (histogram buckets for deeper latency analysis)
- [ ] More package manager support (DEB, RPM pre-built packages)
- [ ] Custom item type configuration (beyond built-in 6 types)

### Medium Priority

- [ ] Web UI dashboard for real-time monitoring
- [ ] Distributed benchmarking (multiple client machines coordinating)
- [ ] Database performance profiling (PostgreSQL-specific metrics)
- [ ] Integration tests with real Zabbix instances

### Lower Priority

- [ ] Additional output formats (CSV, HTML reports)
- [ ] Comparison mode (before/after results diff)
- [ ] Python/Bash port
- [ ] Warmup phase option (to stabilize metrics before measuring)

## How to Contribute

See [CONTRIBUTING.md](CONTRIBUTING.md) for detailed guidelines on:
- Code style (Go conventions, gofmt, go vet)
- Testing expectations
- Commit message format
- Pull request process
- Areas for contribution (high/medium/lower priority)
- Development tips (cross-platform builds, local testing, profiling)

## How to Apply Learning

When working on zabbix-bench:
- **Concurrent performance**: Worker pattern scales horizontally; benchmarks should test varying sender counts
- **Latency tracking**: O(1) ring buffer approach trades perfect accuracy for predictable performance
- **Error resilience**: Continue on partial failures; report aggregate statistics for visibility
- **API integration**: Always test connectivity before assuming it works
- **User safety**: Provide dry-run mode for any destructive operations (host cleanup)
- **Operational clarity**: Print startup summary with all configuration for transparency
