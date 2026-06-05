package benchmark

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	sender "github.com/christos-diamantis/golang-zabbix-sender"
	"golang.org/x/time/rate"
)

func (bm *Benchmarker) effectiveBatchSize() int {
	batchSize := bm.cfg.BatchHosts

	if bm.cfg.MaxBatchSize > 0 && bm.cfg.MetricsPerHost > 0 {
		hostsFit := bm.cfg.MaxBatchSize / bm.cfg.MetricsPerHost
		if hostsFit > 0 && hostsFit < batchSize {
			batchSize = hostsFit
		}
	}

	if batchSize <= 0 {
		batchSize = 1
	}

	return batchSize
}

func (bm *Benchmarker) Run(ctx context.Context) {
	log.Printf("=== BENCHMARK PHASE ===")

	floodMode := bm.cfg.Rate == 0
	log.Printf("Hosts: %d | Senders: %d | Batch: %d | Flood: %v | Duration: %v",
		len(bm.hostNames),
		bm.cfg.NumSenders,
		bm.effectiveBatchSize(),
		floodMode,
		bm.cfg.Duration,
	)

	bm.workerStats = make([]*WorkerStats, bm.cfg.NumSenders)
	bm.workerMu = make([]sync.Mutex, bm.cfg.NumSenders)
	for i := 0; i < bm.cfg.NumSenders; i++ {
		bm.workerStats[i] = &WorkerStats{ID: i, MinLatencyUs: 1<<63 - 1}
	}

	var wg sync.WaitGroup

	hostsPerWorker := (len(bm.hostNames) + bm.cfg.NumSenders - 1) / bm.cfg.NumSenders
	bm.startTime = time.Now()

	for i := 0; i < bm.cfg.NumSenders; i++ {
		start := i * hostsPerWorker
		end := (i + 1) * hostsPerWorker

		if start >= len(bm.hostNames) {
			break
		}
		if end > len(bm.hostNames) {
			end = len(bm.hostNames)
		}

		wg.Add(1)
		go func(workerID int, hosts []string) {
			defer wg.Done()
			bm.worker(ctx, workerID, hosts)
		}(i, bm.hostNames[start:end])
	}

	go bm.printProgressLoop(ctx)

	wg.Wait()
}

func (bm *Benchmarker) printProgressLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastHostsSent := int64(0)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hostsSent := atomic.LoadInt64(&bm.totalHostsSent)
			packets := atomic.LoadInt64(&bm.totalPackets)
			errs := atomic.LoadInt64(&bm.totalErrors)
			attempts := packets + errs

			elapsed := time.Since(bm.startTime).Seconds()
			if elapsed < 0.001 {
				elapsed = 0.001
			}

			mph := int64(bm.cfg.MetricsPerHost)
			vps := float64(hostsSent*mph) / elapsed
			intervalVPS := float64((hostsSent-lastHostsSent)*mph) / 5.0
			lastHostsSent = hostsSent

			errRate := 0.0
			if attempts > 0 {
				errRate = float64(errs) / float64(attempts) * 100
			}

			log.Printf("[%6.0fs] %8d hosts | %6d pkts | %10.2f VPS (inst: %.2f) | errors: %d (%.1f%%)",
				elapsed,
				hostsSent,
				packets,
				vps,
				intervalVPS,
				errs,
				errRate,
			)
		}
	}
}

func (bm *Benchmarker) worker(ctx context.Context, workerID int, hosts []string) {
	if len(hosts) == 0 {
		return
	}

	localRand := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(workerID)))

	var poolSize int
	if bm.pool != nil {
		poolSize = len(bm.pool.bools)
	}
	idx := 0
	if poolSize > 0 {
		idx = localRand.IntN(poolSize)
	}

	batchSize := bm.effectiveBatchSize()
	if batchSize > len(hosts) {
		batchSize = len(hosts)
	}

	metricsPerHost := bm.cfg.MetricsPerHost
	reusableMetrics := make([]*sender.Metric, batchSize*metricsPerHost)
	for i := range reusableMetrics {
		reusableMetrics[i] = &sender.Metric{}
	}
	metricTypes := []string{"bool", "unsigned", "float", "text", "char", "log"}

	sendBatch := func(hostSlice []string) {
		sliceLen := len(hostSlice) * metricsPerHost
		metrics := reusableMetrics[:sliceLen]

		metricIdx := 0
		for _, host := range hostSlice {
			var poolI int
			if poolSize > 0 {
				poolI = idx % poolSize
				idx++
			}

			for m := 0; m < metricsPerHost; m++ {
				metricType := metricTypes[m%len(metricTypes)]
				metricKey := fmt.Sprintf("test.metric.%d.%s", m, metricType)

				var value string
				switch metricType {
				case "bool":
					if poolSize > 0 {
						value = bm.pool.bools[poolI]
					} else {
						value = "0"
					}
				case "unsigned":
					if poolSize > 0 {
						value = bm.pool.uints[poolI]
					} else {
						value = "0"
					}
				case "float":
					if poolSize > 0 {
						value = bm.pool.floats[poolI]
					} else {
						value = "0.0"
					}
				case "text":
					value = fmt.Sprintf("Benchmark text value %d", m)
				case "char":
					if poolSize > 0 {
						value = bm.pool.chars[poolI]
					} else {
						value = "a"
					}
				case "log":
					value = fmt.Sprintf("Benchmark log entry %d", m)
				default:
					value = "unknown"
				}

				mRef := metrics[metricIdx]
				mRef.Host = host
				mRef.Key = metricKey
				mRef.Value = value
				metricIdx++
			}
		}

		t0 := time.Now()
		var err error

		func() {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("sender panic: %v", r)
				}
			}()

			err = bm.sender.SendMetrics(metrics)
		}()

		latency := time.Since(t0).Microseconds()

		if err == nil {
			atomic.AddInt64(&bm.totalHostsSent, int64(len(hostSlice)))
			atomic.AddInt64(&bm.totalPackets, 1)

			bm.recordLatency(latency, workerID)

			bm.workerMu[workerID].Lock()
			stats := bm.workerStats[workerID]
			stats.PacketsSent++
			stats.HostsSent += int64(len(hostSlice))
			bm.workerMu[workerID].Unlock()
		} else {
			bm.recordError(err, workerID)
		}
	}

	sendAll := func() {
		for i := 0; i < len(hosts); i += batchSize {
			if ctx.Err() != nil {
				return
			}

			end := i + batchSize
			if end > len(hosts) {
				end = len(hosts)
			}

			sendBatch(hosts[i:end])
		}
	}

	if bm.cfg.Rate == 0 {
		for ctx.Err() == nil {
			sendAll()
		}
		return
	}

	limiter := rate.NewLimiter(rate.Limit(bm.cfg.Rate), 1)
	cursor := 0

	for ctx.Err() == nil {
		if err := limiter.Wait(ctx); err != nil {
			return
		}

		end := cursor + batchSize
		if end > len(hosts) {
			end = len(hosts)
		}

		sendBatch(hosts[cursor:end])

		cursor = end
		if cursor >= len(hosts) {
			cursor = 0
		}
	}
}
