# zabbix-bench

A high-performance Zabbix NVPS (New Values Per Second) benchmark tool written in Go.

It handles the complete lifecycle of a stress test:

1. Auto-creates host groups, hosts and items via the Zabbix API
2. Floods the Zabbix Trapper with bulk metric packets
3. Auto-deletes all created resources when the test ends or is interrupted (Ctrl+C)

---

## Features

- Parallel host setup with throttled concurrency
- 6 metric types per host: Boolean, Unsigned, Float, Text, Character, Log
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
- Duration flag for automatic time-limited runs
- Graceful shutdown on Ctrl+C or SIGTERM with full cleanup
- Zabbix 7.0 compatible (supports username/password or API tokens)

---

## Requirements

- Go 1.24+ (for building from source)
- Zabbix API access (Admin or Super Admin account)
- Zabbix Trapper port accessible (default: 10051)

---

## Installation

### Download from releases

```bash
tar -xzf zabbix-bench.tgz
cd zabbix-bench
chmod +x zabbix-bench
./zabbix-bench --help
```

### Build from source

```bash
go mod tidy
go build -o zabbix-bench main.go
```

---

## Usage

```
./zabbix-bench [flags]

Flags:
  -api-url       string    Zabbix API URL (default "http://localhost/zabbix/api_jsonrpc.php")
  -user          string    Zabbix username (default "Admin")
  -pass          string    Zabbix password (default: $ZABBIX_PASS or "zabbix")
  -api-key       string    Zabbix API token (default: $ZABBIX_API_KEY; skips user.login)
  -trapper-addr  string    Zabbix Trapper host:port (default "127.0.0.1:10051")
  -hosts         int       Number of hosts to create (default 10)
  -prefix        string    Hostname prefix (default "bench-")
  -senders       int       Number of concurrent sender goroutines (default 10)
  -rate          int       Batches per second per host; 0 = flood mode (default 0)
  -batch-hosts   int       Hosts per bulk Trapper packet (default 50)
  -duration      duration  Benchmark duration e.g. 30s, 2m (0 = run until Ctrl+C)
  -skip-setup    bool      Skip host/item creation, use hosts that already exist
  -keep-hosts    bool      Skip cleanup after test; keep hosts in Zabbix
  -group         string    Host Group name (default "Benchmark-Group")
  -config        string    YAML configuration file (CLI flags override config file values)
  -output-json   string    Export results as JSON to file
```

### Environment variables

| Variable         | Description                                                                                          |
|------------------|------------------------------------------------------------------------------------------------------|
| `ZABBIX_API_KEY` | Zabbix API token (Zabbix 5.4+). When set, skips `user.login` entirely.                               |
| `ZABBIX_PASS`    | Zabbix password. Used when `-pass` is not provided. Avoids exposing credentials in the process list.  |

---

## Authentication

Two methods are supported:

### Method 1: Username & Password (default)

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

**Retrieve token from Zabbix UI:**

1. Open Zabbix web UI → **Administration > API tokens**
2. Find and click the token you created
3. Copy the token value
4. Use with zabbix-bench:

```bash
export ZABBIX_API_KEY="<paste-token-value>"
./zabbix-bench -api-url "http://localhost:8080/api_jsonrpc.php" -hosts 10 -duration 30s
```

**Benefits of API tokens:**

- No password exposure in logs or process list
- Can set expiration dates
- Limited scope (read-only option available)
- Better for automated/CI/CD environments

---

## Configuration files

Instead of passing many CLI flags, create a YAML config file:

```yaml
# benchmark.yaml
hosts: 100
senders: 20
api_url: "http://localhost:8080/api_jsonrpc.php"
user: "Admin"
pass: "zabbix"
trapper_addr: "127.0.0.1:10051"
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
- Full configuration used

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
  -trapper-addr "127.0.0.1:10051" \
  -hosts 20 -senders 10 -batch-hosts 20 \
  -duration 30s
```

Example output:
```
2026/04/09 22:18:15 === SETUP PHASE ===
2026/04/09 22:18:15 Logged into Zabbix API.
2026/04/09 22:18:15 Host Group: Benchmark-Group (ID: 27)
2026/04/09 22:18:15 Creating 20 hosts in parallel (concurrency=5)...
2026/04/09 22:18:19 Setup complete. 20/20 hosts ready.
2026/04/09 22:18:19 === BENCHMARK PHASE ===
2026/04/09 22:18:19 Hosts: 20 | Senders: 10 | Batch: 20 | Flood: true | Duration: 30s
2026/04/09 22:18:24 [     5s]    84736 batches |  101682.28 VPS (inst: 101683.20) | errors: 0 (0.0%)
2026/04/09 22:18:29 [    10s]   147308 batches |   88367.50 VPS (inst: 75086.40)  | errors: 0 (0.0%)
2026/04/09 22:18:34 [    15s]   198684 batches |   79456.49 VPS (inst: 61651.20)  | errors: 0 (0.0%)
2026/04/09 22:18:39 [    20s]   215104 batches |   64527.53 VPS (inst: 19704.00)  | errors: 0 (0.0%)
...

╔══════════════════════════════════════════╗
║         BENCHMARK SUMMARY REPORT         ║
╠══════════════════════════════════════════╣
║  Hosts tested:     20                    ║
║  Total batches:    233908                ║
║  Total values:     1403448               ║
║  Packets sent:     11695                 ║
║  Errors:           0                (0.0%)║
║  Avg latency/pkt:  0                  ms ║
╚══════════════════════════════════════════╝
2026/04/09 22:18:45 === CLEANUP PHASE ===
2026/04/09 22:18:45 Deleting 20 hosts in group (queried from Zabbix)...
2026/04/09 22:18:45 Deleting Host Group 'Benchmark-Group'...
2026/04/09 22:18:45 Cleanup complete.
```

### Full saturation test (500 hosts, 2 minutes)

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -trapper-addr "127.0.0.1:10051" \
  -hosts 500 -senders 50 -batch-hosts 50 \
  -duration 2m
```

### Re-run without recreating hosts

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -trapper-addr "127.0.0.1:10051" \
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

| Key              | Name      | Zabbix value_type       |
|------------------|-----------|-------------------------|
| `test.bool`      | Boolean   | 3 -- Numeric (unsigned) |
| `test.unsigned`  | Unsigned  | 3 -- Numeric (unsigned) |
| `test.float`     | Float     | 0 -- Numeric (float)    |
| `test.text`      | Text      | 4 -- Text               |
| `test.char`      | Character | 1 -- Character          |
| `test.log`       | Log       | 2 -- Log                |

All items use Zabbix Trapper type (type=2), meaning they only accept pushed data with no polling overhead.

---

## Tuning tips

| Goal                     | Action                                                      |
|--------------------------|-------------------------------------------------------------|
| Max raw VPS              | Increase `-senders` and `-batch-hosts`                      |
| Find DB bottleneck       | Watch Zabbix internal queue via `zabbix_server -R diaginfo` |
| Reduce setup time        | Lower `-hosts` or use `-skip-setup` on repeat runs          |
| Keep data for inspection | Add `-keep-hosts` flag                                      |
| Avoid saturating prod    | Use `-rate N` instead of `-rate 0` to cap the send rate     |

Use Ctrl+C or `kill <pid>` (SIGTERM) to stop. The tool cleans up hosts automatically. Avoid `kill -9` as it skips cleanup.

---

## Example benchmark results

Results from an Intel i7-1260P (16 threads), 46 GB DDR5, running Zabbix 7.0 on Docker with TimescaleDB (pg16):

### Before tuning (default Zabbix config)

| Metric | Value |
|--------|-------|
| Peak VPS | 107,844 |
| Sustained VPS | ~79,000 |
| Total values (2min) | 9,573,000 |
| Errors | 0 (0.0%) |
| Avg latency | 3ms |

### After tuning

| Metric | Value |
|--------|-------|
| Peak VPS | 150,036 |
| Sustained VPS | ~106,000 |
| Total values (2min) | 12,726,960 |
| Errors | 0 (0.0%) |
| Avg latency | 2ms |

Net improvement: +34% sustained VPS, +39% peak VPS, -33% latency.

### 10-minute sustained test (50 hosts, 20 senders)

Extended benchmark showing sustained performance over time:

**Configuration:**
```bash
./zabbix-bench -hosts 50 -senders 20 -batch-hosts 50 -duration 10m
```

**Summary results:**

| Metric | Value |
|--------|-------|
| Duration | 10 minutes |
| Hosts tested | 50 |
| Total batches | 5.4M |
| Total values sent | 32.5M |
| Packets sent | 1.8M |
| Sustained VPS (at 10min) | 54,178 |
| Peak VPS | 266,828 |
| Errors | 0 (0.0%) |
| Avg latency | 5ms |
| P50 latency | 1ms |
| P95 latency | 3ms |
| P99 latency | 5ms |

**Key observations:**

- Initial burst reaches 266k VPS, stabilizes around 54k sustained
- Tight latency percentiles (P99 = 5ms) indicate consistent performance
- 20 workers maintain balanced load (avg 107k packets per worker)
- Zero errors across 1.8M packets demonstrates reliability

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

## Monitoring Zabbix During Benchmarks

While running zabbix-bench, monitor these Zabbix internal metrics to identify bottlenecks:

### Key metrics to watch

#### 1. Values processed per second

- Shows actual throughput at server (not client-side estimate)
- Peak reflects network burst capacity
- Sustained rate shows database write capability
- Watch for plateaus indicating saturation

#### 2. Data collector utilization

- % busy time across poller workers
- Should stay below 80% for headroom
- High utilization → add more pollers or reduce load
- In benchmark: not relevant (no polling, only trapping)

#### 3. Internal process utilization

- History Syncer: writes values to database
- Alert Manager: triggers alerts
- Preprocessing: handles item preprocessing
- Watch for sustained >75% → increase worker count

#### 4. Cache usage

- History Cache: temporary storage for new values
- Value Cache: recent item values
- Write Cache: batched database writes
- Rising trend → increase cache size or reduce load
- Flat line at 100% = values being dropped

#### 5. Queue size

- Items waiting to be processed
- Should remain near zero
- Rising queue = backend cannot keep up
- Indicates need for:
  - More History Syncer workers
  - Larger write cache
  - Database optimization

#### 6. Value cache effectiveness

- Hit rate % (higher is better)
- Low rate = cache misses, more database queries
- Optimize by increasing cache size

### How to monitor

#### Via Zabbix UI

1. Administration > Diagnostics > Internal API
2. View real-time server status
3. Monitor "Status of Zabbix" dashboard

#### Via CLI

```bash
# Get diagnostics
zabbix_server -R diaginfo

# Output includes:
# - Queue statistics
# - Cache utilization
# - Process busy %
# - Value processing rate
```

#### During benchmark

```bash
# Monitor in parallel terminal
watch -n 5 'zabbix_server -R diaginfo | grep -E "Queue|Cache|busy"'
```

### Example findings from 10-minute test

During sustained 54k VPS load:

- Trapper workers: ~40% busy (well under 80% limit)
- History Syncer: ~30% busy (scaling well)
- Write Cache: ~45% full (plenty of headroom)
- Queue: < 100 items (healthy)
- Value Cache hit rate: 95%+ (effective)

This indicates the server had significant headroom for higher loads.

---

## Dependencies

- [github.com/claranet/go-zabbix-api](https://github.com/claranet/go-zabbix-api) -- Zabbix API client
- [github.com/chmller/go-zabbix-sender](https://github.com/chmller/go-zabbix-sender) -- Zabbix Trapper sender

---

## License

[MIT](LICENSE)
