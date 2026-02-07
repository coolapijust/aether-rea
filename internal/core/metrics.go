package core

import (
	"sync/atomic"
	"time"
)

// Metrics tracks runtime statistics for the Core.
// All fields are thread-safe via atomic operations.
type Metrics struct {
	sessionStart   atomic.Value // time.Time
	activeStreams  atomic.Int64
	totalStreams   atomic.Int64
	bytesSent      atomic.Uint64
	bytesReceived  atomic.Uint64
	lastLatency    atomic.Value // *int64 (milliseconds)
}

// NewMetrics creates a new Metrics instance.
func NewMetrics() *Metrics {
	m := &Metrics{}
	m.sessionStart.Store(time.Time{})
	m.lastLatency.Store((*int64)(nil))
	return m
}

// RecordSessionStart marks session start time.
func (m *Metrics) RecordSessionStart() {
	m.sessionStart.Store(time.Now())
}

// RecordSessionEnd clears session state.
func (m *Metrics) RecordSessionEnd() {
	m.sessionStart.Store(time.Time{})
	m.activeStreams.Store(0)
}

// SessionUptime returns milliseconds since session start (0 if not started).
func (m *Metrics) SessionUptime() int64 {
	start := m.sessionStart.Load().(time.Time)
	if start.IsZero() {
		return 0
	}
	return time.Since(start).Milliseconds()
}

// StreamOpened increments active and total stream counts.
func (m *Metrics) StreamOpened() {
	m.activeStreams.Add(1)
	m.totalStreams.Add(1)
}

// StreamClosed decrements active stream count.
func (m *Metrics) StreamClosed() {
	m.activeStreams.Add(-1)
}

// ActiveStreams returns current number of open streams.
func (m *Metrics) ActiveStreams() int64 {
	return m.activeStreams.Load()
}

// TotalStreams returns total streams opened in this session.
func (m *Metrics) TotalStreams() int64 {
	return m.totalStreams.Load()
}

// RecordBytesSent adds to sent bytes counter.
func (m *Metrics) RecordBytesSent(n uint64) {
	m.bytesSent.Add(n)
}

// RecordBytesReceived adds to received bytes counter.
func (m *Metrics) RecordBytesReceived(n uint64) {
	m.bytesReceived.Add(n)
}

// BytesSent returns total bytes sent.
func (m *Metrics) BytesSent() uint64 {
	return m.bytesSent.Load()
}

// BytesReceived returns total bytes received.
func (m *Metrics) BytesReceived() uint64 {
	return m.bytesReceived.Load()
}

// RecordLatency stores last measured latency.
func (m *Metrics) RecordLatency(ms int64) {
	m.lastLatency.Store(&ms)
}

// LastLatency returns last measured latency (nil if none).
func (m *Metrics) LastLatency() *int64 {
	val := m.lastLatency.Load()
	if val == nil {
		return nil
	}
	return val.(*int64)
}

// Snapshot returns current metrics as an event.
func (m *Metrics) Snapshot() Event {
	latency := m.LastLatency()
	return NewMetricsSnapshotEvent(
		m.SessionUptime(),
		m.ActiveStreams(),
		m.TotalStreams(),
		m.BytesSent(),
		m.BytesReceived(),
		latency,
	)
}

// MetricsCollector periodically emits metrics snapshots.
type MetricsCollector struct {
	metrics  *Metrics
	interval time.Duration
	stop     chan struct{}
	emitFunc func(Event)
}

// NewMetricsCollector creates a collector.
func NewMetricsCollector(metrics *Metrics, interval time.Duration, emit func(Event)) *MetricsCollector {
	return &MetricsCollector{
		metrics:  metrics,
		interval: interval,
		stop:     make(chan struct{}),
		emitFunc: emit,
	}
}

// Start begins periodic collection.
func (mc *MetricsCollector) Start() {
	go func() {
		ticker := time.NewTicker(mc.interval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ticker.C:
				mc.emitFunc(mc.metrics.Snapshot())
			case <-mc.stop:
				return
			}
		}
	}()
}

// Stop halts collection.
func (mc *MetricsCollector) Stop() {
	close(mc.stop)
}
