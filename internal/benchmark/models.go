package benchmark

import (
	"fmt"
	"math/rand/v2"
)

const benchAlpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"

type ValuePool struct {
	bools  []string
	uints  []string
	floats []string
	chars  []string
}

func newValuePool(size int) *ValuePool {
	vp := &ValuePool{}
	for i := 0; i < size; i++ {
		vp.bools = append(vp.bools, fmt.Sprintf("%d", rand.IntN(2)))
		vp.uints = append(vp.uints, fmt.Sprintf("%d", rand.Uint64()))
		vp.floats = append(vp.floats, fmt.Sprintf("%.4f", rand.Float64()*100))
		n := rand.IntN(len(benchAlpha))
		vp.chars = append(vp.chars, benchAlpha[n:n+1])
	}
	return vp
}

type ErrorCategory struct {
	Timeout int `json:"timeout"`
	Closed  int `json:"closed"`
	Network int `json:"network"`
	Other   int `json:"other"`
	Total   int `json:"total"`
}

type WorkerStats struct {
	ID             int   `json:"worker_id"`
	PacketsSent    int64 `json:"packets_sent"`
	HostsSent      int64 `json:"hosts_sent"`
	ErrorCount     int64 `json:"errors"`
	TotalLatencyUs int64 `json:"total_latency_us"`
	MinLatencyUs   int64 `json:"min_latency_us"`
	MaxLatencyUs   int64 `json:"max_latency_us"`
	AvgLatencyUs   int64 `json:"avg_latency_us"`
}

type BenchmarkResult struct {
	Duration       float64       `json:"duration_seconds"`
	HostsTested    int           `json:"hosts_tested"`
	TotalHostsSent int64         `json:"total_host_sends"`
	TotalValues    int64         `json:"total_values"`
	TotalPackets   int64         `json:"total_packets"`
	TotalAttempts  int64         `json:"total_attempts"`
	ErrorCount     int64         `json:"error_count"`
	ErrorRate      float64       `json:"error_rate_percent"`
	Throughput     float64       `json:"throughput_vps"`
	AvgLatencyUs   int64         `json:"avg_latency_us"`
	MinLatencyUs   int64         `json:"min_latency_us"`
	MaxLatencyUs   int64         `json:"max_latency_us"`
	P50LatencyUs   int64         `json:"p50_latency_us"`
	P95LatencyUs   int64         `json:"p95_latency_us"`
	P99LatencyUs   int64         `json:"p99_latency_us"`
	LatencySamples int           `json:"latency_samples"`
	ErrorsByType   ErrorCategory `json:"errors_by_type"`
	WorkerStats    []WorkerStats `json:"worker_stats"`
	Config         any           `json:"config"`
}
