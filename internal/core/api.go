package core

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"aether-rea/internal/systemproxy"
	"strings"
)


// SessionConfig is JSON-serializable configuration for Core.
type SessionConfig struct {
	URL            string         `json:"url"`                // https://host/path
	PSK            string         `json:"psk"`                // Pre-shared key
	ListenAddr     string         `json:"listenAddr"`         // SOCKS5 listen address
	DialAddr       string         `json:"dialAddr,omitempty"` // Override dial address (optional)
	MaxPadding     int            `json:"maxPadding,omitempty"` // 0-65535, default 0
	Rotation       RotationConfig `json:"rotation,omitempty"`   // Session rotation policy
	
	// Deprecated: Use Rotation.Enabled = false instead
	RotateInterval int `json:"rotateInterval,omitempty"` 
}

// TargetAddress represents a destination host:port.
type TargetAddress struct {
	Host string `json:"host"` // IP or domain
	Port int    `json:"port"` // 1-65535
}

// StreamHandle is an opaque identifier for an open stream.
type StreamHandle struct {
	ID string `json:"id"`
}

// StreamInfo represents information about an active stream
type StreamInfo struct {
	ID            string `json:"id"`
	TargetHost    string `json:"targetHost"`
	TargetPort    int    `json:"targetPort"`
	OpenedAt      int64  `json:"openedAt"`
	State         string `json:"state"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
}

// CoreState is defined in state.go - use CoreState type from state.go

// EventHandler receives Core events.
type EventHandler func(event Event)

// Subscription represents an event subscription.
type Subscription struct {
	ID      string
	Handler EventHandler
	cancel  func()
}

// Cancel removes the subscription.
func (s *Subscription) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Core is the main API interface.
// It encapsulates the entire Aether-Realist protocol without exposing internals.
type Core struct {
	stateMachine *StateMachine
	config       *SessionConfig
	editingConfig SessionConfig
	handlers     map[string]EventHandler
	handlerMu    sync.RWMutex
	subCounter   int64
	mu           sync.RWMutex
	
	// Internal components (not exposed)
	sessionMgr   *sessionManager
	socksServer  *socks5Server
	metrics      *Metrics
	metricsCollector *MetricsCollector
	streams      map[string]*StreamInfo
	activeStreams map[string]io.ReadWriteCloser
	systemProxyEnabled bool
	ruleEngine   *RuleEngine
	eventBus     chan Event
	ctx          context.Context
	cancel       context.CancelFunc
}

// New creates a new Core instance.
func New() *Core {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Core{
		handlers:      make(map[string]EventHandler),
		streams:       make(map[string]*StreamInfo),
		activeStreams: make(map[string]io.ReadWriteCloser),
		eventBus:      make(chan Event, 100),
		ctx:           ctx,
		cancel:        cancel,
	}
	
	c.stateMachine = NewStateMachine(func(from, to CoreState) {
		c.emit(NewStateChangedEvent(from, to))
	})
	
	return c
}

// Start transitions from Idle -> Starting -> Active.
// Returns when session is established or error occurs.
func (c *Core) Start(config SessionConfig) error {
	if !c.stateMachine.CanTransition(StateStarting) {
		return fmt.Errorf("cannot start from state %s", c.stateMachine.State())
	}
	
	c.config = &config
	
	if err := c.stateMachine.Transition(StateStarting); err != nil {
		return err
	}
	
	// Initialize internal components
	if err := c.initialize(); err != nil {
		c.stateMachine.Transition(StateError)
		return err
	}
	
	return c.stateMachine.Transition(StateActive)
}

// Rotate manually triggers session rotation (Active -> Rotating -> Active).
func (c *Core) Rotate() error {
	if !c.stateMachine.CanTransition(StateRotating) {
		return fmt.Errorf("cannot rotate from state %s", c.stateMachine.State())
	}
	
	if err := c.stateMachine.Transition(StateRotating); err != nil {
		return err
	}
	
	// Perform rotation
	if err := c.performRotation(); err != nil {
		c.stateMachine.Transition(StateError)
		return err
	}
	
	return c.stateMachine.Transition(StateActive)
}

// Close gracefully shuts down (Active/Rotating -> Closing -> Closed).
func (c *Core) Close() error {
	current := c.stateMachine.State()
	if current != StateActive && current != StateRotating && current != StateError {
		return fmt.Errorf("cannot close from state %s", current)
	}
	
	if err := c.stateMachine.Transition(StateClosing); err != nil {
		return err
	}
	
	// Cleanup
	c.cleanup()
	c.cancel()
	
	return c.stateMachine.Transition(StateClosed)
}

// OpenStream creates a new stream to target (only valid in Active state).
func (c *Core) OpenStream(target TargetAddress, options map[string]interface{}) (StreamHandle, error) {
	if c.stateMachine.State() != StateActive {
		return StreamHandle{}, fmt.Errorf("cannot open stream in state %s", c.stateMachine.State())
	}
	
	return c.openStreamInternal(target, options)
}

// CloseStream closes an existing stream.
func (c *Core) CloseStream(handle StreamHandle) error {
	return c.closeStreamInternal(handle)
}

// Subscribe registers an event handler. Returns subscription for cancellation.
func (c *Core) Subscribe(handler EventHandler) *Subscription {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	
	c.subCounter++
	id := fmt.Sprintf("sub-%d", c.subCounter)
	sub := &Subscription{
		ID:      id,
		Handler: handler,
		cancel: func() {
			c.handlerMu.Lock()
			delete(c.handlers, id)
			c.handlerMu.Unlock()
		},
	}
	c.handlers[id] = handler
	return sub
}

// GetState returns current FSM state (for initialization recovery only, do not poll).
func (c *Core) GetState() string {
	return string(c.stateMachine.State())
}

// GetActiveConfig returns current config (read-only, for display).
func (c *Core) GetActiveConfig() *SessionConfig {
	return c.config
}

// emit broadcasts event to all handlers.
func (c *Core) emit(event Event) {
	select {
	case c.eventBus <- event:
	default:
		// Channel full, drop event
	}
}

// UpdateConfig updates the core configuration.
func (c *Core) UpdateConfig(config SessionConfig) error {
	c.mu.Lock()
	
	// Check if listen address changed while system proxy is enabled
	oldAddr := ""
	if c.config != nil {
		oldAddr = c.config.ListenAddr
	}
	
	c.config = &config
	needsProxyRefresh := c.systemProxyEnabled && oldAddr != config.ListenAddr
	c.mu.Unlock()
	
	if needsProxyRefresh {
		c.SetSystemProxy(true)
	}
	
	currentState := c.stateMachine.State()
	if currentState == StateIdle || currentState == StateError {
		// Recover from error state if needed
		if currentState == StateError {
			if err := c.stateMachine.Transition(StateIdle); err != nil {
				return err
			}
		}

		if config.URL != "" {
			// Auto-start if we have a valid config now
			return c.Start(config)
		}
	} else if currentState == StateActive {
		// If already active, we might need to reconnect session
		if c.sessionMgr != nil {
			go c.Rotate()
		}
	}
	
	return nil
}

// GetStreams returns list of active stream info.
func (c *Core) GetStreams() []*StreamInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	res := make([]*StreamInfo, 0, len(c.streams))
	for _, s := range c.streams {
		res = append(res, s)
	}
	return res
}

// GetMetrics returns current metrics snapshot.
func (c *Core) GetMetrics() Event {
	if c.metrics == nil {
		return nil
	}
	return c.metrics.Snapshot()
}

// GetRules returns current routing rules.
func (c *Core) GetRules() []*Rule {
	if c.ruleEngine == nil {
		return nil
	}
	return c.ruleEngine.GetRules()
}

// UpdateRules updates routing rules.
func (c *Core) UpdateRules(rules []*Rule) error {
	if c.ruleEngine == nil {
		return fmt.Errorf("rule engine not initialized")
	}
	return c.ruleEngine.UpdateRules(rules)
}

// IsSystemProxyEnabled returns true if system proxy is active.
func (c *Core) IsSystemProxyEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.systemProxyEnabled
}

// GetLogWriter returns a writer that emits logs as events.
func (c *Core) GetLogWriter() io.Writer {
	return &logWriter{core: c}
}

type logWriter struct {
	core *Core
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	msg := string(p)
	// Simple level detection
	level := "info"
	lowerMsg := strings.ToLower(msg)
	if strings.Contains(lowerMsg, "error") || strings.Contains(lowerMsg, "failed") {
		level = "error"
	} else if strings.Contains(lowerMsg, "warn") {
		level = "warn"
	}
	
	w.core.emit(NewAppLogEvent(level, strings.TrimSpace(msg), "core"))
	return len(p), nil
}

// SetSystemProxy enables or disables the system proxy for this Core's listener.
func (c *Core) SetSystemProxy(enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if enabled {
		if c.config == nil || c.config.ListenAddr == "" {
			return fmt.Errorf("no listen address configured")
		}
		if err := systemproxy.EnableSocksProxy(c.config.ListenAddr); err != nil {
			return err
		}
		c.systemProxyEnabled = true
	} else {
		if err := systemproxy.DisableSocksProxy(); err != nil {
			return err
		}
		c.systemProxyEnabled = false
	}
	
	// Emit an event (optional, but good for UI)
	// c.emit(NewProxyStateChangedEvent(c.systemProxyEnabled))
	
	return nil
}

// runEventLoop processes events and dispatches to handlers.
func (c *Core) runEventLoop() {
	for {
		select {
		case event := <-c.eventBus:
			c.handlerMu.RLock()
			handlers := make([]EventHandler, 0, len(c.handlers))
			for _, h := range c.handlers {
				handlers = append(handlers, h)
			}
			c.handlerMu.RUnlock()
			
			for _, h := range handlers {
				go h(event) // Async dispatch to prevent blocking
			}
			
		case <-c.ctx.Done():
			return
		}
	}
}

// initialize sets up internal components.
func (c *Core) initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = NewMetrics() // Use constructor!
	c.metricsCollector = NewMetricsCollector(c.metrics, 1*time.Second, c.emit)
	c.metricsCollector.Start()

	c.ruleEngine = NewRuleEngine(ActionProxy) // Default to proxy

	c.sessionMgr = newSessionManager(c.config, c.emit, c.metrics)
	if err := c.sessionMgr.initialize(); err != nil {
		return err
	}
	if err := c.sessionMgr.connect(); err != nil {
		return err
	}

	c.socksServer = newSocks5Server(c.config.ListenAddr, c)
	if err := c.socksServer.start(); err != nil {
		return err
	}

	go c.runEventLoop()
	return nil
}

// cleanup releases all resources.
func (c *Core) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.metricsCollector != nil {
		c.metricsCollector.Stop()
	}

	if c.socksServer != nil {
		c.socksServer.stop()
	}
	if c.sessionMgr != nil {
		c.sessionMgr.close("cleanup")
	}

	for id, s := range c.activeStreams {
		s.Close()
		delete(c.activeStreams, id)
	}
	c.streams = make(map[string]*StreamInfo)
}

// performRotation handles session rotation.
func (c *Core) performRotation() error {
	if c.sessionMgr == nil {
		return fmt.Errorf("session manager not initialized")
	}
	return c.sessionMgr.rotate()
}

// openStreamInternal creates a stream (protocol internal).
func (c *Core) openStreamInternal(target TargetAddress, options map[string]interface{}) (StreamHandle, error) {
	session, streamID, err := c.sessionMgr.getSession(c.ctx)
	if err != nil {
		return StreamHandle{}, err
	}

	stream, err := session.OpenStreamSync(c.ctx)
	if err != nil {
		return StreamHandle{}, err
	}

	// Handshake metadata
	maxPadding := uint16(c.config.MaxPadding)
	if v, ok := options["maxPadding"].(float64); ok {
		maxPadding = uint16(v)
	}

	metaRecord, err := BuildMetadataRecord(target.Host, uint16(target.Port), maxPadding, c.config.PSK, streamID)
	if err != nil {
		stream.Close()
		return StreamHandle{}, err
	}

	if _, err := stream.Write(metaRecord); err != nil {
		stream.Close()
		return StreamHandle{}, err
	}

	id := fmt.Sprintf("str-%d-%d", streamID, time.Now().UnixNano())
	handle := StreamHandle{ID: id}

	info := &StreamInfo{
		ID:         id,
		TargetHost: target.Host,
		TargetPort: target.Port,
		OpenedAt:   time.Now().UnixMilli(),
		State:      "Open",
	}

	c.mu.Lock()
	c.streams[id] = info
	c.activeStreams[id] = stream
	c.mu.Unlock()

	if c.metrics != nil {
		c.metrics.StreamOpened()
	}

	c.emit(NewStreamOpenedEvent(id, target))

	return handle, nil
}

// closeStreamInternal closes a stream.
func (c *Core) closeStreamInternal(handle StreamHandle) error {
	c.mu.Lock()
	stream, ok := c.activeStreams[handle.ID]
	info := c.streams[handle.ID]
	delete(c.activeStreams, handle.ID)
	delete(c.streams, handle.ID)
	c.mu.Unlock()

	if !ok {
		return fmt.Errorf("stream not found: %s", handle.ID)
	}

	err := stream.Close()
	if c.metrics != nil {
		c.metrics.StreamClosed()
	}
	if info != nil {
		c.emit(NewStreamClosedEvent(handle.ID, info.BytesSent, info.BytesReceived))
	}
	return err
}

// GetUnderlyingStream returns the actual stream object for a handle.
func (c *Core) GetUnderlyingStream(handle StreamHandle) (io.ReadWriteCloser, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.activeStreams[handle.ID]
	return s, ok
}
