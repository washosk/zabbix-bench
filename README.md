# zabbix-bench

A high-performance Zabbix NVPS (New Values Per Second) benchmark tool written in Go.

It handles the complete lifecycle of a stress test:

1. Auto-creates host groups, hosts and items via the Zabbix API
2. Floods the Zabbix Trapper with bulk metric packets
3. Auto-deletes all created resources when the test ends or is interrupted (Ctrl+C)

---

## Features

- Configurable metrics per host (1 to thousands) for variable load profiles
- 6 metric types cycled per host: Boolean, Unsigned, Float, Text, Character, Log
- Flood mode (`-rate 0`) sends metrics as fast as possible with no artificial delay
- Bulk Trapper packets pack multiple hosts per packet to maximize throughput
- Pre-generated value pool eliminates `rand` overhead in the hot loop
- Atomic error and latency tracking with zero lock contention
- Real-time progress reporting every 5 seconds (VPS, errors, latency)
- Latency percentiles (P50, P95, P99) for detailed performance analysis
- Per-worker statistics for identifying bottlenecks
- Error categorization (timeout, connection closed, network, other)
- JSON output export for analysis and CI/CD integration
- YAML configuration file support for complex setups
- Auto-detection of Trapper address from API URL
- Duration flag for automatic time-limited runs
- Graceful shutdown on Ctrl+C or SIGTERM with full cleanup
- Zabbix 7.0 compatible (supports username/password or API tokens)

---

## Requirements

- Go 1.23+ (for building from source)
- Zabbix API access (Admin or Super Admin account)
- Zabbix Trapper port accessible (default: 10051)

---

## Installation

### Download pre-built binaries

Grab the binary for your platform from the [latest release](https://github.com/washosk/zabbix-bench/releases/latest):

| Platform            | Binary                       |
|---------------------|------------------------------|
| Linux x86_64        | `zabbix-bench`               |
| Linux ARM64         | `zabbix-bench-linux-arm64`   |
| macOS Intel         | `zabbix-bench-darwin-amd64`  |
| macOS Apple Silicon | `zabbix-bench-darwin-arm64`  |
| Windows x86_64      | `zabbix-bench.exe`           |

```bash
# Example: Linux x86_64
curl -LO https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench
curl -LO https://github.com/washosk/zabbix-bench/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
chmod +x zabbix-bench
./zabbix-bench --help
```

### Build from source

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
go mod tidy
go build -o zabbix-bench main.go
```

---

## Usage

```
./zabbix-bench [flags]

Flags:
  -api-url          string    Zabbix API URL (default "http://localhost/zabbix/api_jsonrpc.php")
  -user             string    Zabbix username (default "Admin")
  -pass             string    Zabbix password (default: $ZABBIX_PASS or "zabbix")
  -api-key          string    Zabbix API token (default: $ZABBIX_API_KEY; skips user.login)
  -trapper-addr     string    Zabbix Trapper host:port (auto-detected from -api-url if not set)
  -hosts            int       Number of hosts to create (default 10)
  -prefix           string    Hostname prefix (default "bench-")
  -senders          int       Number of concurrent sender goroutines (default 10)
  -rate             int       Batches per second per host; 0 = flood mode (default 0)
  -batch-hosts      int       Hosts per bulk Trapper packet (default 50)
  -metrics-per-host int       Number of metrics to send per host (default 6)
  -duration         duration  Benchmark duration e.g. 30s, 2m (0 = run until Ctrl+C)
  -skip-setup       bool      Skip host/item creation, use hosts that already exist
  -keep-hosts       bool      Skip cleanup after test; keep hosts in Zabbix
  -group            string    Host Group name (default "Benchmark-Group")
  -config           string    YAML configuration file (CLI flags override config file values)
  -output-json      string    Export results as JSON to file
```

### Environment variables

| Variable         | Description                                                                                          |
|------------------|------------------------------------------------------------------------------------------------------|
| `ZABBIX_API_KEY` | Zabbix API token (Zabbix 5.4+). When set, skips `user.login` entirely.                               |
| `ZABBIX_PASS`    | Zabbix password. Used when `-pass` is not provided. Avoids exposing credentials in the process list.  |

---

## Authentication

Two methods are supported:

### Method 1: Username and Password (default)

```bash
./zabbix-bench -api-url "http://localhost:8080/api_jsonrpc.php" -user "Admin" -pass "zabbix" -hosts 10
```

Or use environment variable:

```bash
export ZABBIX_PASS="your-password"
./zabbix-bench -api-url "http://localhost:8080/api_jsonrpc.php" -user "Admin" -hosts 10
```

### Method 2: API Token (recommended)

Tokens are more secure and don't expose passwords. Requires Zabbix 5.4+.

**Create a token via Zabbix API:**

```bash
# 1. Login and get session
SESSION=$(curl -s -X POST "http://localhost:8080/api_jsonrpc.php" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"user.login","params":{"username":"Admin","password":"zabbix"},"id":1}' \
  | grep -o '"result":"[^"]*' | cut -d'"' -f4)

# 2. Create token
curl -s -X POST "http://localhost:8080/api_jsonrpc.php" \
  -H "Content-Type: application/json" \
  -d "{\"jsonrpc\":\"2.0\",\"method\":\"token.create\",\"params\":{\"name\":\"zabbix-bench\",\"userid\":\"1\"},\"auth\":\"$SESSION\",\"id\":1}"
```

**Use with zabbix-bench:**

```bash
export ZABBIX_API_KEY="<paste-token-value>"
./zabbix-bench -api-url "http://localhost:8080/api_jsonrpc.php" -hosts 10 -duration 30s
```

---

## Configuration files

Instead of passing many CLI flags, create a YAML config file:

```yaml
# benchmark.yaml
hosts: 100
senders: 20
metrics_per_host: 10
api_url: "http://localhost:8080/api_jsonrpc.php"
user: "Admin"
pass: "zabbix"
batch_hosts: 50
group: "LoadTest"
skip_setup: false
keep_hosts: false
```

Run with config file:

```bash
./zabbix-bench -config benchmark.yaml -duration 2m
```

CLI flags override config file values:

```bash
./zabbix-bench -config benchmark.yaml -hosts 50 -senders 10
```

---

## Output modes

### Console output

Includes real-time progress and final summary with latency percentiles and per-worker stats.

### JSON export

Export detailed results for analysis, CI/CD integration, or comparison:

```bash
./zabbix-bench -hosts 20 -duration 30s -output-json results.json
```

JSON file includes:

- Latency percentiles (P50, P95, P99)
- Per-worker statistics (packets, hosts, errors, min/max/avg latency)
- Error breakdown by category (timeout, connection, network, other)
- Full configuration used for the run

Example:

```bash
# Parse results with jq
cat results.json | jq '.throughput_vps, .p95_latency_ms, .errors_by_type'
```

---

## Quick start examples

### 30-second quick test

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -hosts 20 -senders 10 -batch-hosts 20 \
  -duration 30s
```

### High-volume stress test (500 metrics per host)

```bash
./zabbix-bench \
  -api-url "http://your-zabbix-server/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -hosts 100 -senders 50 -metrics-per-host 500 \
  -batch-hosts 100 -rate 0 -duration 5m
```

### Full saturation test (500 hosts, 2 minutes)

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -hosts 500 -senders 50 -batch-hosts 50 \
  -duration 2m
```

### Re-run without recreating hosts

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -hosts 500 -senders 50 -batch-hosts 50 \
  -skip-setup -duration 2m
```

### Use env var for password

```bash
export ZABBIX_PASS="my-secret"
./zabbix-bench -api-url "http://127.0.0.1:8080/api_jsonrpc.php" -hosts 20 -duration 30s
```

---

## Items created per host

Items are dynamically created based on `-metrics-per-host` (default: 6). The metric types cycle through:

| Cycle position | Type      | Zabbix value_type       | Example key                |
|----------------|-----------|-------------------------|----------------------------|
| 0              | Boolean   | 3 -- Numeric (unsigned) | `test.metric.0.bool`       |
| 1              | Unsigned  | 3 -- Numeric (unsigned) | `test.metric.1.unsigned`   |
| 2              | Float     | 0 -- Numeric (float)    | `test.metric.2.float`      |
| 3              | Text      | 4 -- Text               | `test.metric.3.text`       |
| 4              | Character | 1 -- Character          | `test.metric.4.char`       |
| 5              | Log       | 2 -- Log                | `test.metric.5.log`        |

With `-metrics-per-host 12`, items 6-11 repeat the same cycle (test.metric.6.bool, test.metric.7.unsigned, ...).

All items use Zabbix Trapper type (type=2), meaning they only accept pushed data with no polling overhead.

---

## Tuning tips

| Goal                     | Action                                                        |
|--------------------------|---------------------------------------------------------------|
| Max raw VPS              | Increase `-senders`, `-batch-hosts`, and `-metrics-per-host`  |
| Find DB bottleneck       | Watch Zabbix internal queue via `zabbix_server -R diaginfo`   |
| Reduce setup time        | Lower `-hosts` or use `-skip-setup` on repeat runs            |
| Keep data for inspection | Add `-keep-hosts` flag                                        |
| Avoid saturating prod    | Use `-rate N` instead of `-rate 0` to cap the send rate       |
| Test heavy item load     | Use `-metrics-per-host 500` to stress database writes         |

Use Ctrl+C or `kill <pid>` (SIGTERM) to stop. The tool cleans up hosts automatically. Avoid `kill -9` as it skips cleanup.

---

## Example benchmark results

Results from an Intel i7-1260P (16 threads), 46 GB DDR5, running Zabbix 7.0 on Docker with TimescaleDB (pg16).

### Default config (6 metrics per host, 50 hosts, 20 senders, 1 minute)

```text
Hosts: 50 | Senders: 20 | Batch: 50 | Flood: true | Duration: 1m0s

Throughput (VPS):    182,258
Avg latency:         2ms
P50 latency:         2ms
P95 latency:         4ms
P99 latency:         6ms
Total values:        10,939,980
Errors:              0 (0.0%)
```

### High-volume test (500 metrics per host, 100 hosts, 200 senders)

Tested against an AWS t3.large instance running Zabbix 7.0:

```text
Hosts: 100 | Senders: 200 | Batch: 100 | Metrics/host: 500 | Flood: true

Throughput (VPS):    ~1,700 (sustained)
Avg latency:         419ms
P50 latency:         206ms
P95 latency:         1,292ms
P99 latency:         2,260ms
Errors:              3 timeouts (0.0%)
```

With 500 metrics per host, the database becomes the bottleneck. This is useful for testing Zabbix server tuning and database write performance.

### 10-minute sustained test (50 hosts, 20 senders, 6 metrics)

```text
Duration:            10 minutes
Sustained VPS:       54,178
Peak VPS:            266,828
Total values:        32,500,000
Packets sent:        1,800,000
Errors:              0 (0.0%)
P99 latency:         5ms
```

Initial burst reaches 266k VPS, stabilizes around 54k sustained. Tight latency percentiles (P99 = 5ms) show consistent performance. Zero errors across 1.8M packets.

### Bottlenecks found via Zabbix internal API

| Process        | Before    | Peak busy % | After |
|----------------|-----------|-------------|-------|
| Trapper        | 5 workers | 98.45%      | 50    |
| History Syncer | 4 workers | 75.05%      | 20    |
| Write Cache    | 64M       | 99.29% full | 1G    |

### Optimized .env (Zabbix Server)

```dotenv
# Cache sizes
ZBX_CACHESIZE=256M
ZBX_HISTORYCACHESIZE=1G
ZBX_HISTORYINDEXCACHESIZE=128M
ZBX_TRENDCACHESIZE=64M
ZBX_TRENDCACHEFUNCTIONSIZE=32M
ZBX_VALUECACHESIZE=128M

# Process counts
ZBX_STARTPOLLERS=10
ZBX_STARTPOLLERSUNREACHABLE=5
ZBX_STARTPREPROCESSORS=12
ZBX_STARTTRAPPERS=50
ZBX_STARTHISTORYSYNCERS=20
ZBX_STARTPINGERS=4
ZBX_STARTDISCOVERERS=2
ZBX_STARTHTTPPOLLERS=2
ZBX_STARTSNMPTRAPPER=1
ZBX_STARTVMWARECOLLECTORS=0
ZBX_STARTJAVAPOLLERS=5
ZBX_TIMEOUT=10
ZBX_LOGSLOWQUERIES=3000
```

Apply with:

```bash
docker compose -f /opt/docker/zabbix/docker-compose.yaml --env-file /opt/docker/zabbix/.env up -d zabbix-server
```

---

## Monitoring Zabbix during benchmarks

While running zabbix-bench, monitor these Zabbix internal metrics to identify bottlenecks:

### Key metrics to watch

**Values processed per second** -- Shows actual throughput at server (not client-side estimate). Watch for plateaus indicating saturation.

**Internal process utilization** -- History Syncer writes values to database. Preprocessing handles item preprocessing. Watch for sustained >75% and increase worker count.

**Cache usage** -- History Cache for temporary storage, Write Cache for batched database writes. Flat line at 100% means values are being dropped.

**Queue size** -- Items waiting to be processed. Should remain near zero. Rising queue means the backend cannot keep up.

### How to monitor

```bash
# Get diagnostics
zabbix_server -R diaginfo

# Monitor in parallel terminal during benchmark
watch -n 5 'zabbix_server -R diaginfo | grep -E "Queue|Cache|busy"'
```

---

## Dependencies

- <https://github.com/claranet/go-zabbix-api> -- Zabbix API client
- <https://github.com/chmller/go-zabbix-sender> -- Zabbix Trapper sender

---

## License

[MIT](LICENSE)
