// Package core implements random jitter rotation strategy for session management.
//
// Rotation Strategy:
// - Random interval between [MinRotateInterval, MaxRotateInterval]
// - Pre-warm: New session established 30s before rotation
// - Graceful switch: New streams use new session, old streams drain naturally
// - Seamless: User connections are not interrupted
//
// This design prevents:
// 1. Traffic pattern analysis (fixed intervals are detectable)
// 2. Thundering herd (all clients rotating simultaneously)
// 3. Connection drops (graceful migration)

package core

import (
	"crypto/rand"
	"math/big"
	"sync"
	"time"
)

// RotationPolicy defines the session rotation strategy.
type RotationPolicy struct {
	// Minimum rotation interval (e.g., 15 minutes)
	MinInterval time.Duration
	
	// Maximum rotation interval (e.g., 40 minutes)
	MaxInterval time.Duration
	
	// Pre-warm duration - new session is established this long before switch
	// Default: 30 seconds
	PreWarmDuration time.Duration
	
	// JitterEnabled adds randomness to prevent predictable patterns
	JitterEnabled bool
}

// DefaultRotationPolicy returns the recommended policy.
// Random interval between 15-40 minutes with 30s pre-warm.
func DefaultRotationPolicy() RotationPolicy {
	return RotationPolicy{
		MinInterval:     15 * time.Minute,
		MaxInterval:     40 * time.Minute,
		PreWarmDuration: 30 * time.Second,
		JitterEnabled:   true,
	}
}

// rotationScheduler manages the timing of session rotations.
type rotationScheduler struct {
	policy       RotationPolicy
	nextRotation time.Time
	preWarmTime  time.Time
	timer        *time.Timer
	mu           sync.RWMutex
	onPreWarm    func()              // Called when pre-warm starts
	onRotate     func()              // Called when rotation should happen
	onScheduled  func(time.Time)     // Called when next rotation is scheduled
	stopCh       chan struct{}
}

// newRotationScheduler creates a scheduler with the given policy.
func newRotationScheduler(policy RotationPolicy, onPreWarm, onRotate func(), onScheduled func(time.Time)) *rotationScheduler {
	return &rotationScheduler{
		policy:      policy,
		onPreWarm:   onPreWarm,
		onRotate:    onRotate,
		onScheduled: onScheduled,
		stopCh:      make(chan struct{}),
	}
}

// start begins the rotation scheduling loop.
func (rs *rotationScheduler) start() {
	rs.scheduleNext()
}

// stop halts the scheduler.
func (rs *rotationScheduler) stop() {
	close(rs.stopCh)
	if rs.timer != nil {
		rs.timer.Stop()
	}
}

// scheduleNext calculates and schedules the next rotation.
func (rs *rotationScheduler) scheduleNext() {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	// Calculate random interval
	var interval time.Duration
	if rs.policy.JitterEnabled {
		interval = rs.randomInterval()
	} else {
		interval = rs.policy.MinInterval
	}

	now := time.Now()
	rs.nextRotation = now.Add(interval)
	rs.preWarmTime = rs.nextRotation.Add(-rs.policy.PreWarmDuration)

	// If pre-warm time is in the past, rotate immediately
	if rs.preWarmTime.Before(now) {
		rs.preWarmTime = now.Add(5 * time.Second)
		rs.nextRotation = now.Add(5*time.Second + rs.policy.PreWarmDuration)
	}

	// Notify scheduler
	if rs.onScheduled != nil {
		rs.onScheduled(rs.nextRotation)
	}

	// Schedule pre-warm
	preWarmDelay := rs.preWarmTime.Sub(now)
	rs.timer = time.AfterFunc(preWarmDelay, func() {
		rs.handlePreWarm()
	})
}

// handlePreWarm is called when pre-warm time arrives.
func (rs *rotationScheduler) handlePreWarm() {
	select {
	case <-rs.stopCh:
		return
	default:
	}

	// Trigger pre-warm (establish new session)
	if rs.onPreWarm != nil {
		go rs.onPreWarm()
	}

	// Schedule actual rotation
	rs.mu.RLock()
	rotationDelay := rs.nextRotation.Sub(time.Now())
	rs.mu.RUnlock()

	if rotationDelay > 0 {
		rs.timer = time.AfterFunc(rotationDelay, func() {
			rs.handleRotation()
		})
	} else {
		rs.handleRotation()
	}
}

// handleRotation is called when rotation time arrives.
func (rs *rotationScheduler) handleRotation() {
	select {
	case <-rs.stopCh:
		return
	default:
	}

	// Trigger rotation
	if rs.onRotate != nil {
		go rs.onRotate()
	}

	// Schedule next rotation
	rs.scheduleNext()
}

// randomInterval generates a cryptographically secure random duration
// between MinInterval and MaxInterval.
func (rs *rotationScheduler) randomInterval() time.Duration {
	minMs := rs.policy.MinInterval.Milliseconds()
	maxMs := rs.policy.MaxInterval.Milliseconds()
	
	if minMs >= maxMs {
		return rs.policy.MinInterval
	}

	// Generate random value in [0, max-min)
	diff := maxMs - minMs
	n, err := rand.Int(rand.Reader, big.NewInt(diff))
	if err != nil {
		// Fallback to time-based pseudo-random
		return rs.policy.MinInterval + time.Duration(time.Now().UnixNano()%(diff))*time.Millisecond
	}

	return rs.policy.MinInterval + time.Duration(n.Int64())*time.Millisecond
}

// getNextRotation returns the scheduled next rotation time (for display).
func (rs *rotationScheduler) getNextRotation() time.Time {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.nextRotation
}

// timeUntilRotation returns duration until next rotation.
func (rs *rotationScheduler) timeUntilRotation() time.Duration {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return time.Until(rs.nextRotation)
}

// RotationConfig is the JSON-serializable configuration for rotation.
// Replaces the old fixed RotateInterval.
type RotationConfig struct {
	// Enabled controls whether auto-rotation is active
	Enabled bool `json:"enabled"`
	
	// MinIntervalMs is the minimum rotation interval in milliseconds
	// Default: 900000 (15 minutes)
	MinIntervalMs int `json:"minIntervalMs,omitempty"`
	
	// MaxIntervalMs is the maximum rotation interval in milliseconds
	// Default: 2400000 (40 minutes)
	MaxIntervalMs int `json:"maxIntervalMs,omitempty"`
	
	// PreWarmMs is the pre-warm duration in milliseconds
	// Default: 30000 (30 seconds)
	PreWarmMs int `json:"preWarmMs,omitempty"`
	
	// Jitter adds randomness to prevent predictable patterns
	// Default: true
	Jitter *bool `json:"jitter,omitempty"`
}

// toPolicy converts RotationConfig to RotationPolicy.
func (rc RotationConfig) toPolicy() RotationPolicy {
	policy := DefaultRotationPolicy()
	
	if rc.MinIntervalMs > 0 {
		policy.MinInterval = time.Duration(rc.MinIntervalMs) * time.Millisecond
	}
	if rc.MaxIntervalMs > 0 {
		policy.MaxInterval = time.Duration(rc.MaxIntervalMs) * time.Millisecond
	}
	if rc.PreWarmMs > 0 {
		policy.PreWarmDuration = time.Duration(rc.PreWarmMs) * time.Millisecond
	}
	if rc.Jitter != nil {
		policy.JitterEnabled = *rc.Jitter
	}
	
	return policy
}

// DefaultRotationConfig returns the default rotation configuration.
func DefaultRotationConfig() RotationConfig {
	jitter := true
	return RotationConfig{
		Enabled:       true,
		MinIntervalMs: 15 * 60 * 1000,  // 15 minutes
		MaxIntervalMs: 40 * 60 * 1000,  // 40 minutes
		PreWarmMs:     30 * 1000,       // 30 seconds
		Jitter:        &jitter,
	}
}
