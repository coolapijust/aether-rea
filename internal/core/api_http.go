package core

import (
	"fmt"
	"time"
)

// API methods for HTTP server integration

// UpdateConfig updates the Core configuration
func (c *Core) UpdateConfig(config SessionConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.config = &config
	c.editingConfig = config
	
	// Emit event
	c.emit(NewCoreEventEvent("config.updated", "Configuration updated", false))
	
	return nil
}

// GetRules returns current rules
func (c *Core) GetRules() []*Rule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	if c.ruleEngine == nil {
		return []*Rule{}
	}
	
	return c.ruleEngine.GetRules()
}

// UpdateRules updates all rules
func (c *Core) UpdateRules(rules []*Rule) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.ruleEngine == nil {
		return fmt.Errorf("rule engine not initialized")
	}
	
	return c.ruleEngine.UpdateRules(rules)
}

// GetStreams returns active streams
func (c *Core) GetStreams() []StreamInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	result := make([]StreamInfo, 0)
	for _, stream := range c.streams {
		result = append(result, *stream)
	}
	
	return result
}

// GetMetrics returns current metrics
func (c *Core) GetMetrics() MetricsData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return MetricsData{
		SessionUptime:   c.metrics.SessionUptime(),
		ActiveStreams:   c.metrics.ActiveStreams(),
		TotalStreams:    c.metrics.TotalStreams(),
		BytesSent:       c.metrics.BytesSent(),
		BytesReceived:   c.metrics.BytesReceived(),
		LastLatencyMs:   c.metrics.LastLatency(),
	}
}

// MetricsData holds metric information
type MetricsData struct {
	SessionUptime   int64   `json:"session_uptime_ms"`
	ActiveStreams   int64   `json:"active_streams"`
	TotalStreams    int64   `json:"total_streams"`
	BytesSent       uint64  `json:"bytes_sent"`
	BytesReceived   uint64  `json:"bytes_received"`
	LastLatencyMs   *int64  `json:"last_latency_ms,omitempty"`
}

// CoreEventEvent is a generic event
type CoreEventEvent struct {
	baseEvent
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NewCoreEventEvent creates a new event
func NewCoreEventEvent(code, message string, fatal bool) Event {
	return CoreEventEvent{
		baseEvent: baseEvent{Type: "core.event", Timestamp: time.Now().UnixMilli()},
		Code:      code,
		Message:   message,
	}
}
