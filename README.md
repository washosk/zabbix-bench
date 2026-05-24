# zabbix-bench: High-Performance Zabbix Benchmarking & Stress Testing

[![Build & Test](https://github.com/washosk/zabbix-bench/actions/workflows/build.yml/badge.svg)](https://github.com/washosk/zabbix-bench/actions/workflows/build.yml)
[![Lint & Code Quality](https://github.com/washosk/zabbix-bench/actions/workflows/lint.yml/badge.svg)](https://github.com/washosk/zabbix-bench/actions/workflows/lint.yml)

`zabbix-bench` is a high-performance Zabbix benchmarking tool and load generator designed to measure ingest throughput and performance through the Zabbix Trapper path. It provides a structured way to perform stress testing and capacity planning for your Zabbix 7.0+ monitoring environment.

Built for repeatable **NVPS (New Values Per Second)** benchmarking, the tool automates the entire benchmark lifecycle. In a single run, it can:

- **Automated Setup**: Create benchmark host groups, hosts, and Trapper items via the Zabbix API.
- **Stress Testing**: Send bulk high-volume metric packets to the Zabbix Trapper.
- **Performance Analytics**: Report real-time throughput, latency percentiles (P50/P95/P99), and per-worker stats.
- **Export & Cleanup**: Export full benchmark results to JSON and remove exactly the resources created during the run.

Also useful for:

- capacity testing a new Zabbix deployment
- comparing tuning changes before and after server changes
- stress testing database-backed ingest paths
- building reproducible benchmark runs for CI, labs, or internal docs

---

## 🌟 Core Features

`zabbix-bench` manages the full lifecycle of a benchmark run through three automated phases:

1. **Setup**: Authenticates with the Zabbix API (Token or User/Pass), creates a dedicated host group, and populates it with Trapper items across multiple hosts.
2. **Benchmark**: Generates high-volume synthetic metric data in memory and floods the Zabbix Trapper with bulk packets across concurrent sender workers.
3. **Cleanup**: Automatically removes **only the hosts and group created during the session**, ensuring your environment remains clean.

### Key Capabilities

- **Scalable Load Generation**: Configurable host count, sender count, and metric density per host.
- **Diverse Data Simulation**: Cycles through 6 metric types (Boolean, Unsigned, Float, Text, Character, Log).
- **Intelligent Batching**: High-efficiency Trapper packets with host-based and metric-count constraints.
- **Real-time Analytics**: Live throughput (VPS) and O(1) latency tracking (P50, P95, P99).
- **Advanced Error Tracking**: Categorized network and protocol errors (timeouts, connection resets, etc.).
- **Zabbix 7.0+ Ready**: Native support for API Tokens, modern API schemas, and **Proxy Group redirects**.
- **Operational Safety**: Built-in `--dry-run` for plan previews and `--validate-only` for pre-flight connectivity checks.

---

## ⚠️ Important Safety Notes

Read this before pointing the tool at any shared environment:

- **Dedicated Groups**: Use a unique host group (e.g., `Benchmark-Production-Tuning`). Do not reuse production groups.
- **Selective Cleanup**: By default, the tool **only deletes resources created during the current run**. Pre-existing hosts in the same group are safe.
- **Graceful Exit**: Avoid `kill -9`. Use `Ctrl+C` to allow the tool to perform its automated cleanup.
- **Trapper Auto-detection**: If `-trapper-addr` is omitted, the tool assumes the Trapper is on port `10051` of the API host.
- **Dry Run First**: Always use `--dry-run` to verify your configuration before generating load.

---

## Requirements

To use `zabbix-bench` for performance testing, you need:

- **Zabbix Environment**: A reachable Zabbix 7.0+ API endpoint.
- **Trapper Access**: Network access to the Zabbix Trapper port (default `10051`).
- **Permissions**: API credentials with permissions to manage host groups, hosts, and items.
- **Build Tools**: Go 1.24+ (only if building from the source code).

The tool is optimized for modern Zabbix deployments using API tokens for secure, high-performance authentication.

---

## Installation

### 1. Download Release Binary (Easiest)

Download the latest binary for your platform from the [Releases](https://github.com/washosk/zabbix-bench/releases) page.

```bash
# Example for Linux AMD64
curl -LO https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench-linux-amd64
chmod +x zabbix-bench-linux-amd64
./zabbix-bench-linux-amd64 --help
```

### 2. Build from Source

Requires Go 1.24+.

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
go build -o zabbix-bench main.go
./zabbix-bench --help
```

### Docker

Build the image (about 6 MB, Go binary on Alpine):

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
docker build -t zabbix-bench .
```

Run a benchmark:

```bash
docker run --rm zabbix-bench \
  -api-url http://zabbix.example.com/zabbix/api_jsonrpc.php \
  -api-key your-token \
  -hosts 50 \
  -duration 1m
```

Pass credentials via environment variable instead of a flag:

```bash
docker run --rm \
  -e ZABBIX_API_KEY=your-token \
  zabbix-bench \
  -api-url http://zabbix.example.com/zabbix/api_jsonrpc.php \
  -hosts 50 \
  -duration 1m
```

Save JSON results by mounting a local directory:

```bash
docker run --rm \
  -e ZABBIX_API_KEY=your-token \
  -v "$(pwd)/results:/results" \
  zabbix-bench \
  -api-url http://zabbix.example.com/zabbix/api_jsonrpc.php \
  -hosts 50 \
  -duration 1m \
  -output-json /results/bench.json
```

Use a YAML config file:

```bash
docker run --rm \
  -v "$(pwd)/benchmark.yaml:/benchmark.yaml:ro" \
  zabbix-bench -config /benchmark.yaml
```

If your Zabbix server runs on the Docker host machine, use `--add-host`:

```bash
docker run --rm --add-host=host.docker.internal:host-gateway \
  -e ZABBIX_API_KEY=your-token \
  zabbix-bench \
  -api-url http://host.docker.internal/zabbix/api_jsonrpc.php \
  -hosts 50 \
  -duration 1m
```

---

---

## 📋 Full Command Line Reference

```text
Usage of zabbix-bench (version 1.7.2):

Example: zabbix-bench -api-url http://zabbix/api_jsonrpc.php -api-key your-token -hosts 50 -duration 1m

Options:
  -api-key string
     Zabbix API token (default: $ZABBIX_API_KEY; skips user.login)
  -api-url string
     Zabbix API URL (default "http://localhost/zabbix/api_jsonrpc.php")
  -batch-hosts int
     Number of hosts to pack into a single bulk Trapper packet (default 50)
  -batch-metrics int
     Maximum number of metrics per batch packet (default 5000)
  -config string
     YAML configuration file
  -dry-run
     Show execution plan and exit
  -duration duration
     Test duration, e.g. 30s, 2m (0 = run until Ctrl+C)
  -group string
     Host group name (default "Benchmark-Group")
  -hosts int
     Number of hosts to create (default 10)
  -keep-hosts
     Keep hosts after test (skip cleanup)
  -metrics-per-host int
     Number of metrics to send per host (default 6)
  -output-json string
     Output results as JSON to file
  -pass string
     Zabbix password (default: $ZABBIX_PASS or "zabbix")
  -prefix string
     Host prefix (default "bench-")
  -profile string
     Use a benchmarking profile (light, balanced, flood)
  -rate int
     Packets per second per worker (0=flood)
  -senders int
     Number of concurrent senders (default 10)
  -skip-setup
     Skip host/item creation (use existing hosts with same prefix)
  -trapper-addr string
     Zabbix Trapper address
  -user string
     Zabbix username (default "Admin")
  -v Print release version and exit
  -validate-only
     Perform pre-flight checks and exit
  -version
     Print release version and exit
```

### Environment variables

| Variable | Meaning |
| --- | --- |
| `ZABBIX_API_KEY` | API token used when `-api-key` is not provided |
| `ZABBIX_PASS` | Password used when `-pass` is not provided |

---

## 🚀 Quick Usage

### 1. Basic Auth (User/Pass)

```bash
./zabbix-bench -profile light -api-url "http://zabbix/api_jsonrpc.php" -user "Admin" -pass "zabbix"
```

### 2. Token Auth (Recommended)

```bash
# Using flag
./zabbix-bench -profile balanced -api-url "http://zabbix/api_jsonrpc.php" -api-key "your-token-here"

# Using environment variable
export ZABBIX_API_KEY="your-token"
./zabbix-bench -profile flood -duration 5m -api-url "http://zabbix/api_jsonrpc.php"
```

## 🛠️ Execution Modes

### 1. Dry Run (`-dry-run`)

Always recommended before a large benchmark. It validates credentials and displays the resolved execution plan without making any changes.

```text
╔═════════════════════════════════════════════════════════╗
║ RUN MODE: DRY RUN                                       ║
╠═════════════════════════════════════════════════════════╣
║ Auth:    User/Pass (user: Admin)                        ║
║ API:     http://localhost:8080/api_jsonrpc.php          ║
║ Trapper: 127.0.0.1:10051 (default)                      ║
║ Group:   Documentation-Example                          ║
╠═════════════════════════════════════════════════════════╣
║ Hosts:   5       | Senders: 2                         ║
║ Metrics: 6       | Batch:   50                        ║
║ Rate:    Fixed (1 packets/sec per worker)               ║
║ Duration: until interrupted                             ║
╠═════════════════════════════════════════════════════════╣
║ Setup:   true    | Cleanup: true                      ║
║ Warnings: 0                                             ║
╚═════════════════════════════════════════════════════════╝
```

### 2. Validation Only (`-validate-only`)

Performs a real login and tests the TCP connection to the Trapper port, then exits.

---

## 📊 Interpreting the Summary

At the end of each run, a detailed report is displayed:

```text
╔═════════════════════════════════════════════════════════╗
║              BENCHMARK SUMMARY REPORT                  ║
╠═════════════════════════════════════════════════════════╣
║ Hosts tested:        1                                 ║
║ Total host sends:    6919                              ║
║ Total values:        41514                             ║
║ Total packets:       6919                              ║
║ Total attempts:      6919                              ║
║ Errors:              0 (0.0%)                          ║
╠═════════════════════════════════════════════════════════╣
║ Throughput (VPS):    20739.04                          ║
║ Avg latency:         0 ms                              ║
║ Min latency:         0 ms                              ║
║ Max latency:         1 ms                              ║
║ P50 latency:         0 ms                              ║
║ P95 latency:         0 ms                              ║
║ P99 latency:         0 ms                              ║
║ Latency samples:     6919                              ║
╠═════════════════════════════════════════════════════════╣
║ PARALLEL EXECUTION BREAKDOWN                            ║
║   Worker #00: 6919 pkts | 6919 hosts | 0 err | 20753 VPS║
╚═════════════════════════════════════════════════════════╝
```

### Key Metrics Explained

- **Hosts tested**: Number of hostnames assigned to the benchmark run.
- **Total host sends**: Total successful host-batch placements across all packets.
- **Total values**: Calculated as `total host sends × metrics per host`.
- **Total packets**: Successful packet sends only.
- **Throughput (VPS)**: Values Per Second. This is the primary metric for Zabbix capacity (NVPS).
- **Latency**: End-to-end packet send and response time for successful sends.
- **P50 / P95 / P99**: Packet latency percentiles derived from up to 1,000,000 samples.
- **Worker Breakdown**: Helps identify if load is evenly distributed across your workers.

> [!NOTE]
> Latency percentiles are calculated from a sample cap of 1,000,000 packets to prevent unbounded memory growth. In high-throughput runs (~50k VPS), this cap is reached in about 20 seconds.

### Real-World Hardware Example: 104k NVPS on Google Cloud

To understand what Zabbix limits look like, here is an example of an **Endurance Test** ran against a Google Cloud VM:

*   **Infrastructure:** GCP `n2-standard-8` (8 vCPUs, 32 GB RAM) with 200 GB `pd-ssd`
*   **Load Parameters:** 300 Hosts, 200 Metrics per host, 200 Senders, 10-Minute Duration.

```text
╔═════════════════════════════════════════════════════════╗
║               BENCHMARK SUMMARY REPORT                  ║
╠═════════════════════════════════════════════════════════╣
║ Hosts tested:        300                                ║
║ Total values:        62793200                           ║
║ Errors:              20 (0.0%)                          ║
╠═════════════════════════════════════════════════════════╣
║ Throughput (VPS):    104436.66                          ║
║ Avg latency:         510 ms                             ║
║ Max latency:         5667 ms                            ║
║ P50 latency:         501 ms                             ║
║ P95 latency:         1238 ms                            ║
╚═════════════════════════════════════════════════════════╝
```

*At this exact threshold (~104k NVPS), the PostgreSQL History Syncers saturated the SSD IOPS, causing minor queuing (represented by the 5.6s max latency spike). Pushing the load higher caused severe thrashing, making this the documented sweet-spot maximum for this specific hardware.*

### Benchmarking Profiles

Profiles provide sensible defaults for common testing scenarios. Explicit CLI flags always override profile values.

| Profile | Hosts | Senders | Rate | Use Case |
| --- | --- | --- | --- | --- |
| `light` | 25 | 10 | 1 batch/s | Local sanity checks / low-impact validation |
| `balanced` | 100 | 50 | flood | Standard throughput and latency testing |
| `flood` | 300 | 200 | flood | Intensive pressure and stress testing |

Example using a profile with a local override:

```bash
./zabbix-bench -profile light -hosts 20 -duration 1m
```

---

## Configuration file

You can use a YAML file instead of passing many flags.

### Important naming note

The CLI flag and the YAML key are intentionally different for metric-batch sizing:

- CLI flag: `-batch-metrics`
- YAML key: `max_batch_size`

That is easy to miss, so use the YAML example below as-is.

### Example `benchmark.yaml`

```yaml
api_url: "http://127.0.0.1:8080/api_jsonrpc.php"
user: "Admin"
pass: "zabbix"
api_key: ""
trapper_addr: ""
group: "Benchmark-Group-Local"
hosts: 20
prefix: "bench-"
senders: 10
rate: 0
batch_hosts: 20
max_batch_size: 5000
metrics_per_host: 6
duration: "30s"
skip_setup: false
keep_hosts: false
output_json: "results.json"
dry_run: false
validate_only: false
profile: ""
```

Run it like this:

```bash
./zabbix-bench -config benchmark.yaml
```

CLI flags override config values:

```bash
./zabbix-bench -config benchmark.yaml -hosts 50 -senders 20 -duration 2m
```

---

## Safe first run

For a first run in a new environment, keep it small and use a unique group name.

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-FirstRun" \
  -hosts 10 \
  -senders 4 \
  -batch-hosts 10 \
  -duration 30s \
  -keep-hosts
```

This lets you:

- verify the API works
- verify the Trapper path is reachable
- inspect the generated hosts and items afterward
- confirm the host naming and item layout before you enable cleanup

When done inspecting, remove `-keep-hosts` or delete the group manually in Zabbix.

---

## Benchmark model

### Host naming

Hosts are created or expected using this pattern:

```text
<prefix><zero-padded index>
```

Examples with the default prefix:

```text
bench-0001
bench-0002
bench-0003
```

### Metric generation

Each host gets `-metrics-per-host` items. The metric types cycle in this order:

1. bool
2. unsigned
3. float
4. text
5. char
6. log

If you use more than six metrics per host, the cycle repeats.

Examples for `-metrics-per-host 8`:

```text
test.metric.0.bool
test.metric.1.unsigned
test.metric.2.float
test.metric.3.text
test.metric.4.char
test.metric.5.log
test.metric.6.bool
test.metric.7.unsigned
```

### Item types created

| Metric type | Zabbix `value_type` |
| --- | --- |
| bool | numeric unsigned |
| unsigned | numeric unsigned |
| float | numeric float |
| text | text |
| char | character |
| log | log |

All items are Trapper items.

### Worker model

The benchmarker splits the configured host list across sender workers. Each worker repeatedly sends batches for the subset of hosts assigned to it.

This means:

- `-senders` increases concurrency
- the load is host-slice based, not a global queue of independent packets
- per-worker stats can reveal imbalance or bottlenecks

### Batch sizing

Two settings affect packet composition:

- `-batch-hosts`
- `-batch-metrics`

The effective batch size starts from `-batch-hosts`, then is reduced when `-batch-metrics` would otherwise be exceeded.

Example:

- `-batch-hosts 50`
- `-metrics-per-host 200`
- `-batch-metrics 5000`

In that case, only `5000 / 200 = 25` hosts fit into a packet, so the effective batch becomes 25 hosts.

---

### JSON Output Structure

The JSON export (`-output-json`) includes global totals, latency percentiles, categorized error counts, and per-worker stats. It is ideal for loading results into dashboards or comparing performance regressions over time.

---

## Tuning guidance

| Goal | What to change |
| --- | --- |
| Increase raw ingest pressure | Raise `-senders`, `-batch-hosts`, or `-metrics-per-host` |
| Keep packet size under control | Lower `-batch-hosts` or `-batch-metrics` |
| Stress database writes | Raise `-metrics-per-host` significantly |
| Repeat runs without setup cost | Use `-skip-setup` only when hosts/items already exist |
| Avoid hammering the server too hard | Use a positive `-rate` instead of flood mode |
| Keep benchmark artifacts for inspection | Add `-keep-hosts` |
| Make comparisons easier | Export JSON and keep test parameters stable |

Suggested workflow:

1. start with 10 hosts and a short duration
2. confirm connectivity and cleanup behavior
3. increase `-senders`
4. increase `-metrics-per-host`
5. watch latency and Zabbix internal health in parallel

---

## Monitoring Zabbix during a run

Client-side numbers only show what the sender sees. While benchmarking, watch the Zabbix server too.

Things worth monitoring:

- values processed per second
- queue size
- history syncer utilization
- preprocessing utilization
- write cache and history cache usage
- database pressure and disk latency

Example command:

```bash
watch -n 5 'zabbix_server -R diaginfo | grep -E "Queue|Cache|busy"'
```

If client-side throughput stays high but queue or cache pressure rises, the backend may be the real bottleneck.

---

## Troubleshooting

### Connection refused to the Trapper

Example:

```text
dial tcp 127.0.0.1:10051: connect: connection refused
```

Check:

- the Zabbix server or proxy is listening on the expected Trapper port
- `-trapper-addr` points to the correct host and port
- firewalls are not blocking the connection

### API authentication fails

Example:

```text
error logging into Zabbix API: invalid username or password
```

Check:

- the API URL ends with `api_jsonrpc.php`
- the account has API access
- `ZABBIX_PASS` is not shadowing what you expect
- the API token is valid if using `-api-key` or `ZABBIX_API_KEY`

### Setup succeeds only partially

If the setup phase reports fewer ready hosts than requested, some host or item creations likely failed. Review the warnings in the setup logs.

### `-skip-setup` behaves oddly

Remember that `-skip-setup` assumes predictable hostnames and existing Trapper items. It does not create missing items and does not verify each host one by one before sending.

### Error rate grows under load

Likely causes:

- Trapper saturation
- network timeout
- database backpressure causing server-side rejection or slow response
- packets too large for the target environment

Things to try:

- lower `-senders`
- lower `-metrics-per-host`
- lower `-batch-hosts`
- lower `-batch-metrics`
- switch from flood mode to a positive `-rate`

### Validation Errors

If you provide invalid parameters, the tool will exit early with a clear explanation:

```text
❌ Validation Errors:
   - prefix must not be empty
   - hosts must be > 0
   - api_url is required
```

Check your command-line flags or YAML configuration file for missing or invalid values.

The tool records latency in milliseconds, so very fast local runs may show a lot of `0 ms` packet times. That does not mean the run is broken; it means the measured packet round-trip fell below 1 ms often enough to round down.

---

## Known caveats

- The setup phase creates a dummy host interface even though the benchmark uses Trapper items.
- Cleanup works at the **group** level, not only from the in-memory list of just-created hosts, if the group lookup succeeds.
- Trapper address auto-detection assumes the API host is also the Trapper host.
- Packet latency percentiles are based on successful sends only.
- The progress log reports values per second from successful host sends, not from attempted sends.

---

## Example workflow for repeatable testing

A repeatable approach:

1. create a unique group name for each test family
2. run a 30-second validation benchmark
3. export JSON
4. tune Zabbix
5. rerun with identical parameters
6. compare throughput, P95, P99, and error rate
7. only then increase pressure

Example pair of runs:

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Before" \
  -hosts 50 \
  -senders 20 \
  -metrics-per-host 50 \
  -duration 2m \
  -output-json before.json
```

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-After" \
  -hosts 50 \
  -senders 20 \
  -metrics-per-host 50 \
  -duration 2m \
  -output-json after.json
```

---

## Development notes

If you modify the benchmarker, keep docs aligned with these implementation details:

- YAML key names vs CLI flag names
- cleanup scope
- batch size calculation
- metric type cycle and item generation
- rate semantics
- JSON output structure

For this project, README accuracy matters because users will point it at real Zabbix instances.

---

## License

MIT
