# Project Memory: zabbix-bench

## Project Initialization: April 21, 2026

### .claude Setup Created

Comprehensive documentation structure created for zabbix-bench project:

- **instructions.md** — Development workflow for Go build, testing, version management, code standards, architectural patterns
- **project_context.md** — Project overview, three-phase architecture, data structures, dependencies, configuration methods, safety mechanisms
- **memory.md** — This file for session notes

## Project Overview

**Type**: Go benchmarking/stress testing tool for Zabbix 7.0+

**Purpose**: Measure NVPS (New Values Per Second) ingest capacity through Zabbix Trapper protocol

**Version**: 1.6.2 (as of April 2026)

**License**: Apache 2.0 (or check LICENSE file)

## Architecture Summary

### Three-Phase Execution

1. **Setup** — Create benchmark group, hosts, Trapper items via Zabbix API
2. **Benchmark** — Spawn workers, send metrics via Trapper, track throughput/latency
3. **Cleanup** — Delete hosts and group (unless `-keep-hosts` specified)

### Key Features

- Concurrent metric sending via goroutine workers
- Pre-generated metric value pools (reduce GC pressure)
- O(1) latency percentile calculation using ring buffer
- Categorized error tracking (timeout, closed, network, other)
- JSON export for analysis
- Dry-run and validate-only modes for safety

## Code Quality Standards

### Build & Test

- Build: `go build -o zabbix-bench main.go`
- Test: `go test ./...`
- Lint: `go vet ./...` and `gofmt -s -w .`
- CI/CD: GitHub Actions (build.yml, lint.yml)

### Architectural Patterns Used

- **Concurrent workers**: Goroutines + sync.WaitGroup for metric sending
- **Atomic counters**: sync/atomic for thread-safe stats (no mutexes)
- **Value pool pre-generation**: Reduce allocations during benchmark
- **Ring buffer latency**: O(1) percentile extraction without sorting
- **Config struct centralization**: All params in one struct (main.go)

## Dependencies

### External Packages

- `github.com/chmller/go-zabbix-sender` — Trapper protocol implementation
- `github.com/kgeroczi/go-zabbix-api` — Zabbix API client
- `gopkg.in/yaml.v3` — YAML config parsing

### Standard Library

- `net`, `sync`, `atomic` — Concurrent I/O and synchronization
- `encoding/json` — JSON export
- `flag`, `os` — CLI and environment

## Configuration Methods

### Priority Order

1. **CLI flags** (highest priority) — e.g., `-api-url`, `-user`, `-pass`, `-hosts`
2. **YAML config file** (medium) — `zabbix-bench.yaml` in current directory
3. **Environment variables** (not yet implemented)

### Performance Profiles

| Profile | Hosts | Senders | Metrics/Host | Rate |
| --- | --- | --- | --- | --- |
| light | 10 | 5 | 6 | 1000 VPS |
| balanced | 100 | 20 | 12 | 10000 VPS |
| flood | 1000 | 100 | 20 | unlimited |

## Safety Mechanisms

### Preventing Accidental Data Loss

1. **Unique group naming** — Use `Benchmark-Group-2026-04-21` instead of generic `Benchmark`
2. **Dry-run mode** — `-dry-run` previews without network changes
3. **Keep-hosts flag** — `-keep-hosts` preserves resources for inspection
4. **Startup confirmation** — Displays config and cleanup intent before running
5. **Aggressive cleanup by design** — Deletes all hosts in group (fail-safe)

### Important Caveats

- `-skip-setup` does not validate host existence; reconstructs from prefix/count
- Default cleanup deletes **all hosts** in configured group, then the group
- `kill -9` prevents graceful cleanup
- `-trapper-addr` defaults to same hostname as API URL but port 10051

## Known Limitations

- No histogram bucket support yet (latency distribution)
- No distributed benchmarking (single client only)
- No built-in comparison mode (before/after results)
- Metric types hardcoded to 6 types (Boolean, Unsigned, Float, Text, Character, Log)

## Release Checklist

Before tagging vX.Y.Z:

- [ ] Update Version string in main.go
- [ ] Update CHANGELOG.md
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] Cross-platform builds work (Linux, macOS, Windows)
- [ ] Basic test: `./zabbix-bench -dry-run -profile light`
- [ ] Commit, tag, push
- [ ] GitHub Actions builds automatically

## Integration Notes

### Zabbix Environment

- Requires Zabbix 7.0+ API
- Trapper protocol (UDP/TCP port 10051)
- API credentials with hostgroup, host, item management permissions

### Distribution

- **Homebrew** formula (if applicable)
- **AUR PKGBUILD** (if applicable)
- **Docker** image auto-built via GitHub Actions
- **Binaries** for Linux, macOS, Windows published on releases

## Pending / Future Work

- [ ] Extended metric tracking (histogram buckets)
- [ ] DEB/RPM package support
- [ ] Custom item type configuration
- [ ] Web UI dashboard
- [ ] Distributed benchmarking across multiple machines
- [ ] Database performance profiling
- [ ] CSV/HTML output formats
- [ ] Comparison mode for before/after results

## Related Repositories

- **share-nautobot** — Network discovery system (related Zabbix infrastructure)
- **share-zabbix-templates** — Monitoring templates for devices
- **Local Zabbix stack** — `/opt/docker/zabbix` deployment

