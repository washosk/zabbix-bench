# Quick start guide

Use this guide when you want a safe first run without reading the full documentation first.

For the full behavior, caveats, and tuning notes, read [`README.md`](README.md).

---

## Before you start

`zabbix-bench` is not just a packet generator.

A standard run can:

- create a host group
- create hosts and items through the Zabbix API
- send metrics through the Trapper
- delete the **specific** benchmark hosts and group it created afterward

That means you should use a **dedicated benchmark group name**.

### Safety first: dry run

Before making any changes to your Zabbix server, you can use `--dry-run` to preview exactly what the tool will do. This resolves all parameters and validates your configuration without any network impact.

```bash
./zabbix-bench -profile light -dry-run -api-url "http://zabbix/api_jsonrpc.php"
```

---

## What you need

You need all of the following:

- a reachable Zabbix API URL
- network access to the Zabbix Trapper port, usually `10051`
- either:
- a Zabbix username and password, or
- a Zabbix API token

Typical API URL format:

```text
http://your-zabbix-server/zabbix/api_jsonrpc.php
```

---

## Install

### Option 1: Download a release binary

```bash
curl -LO https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench
chmod +x zabbix-bench
./zabbix-bench --help
```

### Option 2: Build from source

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
go mod tidy
go build -o zabbix-bench main.go
./zabbix-bench --help
```

### Option 3: Docker

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
docker build -t zabbix-bench .
docker run --rm zabbix-bench --help
```

Replace `./zabbix-bench` with `docker run --rm zabbix-bench` in any example below. To save JSON output, mount a results directory:

```bash
docker run --rm \
  -e ZABBIX_API_KEY=your-token \
  -v "$(pwd)/results:/results" \
  zabbix-bench \
  -api-url "http://zabbix.example.com/zabbix/api_jsonrpc.php" \
  -hosts 10 \
  -duration 30s \
  -output-json /results/bench.json
```

If Zabbix runs on the same machine as Docker, add `--add-host=host.docker.internal:host-gateway` and use `host.docker.internal` as the hostname.

---

## Recommended: fast-track with profiles

The easiest way to start is with a built-in profile. Profiles provide sane defaults for hosts, senders, and rates.

| Profile | Hosts | Senders | Use Case |
| --- | --- | --- | --- |
| `light` | 25 | 10 | Local sanity checks / low-impact validation |
| `balanced` | 100 | 50 | Standard throughput and latency testing |
| `flood` | 300 | 200 | Intensive pressure and stress testing |

Example: Run a light 30-second benchmark on localhost:

```bash
./zabbix-bench \
  -profile light \
  -duration 30s \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix"
```

---

## Recommended first run

Use a small run and keep the created hosts so you can inspect them.

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-2026-04-26" \
  -hosts 10 \
  -senders 4 \
  -batch-hosts 10 \
  -duration 30s \
  -keep-hosts
```

What this does:

- logs into the Zabbix API
- creates a benchmark group if needed
- creates 10 hosts named `bench-0001` to `bench-0010`
- creates Trapper items on those hosts
- runs a 30-second benchmark
- prints a summary
- keeps the created resources because `-keep-hosts` is set

After you verify everything looks right, rerun without `-keep-hosts` if you want automatic cleanup of exactly those hosts.

---

## If you prefer an API token

```bash
export ZABBIX_API_KEY="your-token"

./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -group "Benchmark-Group-Quickstart-Token" \
  -hosts 10 \
  -senders 4 \
  -duration 30s \
  -keep-hosts
```

---

## Cleanup run

Once you are comfortable with the behavior, run without `-keep-hosts`:

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Quickstart" \
  -hosts 10 \
  -senders 4 \
  -batch-hosts 10 \
  -duration 30s
```

This will clean up exactly the resources created during this run when the benchmark ends.

---

## Minimal config file example

Create `benchmark.yaml`:

```yaml
api_url: "http://127.0.0.1:8080/api_jsonrpc.php"
user: "Admin"
pass: "zabbix"
group: "Benchmark-Group-Quickstart-Yaml"
hosts: 10
prefix: "bench-"
senders: 4
rate: 0
batch_hosts: 10
max_batch_size: 5000
metrics_per_host: 6
duration: "30s"
skip_setup: false
keep_hosts: true
output_json: "results.json"
profile: "light"
```

Run it:

```bash
./zabbix-bench -config benchmark.yaml
```

Important:

- the YAML key is `max_batch_size`
- the CLI flag is `-batch-metrics`

---

## What the summary means

At the end of the run you will see a summary like this:

```text
╔═════════════════════════════════════════════════════════╗
║              BENCHMARK SUMMARY REPORT                  ║
╠═════════════════════════════════════════════════════════╣
║ Hosts tested:        10                                ║
║ Total host sends:    327161                            ║
║ Total values:        1962966                           ║
║ Total packets:       137121                            ║
║ Total attempts:      137121                            ║
║ Errors:              0 (0.0%)                          ║
╠═════════════════════════════════════════════════════════╣
║ Throughput (VPS):    63501.22                          ║
║ Avg latency:         0 ms                              ║
║ Min latency:         0 ms                              ║
║ Max latency:         1001 ms                           ║
║ P50 latency:         0 ms                              ║
║ P95 latency:         0 ms                              ║
║ P99 latency:         1 ms                              ║
║ Latency samples:     137121                            ║
╠═════════════════════════════════════════════════════════╣
║ PARALLEL EXECUTION BREAKDOWN                            ║
║   Worker #00: 34280 pkts | 34280 hosts | 0 err | 1109 VPS ║
╚═════════════════════════════════════════════════════════╝
```

Quick interpretation:

- **Hosts tested**: how many hostnames were included in the run
- **Total host sends**: successful host sends across all packets
- **Total values**: successful host sends multiplied by metrics per host
- **Total packets**: successful packets sent
- **Throughput (VPS)**: values per second
- **P95 / P99 latency**: tail latency for successful packets

---

## Useful next commands

### Save results to JSON

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

### Increase load slightly

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Step2" \
  -hosts 50 \
  -senders 10 \
  -batch-hosts 25 \
  -duration 1m
```

### Reuse existing hosts

Only do this if matching hosts and items already exist.

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Step2" \
  -prefix "bench-" \
  -hosts 50 \
  -senders 10 \
  -skip-setup \
  -duration 1m
```

---

## Common mistakes

### Using a shared group name

Avoid this if possible, although version 1.7.2+ only deletes hosts it specifically created.

### Forgetting the YAML key name

In YAML, use:

```yaml
max_batch_size: 5000
```

Not:

```yaml
batch_metrics: 5000
```

### Expecting `-skip-setup` to create missing items

It does not. It assumes the hosts already exist and have matching Trapper items.

### Relying on auto-detected Trapper address in every topology

If your API host and Trapper host differ, set `-trapper-addr` explicitly.

If your topology includes a Zabbix Proxy, set `-trapper-addr` to point to the Proxy and use `-skip-setup` after registering and assigning the benchmark hosts to that Proxy in the frontend.

---

## Troubleshooting

### Connection refused

```text
dial tcp 127.0.0.1:10051: connect: connection refused
```

Check:

- the Trapper is enabled and reachable
- the host and port are correct
- firewalls are not blocking the connection

### Login failed

```text
error logging into Zabbix API: invalid username or password
```

Check:

- the API URL is correct
- credentials are correct
- `ZABBIX_PASS` is not set to an old value
- the token is valid if using token auth

### High error rate under load

Try:

- fewer senders
- fewer metrics per host
- smaller host batches
- a shorter test
- a positive `-rate` instead of flood mode

---

## Next step

Once the quickstart run works, move to [`README.md`](README.md) for:

- a full explanation of batching and rate semantics
- tuning guidance
- known caveats
- safer use of `-skip-setup`
- better interpretation of results
