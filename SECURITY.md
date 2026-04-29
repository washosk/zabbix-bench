# Security Policy

This document outlines security practices, vulnerability reporting, and safe usage guidelines for `zabbix-bench`.

## Overview

`zabbix-bench` is a high-performance benchmarking tool for Zabbix 7.0+ that generates synthetic metric load through the Zabbix Trapper protocol. Because it operates with API credentials and network access to monitoring infrastructure, security is a critical concern.

## Supported Versions

| Version | Support Status | Security Updates |
| --- | --- | --- |
| 1.7.x | ✓ Active | Yes |
| 1.6.x | ⚠️ Limited | Critical fixes only |
| 1.5.x and earlier | ❌ Unsupported | None |

Please upgrade to the latest stable version. Security updates will not be backported to versions older than 1.5.x.

## Reporting Security Vulnerabilities

**Do not open public GitHub issues for security vulnerabilities.** This prevents disclosure before a fix is available.

### Private Reporting (Recommended)

If your GitHub user profile has an email, GitHub's Security Advisory feature allows private reporting:

1. Go to the [Security tab](https://github.com/washosk/zabbix-bench/security)
2. Click **"Report a vulnerability"** → **"Draft a security advisory"**
3. Describe the vulnerability, affected versions, and suggested fix
4. Submit; maintainers will respond within 72 hours

### Email Reporting

If GitHub's private reporting is unavailable, email the maintainer directly:

- **To**: [security email address from project maintainer]
- **Subject**: `[SECURITY] zabbix-bench vulnerability report`
- **Include**:
  - Detailed vulnerability description
  - Affected version(s)
  - Steps to reproduce (if applicable)
  - Suggested remediation
  - Your contact information for follow-up

### Response Timeline

- **72 hours**: Acknowledgment of receipt
- **7 days**: Initial assessment and preliminary fix timeline
- **30 days**: Security patch release (typical)

We will credit the reporter in the CHANGELOG and GitHub release notes unless you request anonymity.

## Known Security Considerations

### 1. API Credentials

**Risk**: `zabbix-bench` requires Zabbix API credentials to create and delete hosts/items.

**Mitigation**:

- Use **API tokens** instead of username/password when possible (Zabbix 7.0+)
  - Tokens are shorter, rotatable, and can be scoped to specific permissions
  - Pass via `-api-key` flag; never store in YAML config files
- If using username/password (`-user` / `-pass`):
  - Credentials are stored in memory only during execution
  - **Never** pass via YAML config file (treat as secrets; use 1Password, Vault, etc.)
  - Use environment variable substitution: `pass: ${ZABBIX_PASSWORD}` in YAML (not supported yet; use CLI flags instead)
- Credentials are **never** logged, exported, or written to output files

**Best Practice**:

```bash
# Good: Use API token
./zabbix-bench -api-key "your-token-here" -duration 10s

# Good: Export to env and reference in YAML
export ZABBIX_API_KEY="your-token"
./zabbix-bench  # Then YAML can reference ${ZABBIX_API_KEY} (requires code change to support)

# Avoid: Hardcoding in shell history
./zabbix-bench -user "Admin" -pass "zabbix"  # Password in shell history

# Avoid: Storing in YAML config (committed to git)
# zabbix-bench.yaml
# user: Admin
# pass: zabbix
```

### 2. Trapper Data

**Risk**: Metric data sent to Zabbix Trapper is transmitted unencrypted by default.

**Mitigation**:

- Trapper protocol supports TLS encryption (Zabbix 6.0+)
- Deploy Zabbix with TLS enabled on port 10051
- Use `-trapper-addr "your-zabbix:10051"` with TLS endpoint

**Network Best Practice**:

- Run `zabbix-bench` on the same network segment as Zabbix (not across the internet)
- Use firewall rules to restrict Trapper port (10051) to known benchmark sources
- Monitor for unexpected Trapper connections

### 3. Host/Item Cleanup

**Risk**: By default, `zabbix-bench` deletes all hosts in the configured benchmark group after completion.

**Mitigation**:

1. **Use unique group names**:
   - ✓ Good: `Benchmark-Group-2026-04-21-Capacity-Test`
   - ❌ Bad: `Benchmark` or `Test`
2. **Use `--dry-run` first**:
   - Always run `./zabbix-bench --dry-run -profile light` before actual benchmark
   - This previews execution plan without making changes
3. **Use `-keep-hosts` to preserve resources**:
   - First run: `./zabbix-bench -profile light -keep-hosts`
   - Inspect created hosts/items in Zabbix UI
   - Manually delete or re-run without `-keep-hosts` for cleanup
4. **Manual cleanup**:
   - If process dies (`kill -9`), manually delete benchmark group and hosts via Zabbix UI
   - Use host IDs displayed in startup summary for quick lookup

### 4. API Rate Limiting & Resource Exhaustion

**Risk**: Rapid benchmarking runs could cause Zabbix API or database load.

**Mitigation**:

- Use performance profiles to scale appropriately:
  - `light` — 10 hosts, 5 workers (safe for production-adjacent systems)
  - `balanced` — 100 hosts, 20 workers (typical capacity test)
  - `flood` — 1000 hosts, 100 workers (stress test; requires isolated lab)
- Monitor Zabbix API response times during benchmarks
- Allow cooldown period between runs
- Run benchmarks during maintenance windows on production systems

### 5. Network Access & Firewall

**Risk**: `zabbix-bench` requires network access to Zabbix API and Trapper ports.

**Mitigation**:

- Restrict Trapper port (10051) to known benchmark sources via firewall
- Use VPN or private network segments for benchmarking
- Verify API URL correctness before running (`--validate-only` flag)
- Monitor network logs for failed connection attempts
- Use TLS for API connections if available (HTTPS)

### 6. Process Interruption

**Risk**: Sending `kill -9` to `zabbix-bench` skips graceful cleanup of benchmark resources.

**Mitigation**:

- Always send SIGTERM (`kill <pid>` or Ctrl+C), not SIGKILL (`kill -9`)
- Process will finish current batch and cleanup hosts gracefully
- If `kill -9` occurs:
  1. Check Zabbix UI for benchmark hosts in the configured group
  2. Manually delete hosts and group
  3. Use startup summary or logs to identify created host IDs

## Dependency Security

### Third-Party Package Audits

`zabbix-bench` uses minimal external dependencies:

- **golang-zabbix-sender** — Implements Zabbix Trapper protocol
  - Repository: github.com/christos-diamantis/golang-zabbix-sender
  - Audit: Minimal scope; protocol implementation only; supports HA and Proxy Groups.
  - Status: Actively maintained.

- **go-zabbix-api** — Zabbix API client library
  - Repository: github.com/kgeroczi/go-zabbix-api
  - Audit: API wrapper; no cryptographic functions
  - Status: Community-maintained; monitor for updates

- **gopkg.in/yaml.v3** — YAML parser (stdlib alternative)
  - Audit: Standard, widely-used YAML library
  - Status: Actively maintained by Go community

### Scanning for Vulnerabilities

Run `go mod tidy` and check for known vulnerabilities:

```bash
# Update dependencies to latest
go get -u ./...
go mod tidy

# Check for known vulnerabilities (Go 1.18+)
go list -m all | while read module version; do
  echo "Checking $module@$version"
done

# Or use external scanner
# govulncheck ./...  (Go 1.18+)
```

If vulnerabilities are found in dependencies, file an issue on this repository or the dependency's repository.

## Build & Release Security

### Signed Releases

GitHub releases are signed. Verify authenticity:

```bash
# List signatures
git tag -l -n1 | grep v1.

# Verify tag signature
git verify-tag v1.7.1
```

### Binary Verification

When downloading release binaries:

1. **Use HTTPS only** (not HTTP)
2. **Check checksums** if provided:

   ```bash
   sha256sum zabbix-bench-linux-amd64 | grep <expected-hash>
   ```

3. **Verify GPG signature** if available:

   ```bash
   gpg --verify zabbix-bench-linux-amd64.sig zabbix-bench-linux-amd64
   ```

### Docker Image Security

If using the Docker image:

```bash
# Pull from official release
docker pull ghcr.io/washosk/zabbix-bench:v1.7.1

# Verify image (check for CVEs)
# Use Trivy or similar image scanner
trivy image ghcr.io/washosk/zabbix-bench:v1.7.1
```

## Safe Usage Practices

### Pre-Benchmark Checklist

- [ ] Use unique, descriptive group name (e.g., `Benchmark-2026-04-21-Capacity`)
- [ ] Run `--dry-run` to preview execution plan
- [ ] Verify `-api-url` points to correct Zabbix instance
- [ ] Use API token, not username/password
- [ ] Check Zabbix UI for existing hosts in the benchmark group (should be none)
- [ ] Schedule benchmark during maintenance window (if production)
- [ ] Notify Zabbix operations team before running on production

### Benchmark Execution

- [ ] Use appropriate profile (`light` for prod-adjacent, `flood` for isolated lab)
- [ ] Monitor Zabbix API response times and database load
- [ ] Watch for error messages on stdout
- [ ] Allow graceful shutdown (don't use `kill -9`)

### Post-Benchmark Verification

- [ ] Verify cleanup completed (check Zabbix for benchmark hosts)
- [ ] Review JSON export for anomalies
- [ ] If benchmarking for capacity planning, store results securely
- [ ] Document benchmark conditions (profile, host count, duration, throughput achieved)

## Code Security Standards

### Input Validation

All user input is validated:

- API URLs are parsed and validated (format, host reachability)
- Host count, duration, and numeric parameters checked for reasonableness
- Group names sanitized (no path traversal, special characters)

### Output Handling

- JSON export sanitized to prevent injection
- Credential data never written to logs or files
- Error messages don't leak sensitive information

### Concurrency Safety

- Metric counters use `sync/atomic` (no data races)
- Worker goroutines isolated (no shared mutable state except counters)
- No unsafe pointer usage

## Responsible Disclosure

If you discover a security vulnerability in `zabbix-bench`:

1. **Do not** open a public GitHub issue
2. **Report privately** using GitHub Security Advisory or email
3. **Include**:
   - Detailed description with steps to reproduce
   - Affected version(s)
   - Suggested fix (if any)
4. **Timeline**: Expect response within 72 hours
5. **Credit**: You will be credited in the fix unless you request anonymity

We follow a 30-day disclosure deadline:

- Day 1: We acknowledge receipt
- Day 7: Initial assessment and proposed fix
- Day 30: Public disclosure (if fix not available by then, details released to allow community patches)

## Security Advisories & Announcements

Subscribe to security announcements:

- Watch this repository (GitHub notifications)
- Star releases (GitHub release notifications)
- Check CHANGELOG.md for security fix notes

### Past Advisories

None currently. This section will be populated if vulnerabilities are discovered and fixed.

## Contact

For security concerns, contact the maintainer privately:

- GitHub Security Advisory: [Use GitHub's built-in reporting](https://github.com/washosk/zabbix-bench/security)
- Email: [security@example.com] (update with actual email)

## Acknowledgments

Security researchers and community members who have responsibly disclosed vulnerabilities will be credited here.

---

**Last Updated**: April 29, 2026
**Version**: 1.0
**Status**: Active
