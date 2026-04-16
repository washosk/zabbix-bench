# Quick Start Guide

Use this guide when you want a safe first run without reading the full documentation first.

For the full behavior, caveats, and tuning notes, read [`README.md`](README.md).

---

## Before you start

`zabbix-bench` is not just a packet generator.

A standard run can:

- create a host group
- create hosts and items through the Zabbix API
- send metrics through the Trapper
- delete the benchmark hosts and group afterward

That means you should use a **dedicated benchmark group name**.

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

---

## Recommended first run

Use a small run and keep the created hosts so you can inspect them.

```bash
./zabbix-bench \
  -api-url "http://127.0.0.1:8080/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -group "Benchmark-Group-Quickstart" \
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

After you verify everything looks right, rerun without `-keep-hosts` if you want automatic cleanup.

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

This will clean up the benchmark resources when the run ends.

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
║ Errors:              0 (0.0%)                          ║
╠═════════════════════════════════════════════════════════╣
║ Throughput (VPS):    63501.22                          ║
║ Avg latency:         0 ms                              ║
║ Min latency:         0 ms                              ║
║ Max latency:         1001 ms                           ║
║ P50 latency:         0 ms                              ║
║ P95 latency:         0 ms                              ║
║ P99 latency:         1 ms                              ║
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

Avoid this. Cleanup operates at the benchmark group level.

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
