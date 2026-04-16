# zabbix-bench: High-Performance Zabbix Benchmarking & Stress Testing

[![Build & Test](https://github.com/washosk/zabbix-bench/actions/workflows/build.yml/badge.svg)](https://github.com/washosk/zabbix-bench/actions/workflows/build.yml)
[![Lint & Code Quality](https://github.com/washosk/zabbix-bench/actions/workflows/lint.yml/badge.svg)](https://github.com/washosk/zabbix-bench/actions/workflows/lint.yml)

`zabbix-bench` is a high-performance Zabbix benchmarking tool and load generator designed to measure ingest throughput and performance through the Zabbix Trapper path. It provides a structured way to perform stress testing and capacity planning for your Zabbix 7.0+ monitoring environment.

Built for repeatable **NVPS (New Values Per Second)** benchmarking, the tool automates the entire benchmark lifecycle. In a single run, it can:

- **Automated Setup**: Create benchmark host groups, hosts, and Trapper items via the Zabbix API.
- **Stress Testing**: Send bulk high-volume metric packets to the Zabbix Trapper.
- **Performance Analytics**: Report real-time throughput, latency percentiles (P50/P95/P99), and per-worker stats.
- **Export & Cleanup**: Export full benchmark results to JSON and remove all temporary benchmark resources.

Also useful for:

- capacity testing a new Zabbix deployment
- comparing tuning changes before and after server changes
- stress testing database-backed ingest paths
- building reproducible benchmark runs for CI, labs, or internal docs

---

## What the tool actually does

A normal run has three phases:

1. **Setup**

- logs into the Zabbix API using either username/password or an API token
- ensures the configured host group exists
- creates hosts named from the configured prefix and a zero-padded sequence, for example `bench-0001`
- creates Trapper items on each host

1. **Benchmark**

- splits the configured hosts across concurrent sender workers
- generates metric values in memory
- sends bulk packets to the Zabbix Trapper either as fast as possible or at a fixed rate
- tracks packet latency, throughput, and categorized errors

1. **Cleanup**

- unless `-keep-hosts` is set, deletes hosts in the benchmark group and then deletes the group itself

That last point matters: cleanup is intentionally aggressive.

---

## Important safety notes

Read this before pointing the tool at any shared environment.

- Use a **dedicated benchmark group name**. Do not reuse a production group.
- By default, cleanup deletes **all hosts returned by Zabbix for the configured benchmark group**, then removes the group itself.
- `-skip-setup` does **not** validate that every expected benchmark host already exists before sending starts. It reconstructs hostnames from the configured prefix and count.
- `kill -9` prevents graceful cleanup.
- If `-trapper-addr` is not set, the tool derives it from `-api-url`. For non-localhost API URLs it uses the same host with port `10051`.

Recommended first practice:

- use a unique group name such as `Benchmark-Group-2026-04-16`
- run a short test first
- use `-keep-hosts` on the first run if you want to inspect what was created

---

## Features

- **Scalable Load Generation**: Configurable host count, sender count, and metric density per host.
- **Diverse Data Simulation**: Six metric types cycled per host (Boolean, Unsigned, Float, Text, Character, Log).
- **Flood Mode**: High-volume pressure testing with `-rate 0`.
- **Intelligent Batching**: Bulk Trapper packets with host-based and metric-count batching for protocol efficiency.
- **Automated Lifecycle**: Zero-touch creation and removal of benchmark hosts/items via Zabbix API.
- **Real-time Performance Metrics**: Latency tracking with P50, P95, and P99 percentiles (O(1) efficiency).
- **Detailed Analytics**: Per-worker statistics including throughput (VPS), packet counts, and latency.
- **Advanced Error Tracking**: Categorized network and trap errors (timeouts, connection resets, etc.).
- **Extensible Export**: JSON results for integration with Grafana or other analysis dashboards.
- **Zabbix 7.0+ Ready**: Native support for API Token authentication and modern Zabbix API schemas.

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

### Download a release binary

Download the correct binary from the latest GitHub release.

Typical example:

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
```

Or fetch a release asset directly, for example on Linux:

```bash
curl -LO https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench-linux-amd64
chmod +x zabbix-bench-linux-amd64
./zabbix-bench-linux-amd64 --help
```

### Build from source

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
go mod tidy
go build -o zabbix-bench main.go
./zabbix-bench --help
```

---

## Command-line usage

```text
./zabbix-bench [flags]
```

### Flags

| Flag | Meaning | Default |
| --- | --- | --- |
| `-api-url` | Zabbix API URL | `http://localhost/zabbix/api_jsonrpc.php` |
| `-user` | Zabbix username | `Admin` |
| `-pass` | Zabbix password. If omitted, uses `ZABBIX_PASS`, then falls back to `zabbix` | none at flag level |
| `-api-key` | Zabbix API token. If set, skips `user.login` | none |
| `-trapper-addr` | Zabbix Trapper address in `host:port` form | auto-detected |
| `-hosts` | Number of hosts to create or address | `10` |
| `-prefix` | Host prefix used for generated names like `bench-0001` | `bench-` |
| `-senders` | Number of concurrent sender workers | `10` |
| `-rate` | Send frequency. `0` means flood mode | `0` |
| `-batch-hosts` | Target number of hosts per Trapper packet | `50` |
| `-batch-metrics` | Maximum total metrics per packet | `5000` |
| `-metrics-per-host` | Number of metrics sent per host per batch | `6` |
| `-duration` | Test duration such as `30s` or `2m`. `0` means run until interrupted | `0` |
| `-skip-setup` | Skip host and item creation, use the expected existing naming pattern | `false` |
| `-keep-hosts` | Skip cleanup after the run | `false` |
| `-group` | Host group name | `Benchmark-Group` |
| `-config` | YAML configuration file | none |
| `-output-json` | Write benchmark result JSON to a file | none |
| `-version` | Print version and exit | `false` |
| `-v` | Short form of `-version` | `false` |

### Environment variables

| Variable | Meaning |
| --- | --- |
| `ZABBIX_API_KEY` | API token used when `-api-key` is not provided |
| `ZABBIX_PASS` | Password used when `-pass` is not provided |

---

## Authentication

Two authentication paths are supported.

### Username and password

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -duration 30s
```

Or with an environment variable:

```bash
export ZABBIX_PASS="your-password"
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -duration 30s
```

### API token

If an API token is available, it is the cleaner option because the tool skips `user.login`.

```bash
export ZABBIX_API_KEY="your-api-token"
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -hosts 20 \
  -duration 30s
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

### Rate control

- `-rate 0` means flood mode
- any positive `-rate` uses a ticker in each worker

`-rate` is per-worker, not a global traffic shaper.

---

## Quick examples

### Small validation run

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Validation" \
  -hosts 10 \
  -senders 4 \
  -duration 30s
```

### Token-based run

```bash
export ZABBIX_API_KEY="your-token"
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -group "Benchmark-Group-Token" \
  -hosts 20 \
  -senders 10 \
  -duration 1m
```

### High metric density run

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-HeavyItems" \
  -hosts 100 \
  -senders 40 \
  -metrics-per-host 500 \
  -batch-hosts 100 \
  -batch-metrics 5000 \
  -duration 5m
```

### Reuse existing hosts

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Reuse" \
  -prefix "bench-" \
  -hosts 100 \
  -senders 20 \
  -skip-setup \
  -duration 2m
```

Use that only when:

- the expected hosts already exist
- they follow the same prefix and numbering scheme
- they already have matching Trapper items

### Export JSON results

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Json" \
  -hosts 20 \
  -duration 30s \
  -output-json results.json
```

---

## Output and result interpretation

The console summary includes:

- hosts tested
- total host sends
- total values
- total packets
- error count and error rate
- average, minimum, maximum, and percentile latencies
- per-worker packet, host, and error summaries

### What the terms mean

- **Hosts tested**: number of hostnames assigned to the benchmark run
- **Total host sends**: total successful host-batch placements across all packets
- **Total values**: `total host sends × metrics per host`
- **Total packets**: successful packet sends only
- **Throughput (VPS)**: total values divided by elapsed runtime
- **Latency**: end-to-end packet send and response time for successful sends
- **P50 / P95 / P99**: packet latency percentiles

### Important caveat on latency samples

Latency percentiles are calculated from sampled successful packet latencies, with an in-memory cap on the stored sample count. That is usually fine for benchmarking, but it is still sampling, not an infinite full-history ledger.

### JSON output structure

The JSON export includes:

- global totals
- latency percentiles
- categorized error counts
- per-worker stats
- a small config block used for the run

Useful for comparing runs over time, loading results into dashboards, and checking regressions after tuning.

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

### Very low or zero-looking average latency

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
