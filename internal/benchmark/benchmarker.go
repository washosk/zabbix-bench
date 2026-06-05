package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	zabbixapi "github.com/kgeroczi/go-zabbix-api"
	"github.com/washosk/zabbix-bench/internal/config"
	"github.com/washosk/zabbix-bench/internal/zabbix"
)

const maxLatencySamples = 1_000_000

type Benchmarker struct {
	cfg       config.Config
	api       *zabbixapi.API
	hostIDs   []string
	hostNames []string
	groupID   string

	createdHostIDs []string
	createdGroup   bool

	mu        sync.Mutex
	pool      *ValuePool
	startTime time.Time

	sender *zabbix.TrapperSender

	workerStats []*WorkerStats
	workerMu    []sync.Mutex

	totalHostsSent int64
	totalPackets   int64
	totalErrors    int64
	totalLatencyMs int64

	latencies   []int64
	latenciesMu sync.Mutex

	errorTimeout int64
	errorClosed  int64
	errorNetwork int64
	errorOther   int64
}

func NewBenchmarker(cfg config.Config, sender *zabbix.TrapperSender) *Benchmarker {
	return &Benchmarker{
		cfg:       cfg,
		sender:    sender,
		pool:      newValuePool(1024),
		latencies: make([]int64, 0, 100000),
	}
}

func (bm *Benchmarker) SetAPI(api *zabbixapi.API) {
	bm.api = api
}

func (bm *Benchmarker) recordLatency(latencyUs int64, workerID int) {
	atomic.AddInt64(&bm.totalLatencyMs, latencyUs)

	bm.latenciesMu.Lock()
	if len(bm.latencies) < maxLatencySamples {
		bm.latencies = append(bm.latencies, latencyUs)
	}
	bm.latenciesMu.Unlock()

	if workerID >= 0 && workerID < len(bm.workerStats) {
		bm.workerMu[workerID].Lock()
		stats := bm.workerStats[workerID]
		stats.TotalLatencyUs += latencyUs
		if latencyUs < stats.MinLatencyUs {
			stats.MinLatencyUs = latencyUs
		}
		if latencyUs > stats.MaxLatencyUs {
			stats.MaxLatencyUs = latencyUs
		}
		bm.workerMu[workerID].Unlock()
	}
}

func (bm *Benchmarker) recordError(err error, workerID int) {
	atomic.AddInt64(&bm.totalErrors, 1)

	if workerID >= 0 && workerID < len(bm.workerStats) {
		bm.workerMu[workerID].Lock()
		bm.workerStats[workerID].ErrorCount++
		bm.workerMu[workerID].Unlock()
	}

	var netErr net.Error
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		atomic.AddInt64(&bm.errorTimeout, 1)
	case errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "closed") || strings.Contains(err.Error(), "EOF"):
		atomic.AddInt64(&bm.errorClosed, 1)
	case errors.As(err, &netErr) || strings.Contains(err.Error(), "connection") || strings.Contains(err.Error(), "network"):
		atomic.AddInt64(&bm.errorNetwork, 1)
	default:
		atomic.AddInt64(&bm.errorOther, 1)
	}
}

func (bm *Benchmarker) sortLatencies() {
	sort.Slice(bm.latencies, func(i, j int) bool { return bm.latencies[i] < bm.latencies[j] })
}

func (bm *Benchmarker) calculatePercentile(percent float64) int64 {
	if len(bm.latencies) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(bm.latencies))*percent/100)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(bm.latencies) {
		index = len(bm.latencies) - 1
	}
	return bm.latencies[index]
}

func (bm *Benchmarker) Setup(ctx context.Context) error {
	log.Printf("=== SETUP PHASE ===")

	var err error
	var createdGroup bool

	bm.groupID, createdGroup, err = bm.ensureHostGroup(bm.cfg.GroupName)
	if err != nil {
		return err
	}
	bm.createdGroup = createdGroup

	if createdGroup {
		log.Printf("Host Group created: %s (ID: %s)", bm.cfg.GroupName, bm.groupID)
	} else {
		log.Printf("Host Group exists: %s (ID: %s)", bm.cfg.GroupName, bm.groupID)
	}

	log.Printf("Creating %d hosts in bulk batches...", bm.cfg.NumHosts)

	var setupErrs []error

	hostNames := make([]string, 0, bm.cfg.NumHosts)
	for i := 0; i < bm.cfg.NumHosts; i++ {
		hostNames = append(hostNames, fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1))
	}

	batchSize := 500
	for i := 0; i < len(hostNames); i += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		end := i + batchSize
		if end > len(hostNames) {
			end = len(hostNames)
		}

		batchNames := hostNames[i:end]
		ids, err := bm.bulkCreateHosts(batchNames)
		if err != nil {
			setupErrs = append(setupErrs, err)
			continue
		}

		err = bm.bulkCreateItems(ids)
		if err != nil {
			setupErrs = append(setupErrs, err)
		}

		bm.mu.Lock()
		bm.hostIDs = append(bm.hostIDs, ids...)
		bm.hostNames = append(bm.hostNames, batchNames...)
		bm.createdHostIDs = append(bm.createdHostIDs, ids...)
		bm.mu.Unlock()
	}

	if len(setupErrs) > 0 {
		return errors.Join(setupErrs...)
	}

	log.Printf("Setup complete. %d/%d hosts ready.", len(bm.hostIDs), bm.cfg.NumHosts)

	return nil
}

func (bm *Benchmarker) LoadExistingHosts() error {
	log.Printf("=== SKIP SETUP: Loading existing hosts with prefix '%s' ===", bm.cfg.HostPrefix)

	groups, err := bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": bm.cfg.GroupName}})
	if err == nil && len(groups) > 0 {
		bm.groupID = groups[0].GroupID
		log.Printf("Found Host Group: %s (ID: %s)", bm.cfg.GroupName, bm.groupID)
	} else {
		log.Printf("Warning: Host Group '%s' not found; cleanup will be skipped.", bm.cfg.GroupName)
	}

	for i := 0; i < bm.cfg.NumHosts; i++ {
		name := fmt.Sprintf("%s%04d", bm.cfg.HostPrefix, i+1)
		bm.hostNames = append(bm.hostNames, name)
	}

	if bm.groupID != "" {
		hosts, err := bm.api.HostsGet(zabbixapi.Params{
			"groupids": []string{bm.groupID},
			"output":   []string{"hostid"},
		})
		if err == nil {
			for _, h := range hosts {
				bm.hostIDs = append(bm.hostIDs, h.HostID)
			}
		}
	}

	log.Printf("Loaded %d host names (%d host IDs from API).", len(bm.hostNames), len(bm.hostIDs))
	log.Printf("Skip setup mode will not delete pre-existing hosts during cleanup.")

	return nil
}

func (bm *Benchmarker) GenerateResult() BenchmarkResult {
	bm.latenciesMu.Lock()
	defer bm.latenciesMu.Unlock()

	hostsSent := atomic.LoadInt64(&bm.totalHostsSent)
	packets := atomic.LoadInt64(&bm.totalPackets)
	errs := atomic.LoadInt64(&bm.totalErrors)
	attempts := packets + errs
	latTotal := atomic.LoadInt64(&bm.totalLatencyMs)

	mph := int64(bm.cfg.MetricsPerHost)
	values := hostsSent * mph

	bm.sortLatencies()

	var minLat int64
	var maxLat int64

	if len(bm.latencies) > 0 {
		minLat = bm.latencies[0]
		maxLat = bm.latencies[len(bm.latencies)-1]
	}

	avgLatency := int64(0)
	if packets > 0 {
		avgLatency = latTotal / packets
	}

	errRate := 0.0
	if attempts > 0 {
		errRate = float64(errs) / float64(attempts) * 100
	}

	workerStats := make([]WorkerStats, 0, len(bm.workerStats))
	for i, stats := range bm.workerStats {
		if stats == nil {
			continue
		}

		bm.workerMu[i].Lock()
		if stats.PacketsSent > 0 {
			stats.AvgLatencyUs = stats.TotalLatencyUs / stats.PacketsSent
		}

		if stats.MinLatencyUs == 1<<63-1 {
			stats.MinLatencyUs = 0
		}

		statsCopy := *stats
		bm.workerMu[i].Unlock()

		if statsCopy.PacketsSent > 0 || statsCopy.ErrorCount > 0 {
			workerStats = append(workerStats, statsCopy)
		}
	}

	elapsed := time.Since(bm.startTime).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}

	return BenchmarkResult{
		Duration:       elapsed,
		HostsTested:    len(bm.hostNames),
		TotalHostsSent: hostsSent,
		TotalValues:    values,
		TotalPackets:   packets,
		TotalAttempts:  attempts,
		ErrorCount:     errs,
		ErrorRate:      errRate,
		Throughput:     float64(values) / elapsed,
		AvgLatencyUs:   avgLatency,
		MinLatencyUs:   minLat,
		MaxLatencyUs:   maxLat,
		P50LatencyUs:   bm.calculatePercentile(50),
		P95LatencyUs:   bm.calculatePercentile(95),
		P99LatencyUs:   bm.calculatePercentile(99),
		LatencySamples: len(bm.latencies),
		ErrorsByType: ErrorCategory{
			Timeout: int(atomic.LoadInt64(&bm.errorTimeout)),
			Closed:  int(atomic.LoadInt64(&bm.errorClosed)),
			Network: int(atomic.LoadInt64(&bm.errorNetwork)),
			Other:   int(atomic.LoadInt64(&bm.errorOther)),
			Total:   int(errs),
		},
		WorkerStats: workerStats,
		Config: map[string]any{
			"hosts":            bm.cfg.NumHosts,
			"senders":          bm.cfg.NumSenders,
			"metrics_per_host": bm.cfg.MetricsPerHost,
			"batch_hosts":      bm.cfg.BatchHosts,
			"effective_batch":  bm.effectiveBatchSize(),
			"batch_metrics":    bm.cfg.MaxBatchSize,
			"rate":             bm.cfg.Rate,
			"trapper_addr":     bm.cfg.TrapperAddr,
			"skip_setup":       bm.cfg.SkipSetup,
			"keep_hosts":       bm.cfg.KeepHosts,
		},
	}
}

func (bm *Benchmarker) PrintSummary(result BenchmarkResult) {
	metricsPerHost := bm.cfg.MetricsPerHost
	boxLine := func(content string) {
		fmt.Printf("║ %-56s║\n", content)
	}

	fmt.Println()
	fmt.Println("╔═════════════════════════════════════════════════════════╗")
	boxLine("              BENCHMARK SUMMARY REPORT")
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	boxLine(fmt.Sprintf("Hosts tested:        %d", result.HostsTested))
	boxLine(fmt.Sprintf("Total host sends:    %d", result.TotalHostsSent))
	boxLine(fmt.Sprintf("Total values:        %d", result.TotalValues))
	boxLine(fmt.Sprintf("Total packets:       %d", result.TotalPackets))
	boxLine(fmt.Sprintf("Total attempts:      %d", result.TotalAttempts))
	boxLine(fmt.Sprintf("Errors:              %d (%.1f%%)", result.ErrorCount, result.ErrorRate))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")
	boxLine(fmt.Sprintf("Throughput (VPS):    %.2f", result.Throughput))
	boxLine(fmt.Sprintf("Avg latency:         %.2f ms", float64(result.AvgLatencyUs)/1000.0))
	boxLine(fmt.Sprintf("Min latency:         %.2f ms", float64(result.MinLatencyUs)/1000.0))
	boxLine(fmt.Sprintf("Max latency:         %.2f ms", float64(result.MaxLatencyUs)/1000.0))
	boxLine(fmt.Sprintf("P50 latency:         %.2f ms", float64(result.P50LatencyUs)/1000.0))
	boxLine(fmt.Sprintf("P95 latency:         %.2f ms", float64(result.P95LatencyUs)/1000.0))
	boxLine(fmt.Sprintf("P99 latency:         %.2f ms", float64(result.P99LatencyUs)/1000.0))
	boxLine(fmt.Sprintf("Latency samples:     %d", result.LatencySamples))
	fmt.Println("╠═════════════════════════════════════════════════════════╣")

	if result.ErrorsByType.Total > 0 {
		boxLine("Error breakdown:")
		boxLine(fmt.Sprintf("  Timeout:           %d", result.ErrorsByType.Timeout))
		boxLine(fmt.Sprintf("  Connection closed: %d", result.ErrorsByType.Closed))
		boxLine(fmt.Sprintf("  Network error:     %d", result.ErrorsByType.Network))
		boxLine(fmt.Sprintf("  Other:             %d", result.ErrorsByType.Other))
		fmt.Println("╠═════════════════════════════════════════════════════════╣")
	}

	if len(result.WorkerStats) > 0 {
		boxLine("PARALLEL EXECUTION BREAKDOWN")
		for _, ws := range result.WorkerStats {
			workerVPS := float64(ws.HostsSent*int64(metricsPerHost)) / result.Duration
			boxLine(fmt.Sprintf("  Worker #%02d: %d pkts | %d hosts | %d err | %.0f VPS",
				ws.ID,
				ws.PacketsSent,
				ws.HostsSent,
				ws.ErrorCount,
				workerVPS,
			))
		}
		fmt.Println("╠═════════════════════════════════════════════════════════╣")
	}

	fmt.Println("╚═════════════════════════════════════════════════════════╝")
}

func (bm *Benchmarker) ExportJSON(result BenchmarkResult) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return
	}

	if err := os.WriteFile(bm.cfg.OutputJSON, data, 0600); err != nil {
		log.Printf("Error writing JSON file: %v", err)
		return
	}

	log.Printf("Results exported to %s", bm.cfg.OutputJSON)
}

func (bm *Benchmarker) Cleanup() {
	if bm.api == nil {
		return
	}

	log.Printf("=== CLEANUP PHASE ===")

	if len(bm.createdHostIDs) > 0 {
		log.Printf("Deleting %d hosts created by this run...", len(bm.createdHostIDs))

		batchSize := 500
		for i := 0; i < len(bm.createdHostIDs); i += batchSize {
			end := i + batchSize
			if end > len(bm.createdHostIDs) {
				end = len(bm.createdHostIDs)
			}

			if err := bm.api.HostsDeleteByIds(bm.createdHostIDs[i:end]); err != nil {
				log.Printf("Error deleting hosts: %v", err)
			}
		}
	} else {
		log.Printf("No hosts created by this run; skipping host deletion.")
	}

	if bm.createdGroup && bm.groupID != "" {
		log.Printf("Deleting Host Group created by this run: '%s'...", bm.cfg.GroupName)
		if err := bm.api.HostGroupsDeleteByIds([]string{bm.groupID}); err != nil {
			log.Printf("Error deleting group: %v", err)
		}
	} else {
		log.Printf("Host group was not created by this run; skipping group deletion.")
	}

	log.Printf("Cleanup complete.")
}

func (bm *Benchmarker) ensureHostGroup(name string) (string, bool, error) {
	groups, err := bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	if err == nil && len(groups) > 0 {
		return groups[0].GroupID, false, nil
	}

	if err := bm.api.HostGroupsCreate(zabbixapi.HostGroups{{Name: name}}); err != nil {
		return "", false, fmt.Errorf("failed to create host group: %w", err)
	}

	groups, err = bm.api.HostGroupsGet(zabbixapi.Params{"filter": map[string]string{"name": name}})
	if err != nil || len(groups) == 0 {
		return "", false, fmt.Errorf("failed to retrieve host group after creation: %w", err)
	}

	return groups[0].GroupID, true, nil
}

func (bm *Benchmarker) bulkCreateHosts(hostNames []string) ([]string, error) {
	hosts := make([]map[string]interface{}, 0, len(hostNames))
	for _, name := range hostNames {
		hosts = append(hosts, map[string]interface{}{
			"host":   name,
			"name":   name,
			"groups": []map[string]string{{"groupid": bm.groupID}},
			"interfaces": []map[string]interface{}{{
				"type":  1,
				"main":  1,
				"useip": 1,
				"ip":    "127.0.0.1",
				"dns":   "",
				"port":  "10050",
			}},
		})
	}

	var result struct {
		HostIDs []string `json:"hostids"`
	}

	err := bm.api.CallWithErrorParse("host.create", hosts, &result)
	if err != nil {
		return nil, fmt.Errorf("bulk host.create failed: %w", err)
	}

	if len(result.HostIDs) != len(hostNames) {
		return nil, fmt.Errorf("expected %d hostids, got %d", len(hostNames), len(result.HostIDs))
	}

	return result.HostIDs, nil
}

func (bm *Benchmarker) bulkCreateItems(hostIDs []string) error {
	metricTypeMap := map[string]int{
		"bool":     3,
		"unsigned": 3,
		"float":    0,
		"text":     4,
		"char":     1,
		"log":      2,
	}
	metricTypes := []string{"bool", "unsigned", "float", "text", "char", "log"}

	items := make([]map[string]any, 0, len(hostIDs)*bm.cfg.MetricsPerHost)
	for _, hostID := range hostIDs {
		for m := 0; m < bm.cfg.MetricsPerHost; m++ {
			metricType := metricTypes[m%len(metricTypes)]
			itemKey := fmt.Sprintf("test.metric.%d.%s", m, metricType)
			itemName := fmt.Sprintf("Metric %d (%s)", m, metricType)

			items = append(items, map[string]any{
				"name":       itemName,
				"key_":       itemKey,
				"hostid":     hostID,
				"type":       2,
				"value_type": metricTypeMap[metricType],
			})
		}
	}

	if len(items) > 0 {
		if err := bm.api.CallWithErrorParse("item.create", items, nil); err != nil {
			return fmt.Errorf("bulk item.create failed: %w", err)
		}
	}
	return nil
}
