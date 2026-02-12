package core

import (
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

var (
	perfDiagEnabled  bool
	perfDiagInterval = 10 * time.Second

	downReadCount atomic.Uint64
	downReadBytes atomic.Uint64
	downReadNanos atomic.Uint64

	downParseCount atomic.Uint64
	downParseNanos atomic.Uint64

	downDecryptCount atomic.Uint64
	downDecryptNanos atomic.Uint64

	downConsumerGapCount atomic.Uint64
	downConsumerGapNanos atomic.Uint64

	upWriteCount atomic.Uint64
	upWriteBytes atomic.Uint64
	upWriteNanos atomic.Uint64

	upBuildCount atomic.Uint64
	upBuildNanos atomic.Uint64
)

type perfSnapshot struct {
	downReadCount    uint64
	downReadBytes    uint64
	downReadNanos    uint64
	downParseCount   uint64
	downParseNanos   uint64
	downDecryptCount uint64
	downDecryptNanos uint64
	downConsumerGapCount uint64
	downConsumerGapNanos uint64
	upWriteCount     uint64
	upWriteBytes     uint64
	upWriteNanos     uint64
	upBuildCount     uint64
	upBuildNanos     uint64
}

func init() {
	perfDiagEnabled = os.Getenv("PERF_DIAG_ENABLE") == "1"
	if !perfDiagEnabled {
		return
	}
	if v := os.Getenv("PERF_DIAG_INTERVAL_SEC"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			perfDiagInterval = time.Duration(sec) * time.Second
		}
	}
	log.Printf("[PERF] enabled=true interval=%s", perfDiagInterval)
	go runPerfDiagReporter(perfDiagInterval)
}

func runPerfDiagReporter(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prev := currentPerfSnapshot()
	for range ticker.C {
		cur := currentPerfSnapshot()
		logPerfDelta(interval, prev, cur)
		prev = cur
	}
}

func currentPerfSnapshot() perfSnapshot {
	return perfSnapshot{
		downReadCount:    downReadCount.Load(),
		downReadBytes:    downReadBytes.Load(),
		downReadNanos:    downReadNanos.Load(),
		downParseCount:   downParseCount.Load(),
		downParseNanos:   downParseNanos.Load(),
		downDecryptCount: downDecryptCount.Load(),
		downDecryptNanos: downDecryptNanos.Load(),
		downConsumerGapCount: downConsumerGapCount.Load(),
		downConsumerGapNanos: downConsumerGapNanos.Load(),
		upWriteCount:     upWriteCount.Load(),
		upWriteBytes:     upWriteBytes.Load(),
		upWriteNanos:     upWriteNanos.Load(),
		upBuildCount:     upBuildCount.Load(),
		upBuildNanos:     upBuildNanos.Load(),
	}
}

func logPerfDelta(interval time.Duration, prev, cur perfSnapshot) {
	downReads := cur.downReadCount - prev.downReadCount
	downBytes := cur.downReadBytes - prev.downReadBytes
	downReadNs := cur.downReadNanos - prev.downReadNanos
	downParseCalls := cur.downParseCount - prev.downParseCount
	downParseNs := cur.downParseNanos - prev.downParseNanos
	downDecCalls := cur.downDecryptCount - prev.downDecryptCount
	downDecNs := cur.downDecryptNanos - prev.downDecryptNanos
	downConsumerGapCalls := cur.downConsumerGapCount - prev.downConsumerGapCount
	downConsumerGapNs := cur.downConsumerGapNanos - prev.downConsumerGapNanos

	upWrites := cur.upWriteCount - prev.upWriteCount
	upBytes := cur.upWriteBytes - prev.upWriteBytes
	upWriteNs := cur.upWriteNanos - prev.upWriteNanos
	upBuildCalls := cur.upBuildCount - prev.upBuildCount
	upBuildNs := cur.upBuildNanos - prev.upBuildNanos

	intervalSec := interval.Seconds()
	downMbps := float64(downBytes*8) / 1_000_000.0 / intervalSec
	upMbps := float64(upBytes*8) / 1_000_000.0 / intervalSec

	downReadAvgUs := avgMicros(downReadNs, downReads)
	downParseAvgUs := avgMicros(downParseNs, downParseCalls)
	downDecAvgUs := avgMicros(downDecNs, downDecCalls)
	downConsumerGapAvgUs := avgMicros(downConsumerGapNs, downConsumerGapCalls)
	upWriteAvgUs := avgMicros(upWriteNs, upWrites)
	upBuildAvgUs := avgMicros(upBuildNs, upBuildCalls)

	log.Printf(
		"[PERF] window=%s down{mbps=%.2f rps=%d read_us=%.1f parse_us=%.1f dec_us=%.1f pull_gap_us=%.1f} up{mbps=%.2f wps=%d build_us=%.1f write_us=%.1f}",
		interval,
		downMbps, downReads, downReadAvgUs, downParseAvgUs, downDecAvgUs, downConsumerGapAvgUs,
		upMbps, upWrites, upBuildAvgUs, upWriteAvgUs,
	)
}

func avgMicros(totalNs, calls uint64) float64 {
	if calls == 0 {
		return 0
	}
	return (float64(totalNs) / float64(calls)) / 1000.0
}

func perfObserveDownRead(bytes int, d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	downReadCount.Add(1)
	downReadBytes.Add(uint64(bytes))
	downReadNanos.Add(uint64(d.Nanoseconds()))
}

func perfObserveDownParse(d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	downParseCount.Add(1)
	downParseNanos.Add(uint64(d.Nanoseconds()))
}

func perfObserveDownDecrypt(d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	downDecryptCount.Add(1)
	downDecryptNanos.Add(uint64(d.Nanoseconds()))
}

func perfObserveDownConsumerGap(d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	if d <= 0 {
		return
	}
	downConsumerGapCount.Add(1)
	downConsumerGapNanos.Add(uint64(d.Nanoseconds()))
}

func perfObserveUpBuild(d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	upBuildCount.Add(1)
	upBuildNanos.Add(uint64(d.Nanoseconds()))
}

func perfObserveUpWrite(bytes int, d time.Duration) {
	if !perfDiagEnabled {
		return
	}
	upWriteCount.Add(1)
	upWriteBytes.Add(uint64(bytes))
	upWriteNanos.Add(uint64(d.Nanoseconds()))
}
