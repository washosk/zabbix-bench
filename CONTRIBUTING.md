# Contributing to zabbix-bench

Thank you for your interest in contributing! This document provides guidelines and instructions.

---

## Code of Conduct

Be respectful, inclusive, and professional. We're all here to improve zabbix-bench.

---

## Getting Started

### 1. Fork and clone

```bash
gh repo fork washosk/zabbix-bench --clone
cd zabbix-bench
```

### 2. Set up development environment

```bash
go mod download
go mod tidy

# Verify build
go build -o zabbix-bench main.go

# Run linting
go vet ./...
gofmt -s -w .
```

### 3. Create a feature branch

```bash
git checkout -b feature/your-feature-name
```

---

## Development Guidelines

### Code style

- Follow standard Go conventions
- Run `gofmt -s -w .` before committing
- Run `go vet ./...` to check for errors
- Keep functions focused and well-documented
- Add comments for exported functions

### Testing

- Test locally before pushing
- Add tests for new features
- Ensure all tests pass

```bash
go test ./...
go build main.go && ./zabbix-bench --help
```

### Commit messages

Use clear, descriptive commit messages:

```bash
Add JSON output export feature

- Implement -output-json flag
- Marshal BenchmarkResult to JSON
- Add example in documentation

Fixes #123
```

### Pull request process

1. Update documentation (README, DISTRIBUTION.md, etc.)
1. Ensure CI/CD workflows pass
1. Request review from maintainers
1. Address feedback

---

## Types of Contributions

### Bug fixes

```bash
git checkout -b fix/issue-description
# Make changes
# Test thoroughly
git commit -m "Fix: description of bug fix"
git push origin fix/issue-description
# Open PR with reference to issue
```

### Features

```bash
git checkout -b feature/new-feature
# Implement feature
# Add tests
# Update README
git commit -m "Add: description of new feature"
git push origin feature/new-feature
# Open PR
```

### Documentation

```bash
git checkout -b docs/improvement
# Edit README.md, DISTRIBUTION.md, or CONTRIBUTING.md
git commit -m "Docs: description of changes"
git push origin docs/improvement
# Open PR
```

### Performance improvements

```bash
git checkout -b perf/optimization
# Benchmark before and after
# Include metrics in commit message
git commit -m "Perf: optimization description

Before: X VPS
After: Y VPS
Improvement: +Z%"
```

---

## Areas for Contribution

### High priority

- [ ] Extended metric tracking (histogram buckets)
- [ ] More package manager support (DEB, RPM)
- [ ] Custom item type configuration beyond the 6 built-in types

### Medium priority

- [ ] Web UI dashboard for real-time monitoring
- [ ] Distributed benchmarking (multiple clients)
- [ ] Database performance profiling
- [ ] Integration tests with real Zabbix

### Lower priority

- [ ] Additional output formats (CSV, HTML)
- [ ] Comparison mode between runs
- [ ] Python/Bash port
- [ ] Warmup phase option

---

## Reporting Issues

### Security issues

Email security concerns privately to the maintainer rather than opening public issues.

### Bug reports

Include:

- Go version (`go version`)
- Zabbix version
- Command used
- Error message or unexpected behavior
- Steps to reproduce

Example:

```markdown
**Zabbix version:** 7.0
**Go version:** 1.24
**OS:** Linux Debian 13

**Command:**
./zabbix-bench -hosts 10 -duration 10s

**Error:**
panic: runtime error: index out of range

**Expected behavior:**
Should complete benchmark successfully
```

### Feature requests

Describe:

- Use case
- Why it's needed
- Potential implementation approach

---

## Review Process

1. **Automatic checks:**

- CI/CD workflows pass
- Code is formatted
- No lint errors

1. **Maintainer review:**

- Code quality
- Performance impact
- Documentation completeness
- Test coverage

1. **Merge:**

- All checks pass
- At least one approval
- PR is up to date with main

---

## Release Process

Releases follow semantic versioning (major.minor.patch).

### Steps

1. Update version in comments/docs
1. Update CHANGELOG.md
1. Tag commit: `git tag vX.Y.Z`
1. Push tag: `git push origin vX.Y.Z`
1. GitHub Actions automatically:

- Builds for all platforms
- Creates release
- Updates Docker image

1. Manually update:

- Homebrew formula
- AUR PKGBUILD
- Distribution docs

---

## Development Tips

### Build for different platforms

```bash
# Linux x86_64
GOOS=linux GOARCH=amd64 go build -o zabbix-bench-linux-amd64

# macOS ARM64 (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o zabbix-bench-darwin-arm64

# Windows x86_64
GOOS=windows GOARCH=amd64 go build -o zabbix-bench-windows-amd64.exe
```

### Run local benchmark

```bash
# Assuming Zabbix is running on localhost:8080
./zabbix-bench \
  -api-url "http://localhost:8080/api_jsonrpc.php" \
  -user "Admin" -pass "zabbix" \
  -hosts 10 -duration 30s \
  -output-json /tmp/result.json

# View results
cat /tmp/result.json | jq '.p95_latency_ms, .worker_stats'
```

### Debug performance issues

```bash
# Add timing to see where time is spent
time ./zabbix-bench -hosts 5 -duration 5s

# Profile with pprof (if available)
go build -o zabbix-bench -cpuprofile=cpu.prof main.go
# Then: go tool pprof cpu.prof
```

---

## Questions?

- Open an issue for questions
- Check existing issues for answers
- Review README.md and documentation
- Look at example usage in tests

Thank you for contributing.
