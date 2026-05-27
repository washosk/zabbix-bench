# `zabbix-bench` Future Improvements

This document tracks planned architectural and performance improvements for the `zabbix-bench` tool.

---

## 🚀 High priority

### 1. Migrate to `math/rand/v2`
Modernize the random number generator using Go's newer standard library package.
*   **Why**: The legacy `math/rand` generator relies on deprecated seeding mechanisms (`rand.NewSource`) and suffers from global lock contention if shared.
*   **Implementation guidelines**:
    *   Change the import from `"math/rand"` to `"math/rand/v2"`.
    *   In `newValuePool`, replace legacy calls with `rand.IntN()` or `rand.Uint64()`.
    *   In the worker logic, replace `rand.New(rand.NewSource(...))` with `rand.New(rand.NewPCG(seed1, seed2))` using the PCG generator for lock-free, faster execution.

### 2. Implement setup phase API retries
Make the host registration phase resilient to transient infrastructure issues.
*   **Why**: Benchmarking often targets busy Zabbix environments where setup API requests can fail due to temporary network blips, HTTP gateway timeouts, or database rate limits.
*   **Implementation guidelines**:
    *   Write a helper method `callAPIWithRetry` that accepts the API method, params, and target struct.
    *   Implement exponential backoff (e.g., attempt 3 times, waiting 500ms, 1s, then 2s).
    *   Use this helper inside `createHostWithItems()` and `ensureHostGroup()`.

---

## 📊 Diagnostics & profiling

### 3. Add profiling CLI flags (`-cpuprofile` and `-memprofile`)
Implement the profiling flags documented in `CONTRIBUTING.md`.
*   **Why**: Developers and maintainers need a standardized way to collect CPU and memory profiles of the benchmark run to find allocation hotspots.
*   **Implementation guidelines**:
    *   Import `"runtime/pprof"` and `"os"`.
    *   Register `-cpuprofile` and `-memprofile` flags.
    *   At the start of `main()`, if `-cpuprofile` is set, create the file and call `pprof.StartCPUProfile(file)`. Defer the stop call.
    *   At the end of `main()`, if `-memprofile` is set, run `runtime.GC()` and call `pprof.WriteHeapProfile(file)`.

---

## ⚙️ Configuration & flexibility

### 4. Support more environment variables
Allow configuring the API URL and username headlessly.
*   **Why**: Support running in containerized environments (Kubernetes, Docker) or CI/CD pipelines where passing flags is inconvenient or exposes parameters.
*   **Implementation guidelines**:
    *   Inside `main()`, check `os.Getenv("ZABBIX_API_URL")` and `os.Getenv("ZABBIX_USER")`.
    *   Use these values as fallbacks when flags are not provided, similar to the existing `ZABBIX_API_KEY` handling.

### 5. Customizable Trapper network timeouts
Make dial, read, and write timeouts configurable via command-line flags and YAML.
*   **Why**: When testing over high-latency networks, VPNs, or under extreme CPU starvation, the hardcoded 5-second dial/write timeouts can cause spurious network errors.
*   **Implementation guidelines**:
    *   Add `TrapperDialTimeout`, `TrapperReadTimeout`, and `TrapperWriteTimeout` to the `Config` struct.
    *   Expose them as CLI options (e.g., `-trapper-dial-timeout`) and YAML fields.
    *   Pass the configured durations to `zabbix.NewSenderTimeout()` in `NewTrapperSender`.
