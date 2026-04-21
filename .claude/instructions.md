# Interaction Instructions: zabbix-bench

## Development Workflow

### Build and Test

- **Language**: Go 1.24+
- **Build**: `go build -o zabbix-bench main.go`
- **Test**: `go test ./...` (currently one test file: main_test.go)
- **Linting**: `go vet ./...` and `gofmt -s -w .`
- **Docker**: `docker build -t zabbix-bench .` and `docker run --rm zabbix-bench`

### Version Management

- Version string is hardcoded in `main.go` (`var Version = "X.Y.Z"`)
- Update version when releasing:
  1. Edit Version string in main.go
  2. Update CHANGELOG.md with changes
  3. Tag commit: `git tag vX.Y.Z`
  4. Push tag: GitHub Actions builds and releases automatically

### Platform-Specific Builds

When releasing, build for multiple platforms:

```bash
# Linux x86_64
GOOS=linux GOARCH=amd64 go build -o zabbix-bench-linux-amd64

# macOS ARM64
GOOS=darwin GOARCH=arm64 go build -o zabbix-bench-darwin-arm64

# Windows x86_64
GOOS=windows GOARCH=amd64 go build -o zabbix-bench-windows-amd64.exe
```

## Code Standards

### Go Conventions

- Follow standard Go style with `gofmt -s -w .` before committing
- Run `go vet ./...` to catch errors
- Keep functions focused and add comments for exported functions
- Use package main and avoid external dependencies beyond:
  - `github.com/chmller/go-zabbix-sender` — Zabbix Trapper protocol
  - `github.com/kgeroczi/go-zabbix-api` — Zabbix API client
  - `gopkg.in/yaml.v3` — YAML config parsing

### Architectural Patterns

- **Config struct**: All configuration flags and defaults in `type Config`
- **Value pool pattern**: Pre-generate metric values in `ValuePool` to reduce allocation pressure during benchmark
- **Worker pattern**: Use goroutines with sync.WaitGroup for concurrent metric sending
- **Atomic counters**: Use atomic.* for thread-safe metrics collection (no mutexes)
- **Latency percentiles**: Use ring buffer (O(1) time) instead of sorting samples for P50/P95/P99 calculation

### Error Handling

- Categorize Trapper errors into: Timeout, Closed (connection reset), Network, Other
- Track per-worker error counts and latencies
- Do not fail on individual packet errors; continue sending and report aggregate statistics
- Validate API connectivity on startup with `-validate-only` flag

### Flag Design

- All flags have defaults in Config struct
- Support three configuration methods (in priority order):
  1. CLI flags: `-api-url`, `-user`, `-pass`, `-api-key`, `-hosts`, etc.
  2. Environment variables: (none currently, could add later)
  3. YAML config file: `zabbix-bench.yaml` in current directory
- Provide `-dry-run` flag to preview execution without network impact
- Provide performance profiles: `-profile light|balanced|flood` as shortcuts

## Key Functions and Data Flow

### Startup

1. `loadConfig()` — Parses flags and YAML config, applies defaults
2. `validateConfig()` — Checks for required parameters and connectivity
3. `dryRun()` — Prints execution plan without making changes

### Setup Phase

1. `client := zabbixapi.NewClient(apiURL, user, pass)` or token auth
2. `client.Call("hostgroup.create", ...)` — Create benchmark group
3. `client.Call("host.create", ...)` — Create hosts (name = prefix + padded index)
4. `client.Call("item.create", ...)` — Create Trapper items on each host
5. Store hostIDs for later cleanup

### Benchmark Phase

1. Instantiate `ValuePool` with pre-generated random metric values
2. Spawn `numSenders` goroutines (each is a `sender` worker)
3. Each worker:
   - Connects to Trapper (`net.Dial(trapperAddr)`)
   - Loops for configured duration, sending batches of metrics
   - Tracks latency, errors, throughput per worker
   - Maintains local counters for aggregation
4. Main thread collects metrics every second and prints live stats
5. On exit (timeout or Ctrl+C), calculate percentiles and aggregates

### Cleanup Phase

1. `client.Call("host.delete", ...)` — Delete benchmark hosts by hostID (unless `-keep-hosts`)
2. `client.Call("hostgroup.delete", ...)` — Delete group (unless `-keep-hosts`)
3. Export results to JSON if `-output-json` specified

## Testing

### Local Benchmark Testing

Test against a real Zabbix instance:

```bash
./zabbix-bench \
  -profile light \
  -api-url "http://localhost:8080/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -duration 30s \
  -output-json /tmp/result.json
```

Then inspect results:

```bash
cat /tmp/result.json | jq '.global_totals.throughput_vps'
```

### Unit Testing

Add tests to `main_test.go` for:
- Config parsing (flags, YAML, defaults)
- Value pool generation (verify diversity of generated values)
- Latency percentile calculation
- Error categorization
- Batch construction logic

### CI/CD

GitHub Actions workflows (in `.github/workflows/`):
- **build.yml** — Compiles for all platforms on every push
- **lint.yml** — Runs `go vet` and `gofmt` checks

## Release Checklist

Before tagging a release:

- [ ] Update Version string in main.go
- [ ] Update CHANGELOG.md with new features, fixes, improvements
- [ ] Verify `go test ./...` passes
- [ ] Verify `go vet ./...` passes
- [ ] Verify builds for Linux, macOS, Windows complete without errors
- [ ] Test basic functionality: `./zabbix-bench -dry-run -profile light`
- [ ] Commit and tag: `git tag vX.Y.Z`
- [ ] Push tag: `git push origin vX.Y.Z`
- [ ] GitHub Actions builds and creates release automatically
- [ ] Manually update Homebrew formula if applicable

## Common Development Tasks

### Adding a new flag

1. Add field to `Config` struct with YAML tag
2. Register flag in `func init()` or `flag.XxxVar()`
3. Add validation in `validateConfig()`
4. Document in README.md and CONTRIBUTING.md
5. Add test case if parsing logic changed

### Changing Trapper behavior

- Trapper protocol is in `zabbix-sender` dependency
- If protocol changes needed, check if dependency supports it
- Metric generation happens in batch construction loop in `sender()` worker
- Changes to batch format may affect latency and throughput

### Improving performance

- Profile with `time ./zabbix-bench ...` to identify bottlenecks
- Use `go tool pprof` if CPU profiling needed
- Benchmark current vs. optimized: measure VPS (values per second) improvement
- Document performance metrics in commit message

## Security Considerations

- **API credentials**: Stored in memory only, not logged or exported
- **Password handling**: Accepted via `-pass` flag or `-api-key` for token auth; never stored to disk
- **YAML config**: If used, do not commit files with passwords; use env vars or flags
- **Trapper data**: Metric values are synthetic and non-sensitive (suitable for stress testing)
- **No hardcoded hosts**: Always use `-api-url` flag or config; avoid localhost assumptions

