# Quick Start Guide

Get zabbix-bench up and running in minutes.

---

## Installation

### Option 1: Download Pre-built Binary

```bash
# Linux x86_64
wget https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench-linux-amd64
chmod +x zabbix-bench-linux-amd64

# Or macOS (Intel)
wget https://github.com/washosk/zabbix-bench/releases/latest/download/zabbix-bench-darwin-amd64
chmod +x zabbix-bench-darwin-amd64
```

### Option 2: Homebrew (macOS/Linux)

```bash
brew tap washosk/zabbix-bench
brew install zabbix-bench
```

### Option 3: Build from Source

```bash
git clone https://github.com/washosk/zabbix-bench.git
cd zabbix-bench
go build -o zabbix-bench main.go
```

### Option 4: Go Install

```bash
go install github.com/washosk/zabbix-bench@latest
```

---

## Prerequisites

1. **Zabbix Server** running and accessible
   - API endpoint: `http://your-zabbix-server/zabbix/api_jsonrpc.php`
   - Trapper port: 10051 (default)

2. **Zabbix Admin Account**
   - Default: `Admin` / `zabbix`
   - Or an API token (Zabbix 5.4+)

3. **Network Access**
   - Can reach Zabbix API URL
   - Can reach Zabbix Trapper port

---

## Basic Usage

### 1. Simple test (30 seconds)

```bash
./zabbix-bench \
  -api-url "http://localhost/zabbix/api_jsonrpc.php" \
  -user "Admin" \
  -pass "zabbix" \
  -duration 30s
```

**Output:**

- Console: Real-time progress and summary
- Creates 10 temporary hosts
- Sends metrics via Trapper
- Cleans up automatically

### 2. Larger test (2 minutes)

```bash
./zabbix-bench \
  -hosts 50 \
  -senders 20 \
  -batch-hosts 50 \
  -duration 2m
```

**What it does:**

- Creates 50 test hosts
- Uses 20 concurrent senders
- Batches 50 hosts per packet
- Runs for 2 minutes
- Reports P50, P95, P99 latencies

### 3. With API token (more secure)

```bash
export ZABBIX_API_KEY="your-api-token-here"
./zabbix-bench \
  -api-url "http://localhost/zabbix/api_jsonrpc.php" \
  -hosts 20 \
  -duration 1m
```

### 4. Save results to JSON

```bash
./zabbix-bench \
  -hosts 20 \
  -duration 30s \
  -output-json benchmark-results.json

# View results
cat benchmark-results.json | jq '.'
```

---

## Configuration File

Create `benchmark.yaml`:

```yaml
api_url: "http://localhost/zabbix/api_jsonrpc.php"
user: "Admin"
pass: "zabbix"
hosts: 50
senders: 20
batch_hosts: 50
trapper_addr: "127.0.0.1:10051"
group: "LoadTest"
duration: "2m"
```

Run with:

```bash
./zabbix-bench -config benchmark.yaml -duration 2m
```

---

## Understanding Output

### Console output example

```text
╔═════════════════════════════════════════════════════════╗
║               BENCHMARK SUMMARY REPORT                  ║
╠═════════════════════════════════════════════════════════╣
║ Hosts tested:        10                                 ║
║ Total host sends:    327161                             ║
║ Total values:        1962966                            ║
║ Total packets:       137121                             ║
║ Errors:              0 (0.0%)                           ║
╠═════════════════════════════════════════════════════════╣
║ Throughput (VPS):    63501.22                           ║
║ Avg latency:         0 ms                               ║
║ Min latency:         0 ms                               ║
║ Max latency:         1001 ms                            ║
║ P50 latency:         0 ms                               ║
║ P95 latency:         0 ms                               ║
║ P99 latency:         1 ms                               ║
╠═════════════════════════════════════════════════════════╣
║ Per-worker statistics:                                  ║
║   W0: 31638 pkts, 94914 hosts, 0 err, 18423 VPS         ║
║   W1: 31708 pkts, 95124 hosts, 0 err, 18463 VPS         ║
╠═════════════════════════════════════════════════════════╣
╚═════════════════════════════════════════════════════════╝
```

**What it means:**

- **VPS**: Values per second sent
- **Latency**: Response time from Zabbix Trapper
- **P50/P95/P99**: Percentiles (50% of packets at or below P50ms, etc.)
- **Worker stats**: Individual sender goroutine performance

### JSON output example

```json
{
  "duration_seconds": 30.91,
  "hosts_tested": 10,
  "total_host_sends": 327161,
  "total_values": 1962966,
  "total_packets": 137121,
  "error_count": 0,
  "error_rate_percent": 0,
  "throughput_vps": 63501.22,
  "avg_latency_ms": 0,
  "min_latency_ms": 0,
  "max_latency_ms": 1001,
  "p50_latency_ms": 0,
  "p95_latency_ms": 0,
  "p99_latency_ms": 1,
  "errors_by_type": {
    "timeout": 0,
    "closed": 0,
    "network": 0,
    "other": 0,
    "total": 0
  },
  "worker_stats": [
    {
      "worker_id": 0,
      "packets_sent": 31638,
      "hosts_sent": 94914,
      "errors": 0,
      "total_latency_ms": 17941,
      "min_latency_ms": 0,
      "max_latency_ms": 1001,
      "avg_latency_ms": 0
    }
  ],
  "config": {
    "batch_hosts": 50,
    "hosts": 10,
    "metrics_per_host": 6,
    "rate": 0,
    "senders": 4,
    "trapper_addr": "127.0.0.1:10051"
  }
}
```

---

## Common Scenarios

### Scenario 1: Find server capacity

```bash
# Start small
./zabbix-bench -hosts 10 -senders 5 -duration 1m

# Gradually increase
./zabbix-bench -hosts 50 -senders 20 -duration 1m
./zabbix-bench -hosts 100 -senders 40 -duration 1m

# Stop when errors appear or latency spikes
```

### Scenario 2: Sustained load test

```bash
./zabbix-bench \
  -hosts 50 \
  -senders 20 \
  -rate 50000 \
  -duration 10m
```

- Fixed rate: 50,000 batches/sec
- Watch P99 latency for degradation
- Verify no errors over time

### Scenario 3: Stress test (find breaking point)

```bash
./zabbix-bench \
  -hosts 200 \
  -senders 100 \
  -batch-hosts 10 \
  -duration 5m
```

- No rate limit (flood mode)
- Many hosts and senders
- Stop when seeing errors
- Note: may saturate Zabbix or network

### Scenario 4: API token authentication

```bash
# 1. Create token in Zabbix UI
# Admin > API tokens > Create

# 2. Set environment variable
export ZABBIX_API_KEY="your-token-here"

# 3. Run benchmark (no password needed)
./zabbix-bench \
  -api-url "http://localhost/zabbix/api_jsonrpc.php" \
  -hosts 20 \
  -duration 30s
```

---

## Troubleshooting

### Connection refused

```text
Error: dial tcp 127.0.0.1:10051: connect: connection refused
```

**Solution:**

- Verify Zabbix is running: `curl http://localhost/zabbix/`
- Check Trapper port: `telnet localhost 10051`
- Verify firewall allows port 10051

### Authentication failed

```text
Error logging into Zabbix API: invalid username or password
```

**Solution:**

- Verify credentials: `Admin` / `zabbix`
- Check API URL ends with `api_jsonrpc.php`
- Try API token instead: `-api-key "token-here"`

### High error rate

```text
Errors: 1234 (5.3%)
```

**Solution:**

- Reduce load: fewer hosts or senders
- Increase rate limit: `-rate 1000` instead of `-rate 0`
- Check Zabbix logs for capacity issues
- Monitor Zabbix processes: `zabbix_server -R diaginfo`

### Out of memory

```text
panic: runtime error: memory allocation failed
```

**Solution:**

- Reduce number of hosts/senders
- Use shorter duration
- Check system resources: `free -h`, `top`

---

## Tips for Best Results

1. **Warm up Zabbix:**
   - Run small test first
   - Allows caches to initialize

2. **Monitor Zabbix:**
   - Watch CPU, memory, disk I/O
   - Check `Administration > Diagnostics`
   - Monitor queue size and cache usage

3. **Use configuration file:**
   - Reusable across tests
   - Easier to track settings
   - Good for CI/CD

4. **Save JSON results:**
   - Compare runs over time
   - Analyze trends
   - Integrate with monitoring

5. **Run multiple tests:**
   - Results vary with system load
   - Average 3-5 runs
   - Use comparable settings

---

## Next Steps

- Read [README.md](README.md) for full documentation
- Check [DISTRIBUTION.md](DISTRIBUTION.md) for package manager setup
- See [CONTRIBUTING.md](CONTRIBUTING.md) if you want to contribute
- Review [CHANGELOG.md](CHANGELOG.md) for version history

---

## Need Help?

- Open an issue on [GitHub](https://github.com/washosk/zabbix-bench/issues)
- Check existing documentation
- Review example configurations

Happy benchmarking.
