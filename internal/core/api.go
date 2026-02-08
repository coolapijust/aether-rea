package core

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"aether-rea/internal/systemproxy"
	"strings"
)


// SessionConfig is JSON-serializable configuration for Core.
type SessionConfig struct {
	URL            string         `json:"url"`                // https://host/path
	PSK            string         `json:"psk"`                // Pre-shared key
	ListenAddr     string         `json:"listen_addr"`         // SOCKS5 listen address
	HttpProxyAddr  string         `json:"http_proxy_addr"`      // HTTP proxy listen address
	DialAddr       string         `json:"dial_addr,omitempty"` // Override dial address (optional)
	MaxPadding     int            `json:"max_padding,omitempty"` // 0-65535, default 0
	AllowInsecure  bool           `json:"allow_insecure"`        // Skip TLS verification
	Rotation       RotationConfig `json:"rotation,omitempty"`   // Session rotation policy
	BypassCN       bool           `json:"bypass_cn"`             // Bypass China sites
	BlockAds       bool           `json:"block_ads"`             // Block advertisement
	
	Rules []*Rule `json:"rules,omitempty"` // Custom routing rules
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
	handlersMu   sync.RWMutex
	subCounter   int64
	mu           sync.RWMutex
	
	// Internal components (not exposed)
	sessionMgr   *sessionManager
	socksServer  *socks5Server
	httpProxyServer *HttpProxyServer
	metrics      *Metrics
	metricsCollector *MetricsCollector
	streams      map[string]*StreamInfo
	activeStreams map[string]io.ReadWriteCloser
	systemProxyEnabled bool
	ruleEngine   *RuleEngine
	eventBus     chan Event
	ctx          context.Context
	cancel       context.CancelFunc
	configManager *ConfigManager
	lastError     error
}

// New creates a new Core instance.
func New() *Core {
	ctx, cancel := context.WithCancel(context.Background())
	
	cm, _ := NewConfigManager() // Ignore error for now, just won't save if fails
	
	c := &Core{
		handlers:      make(map[string]EventHandler),
		handlersMu:    sync.RWMutex{}, // Renamed to handlersMu for clarity
		streams:       make(map[string]*StreamInfo),
		activeStreams: make(map[string]io.ReadWriteCloser),
		eventBus:      make(chan Event, 100),
		ctx:           ctx,
		cancel:        cancel,
		configManager: cm,
	}
	
	c.stateMachine = NewStateMachine(func(from, to CoreState) {
		c.emit(NewStateChangedEvent(from, to))
	})

	// Add event processing loop
	go c.processEvents()
	
	return c
}

func (c *Core) processEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case event := <-c.eventBus:
			c.handlersMu.RLock()
			handlers := make([]EventHandler, 0, len(c.handlers))
			for _, h := range c.handlers {
				handlers = append(handlers, h)
			}
			c.handlersMu.RUnlock()
			
			for _, h := range handlers {
				go h(event) // Async dispatch to prevent blocking
			}
		}
	}
}

// Start transitions from Idle -> Starting -> Active.
// Returns when session is established or error occurs.
func (c *Core) Start(config SessionConfig) error {
	log.Printf("[DEBUG] Core.Start called")
	if !c.stateMachine.CanTransition(StateStarting) {
		return fmt.Errorf("cannot start from state %s", c.stateMachine.State())
	}
	
	c.mu.Lock()
	c.config = &config
	c.mu.Unlock()
	
	if err := c.stateMachine.Transition(StateStarting); err != nil {
		return err
	}
	
	log.Printf("[DEBUG] Transitioned to Starting, calling initialize")
	// Initialize internal components
	if err := c.initialize(); err != nil {
		log.Printf("[DEBUG] Initialize failed: %v. Cleaning up...", err)
		c.cleanup() // Ensure we don't leak listeners on partial failure
		c.setLastError(err)
		c.stateMachine.Transition(StateError)
		return err
	}
	
	log.Printf("[DEBUG] Initialize success, transitioning to Active")
	c.setLastError(nil)
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
		c.setLastError(err)
		c.stateMachine.Transition(StateError)
		return err
	}
	
	c.setLastError(nil)
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
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	
	c.subCounter++
	id := fmt.Sprintf("sub-%d", c.subCounter)
	
	c.handlers[id] = handler
	
	return &Subscription{
		ID:      id,
		Handler: handler,
		cancel: func() {
			c.handlersMu.Lock()
			delete(c.handlers, id)
			c.handlersMu.Unlock()
		},
	}
}

// GetState returns current FSM state (for initialization recovery only, do not poll).
func (c *Core) GetState() string {
	return string(c.stateMachine.State())
}

// GetLastError returns the last error that occurred.
func (c *Core) GetLastError() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastError == nil {
		return ""
	}
	return c.lastError.Error()
}

func (c *Core) setLastError(err error) {
	c.mu.Lock()
	c.lastError = err
	c.mu.Unlock()
	// Also emit error event
	if err != nil {
		c.emit(NewCoreErrorEvent("CORE_ERROR", err.Error(), false))
	}
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
	
	// Check if critical addresses changed
	var oldListenAddr, oldHttpAddr string
	if c.config != nil {
		oldListenAddr = c.config.ListenAddr
		oldHttpAddr = c.config.HttpProxyAddr
	}
	
	c.config = &config
	
	// Save to disk
	if c.configManager != nil {
		if err := c.configManager.Save(&config); err != nil {
			log.Printf("[ERROR] Failed to save config: %v", err)
			return fmt.Errorf("failed to save config: %w", err)
		}
	}

	// Update session manager config if it exists
	if c.sessionMgr != nil {
		c.sessionMgr.updateConfig(&config)
	}

	// Update rules from config if they exist
	if len(config.Rules) > 0 {
		c.UpdateRules(config.Rules)
	}

	addressChanged := oldListenAddr != "" && (oldListenAddr != config.ListenAddr || oldHttpAddr != config.HttpProxyAddr)
	needsProxyRefresh := c.systemProxyEnabled && oldListenAddr != config.ListenAddr
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

		// Auto-start if we have a valid config now
		if err := c.Start(config); err != nil {
			// Return error so frontend knows it failed
			return fmt.Errorf("auto-start failed: %w", err)
		}
		return nil
	} else if currentState == StateActive {
		if addressChanged {
			log.Printf("[INFO] Proxy addresses changed, restarting core...")
			c.Close()
			return c.Start(config)
		}
		// If only other params changed, just rotate session
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
	
	// Filter logic is now handled globally by FilteredWriter in main.go
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
		if c.config == nil {
			return fmt.Errorf("no config loaded")
		}
		
		// Only use HTTP proxy for system proxy to ensure compatibility
		addr := c.config.HttpProxyAddr
		isHttp := true
		
		if addr == "" {
			return fmt.Errorf("system proxy requires http proxy to be configured")
		}
		
		log.Printf("[DEBUG] Enabling system proxy: %s (http=%v)", addr, isHttp)
		if err := systemproxy.EnableProxy(addr, isHttp); err != nil {
			return err
		}
		c.systemProxyEnabled = true
	} else {
		if err := systemproxy.DisableProxy(); err != nil {
			return err
		}
		c.systemProxyEnabled = false
	}
	
	// Emit an event (optional, but good for UI)
	// c.emit(NewProxyStateChangedEvent(c.systemProxyEnabled))
	
	return nil
}


// initialize sets up internal components.
func (c *Core) initialize() error {
	log.Printf("[DEBUG] initialize entered")
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = NewMetrics() // Use constructor!
	c.metricsCollector = NewMetricsCollector(c.metrics, 1*time.Second, c.emit)
	c.metricsCollector.Start()
	log.Printf("[DEBUG] Metrics started")

	c.ruleEngine = NewRuleEngine(ActionProxy) // Default to proxy
	
	// Defensive: Ensure HttpProxyAddr is set if system proxy is to be enabled via HTTP
	if c.config.HttpProxyAddr == "" {
		// Log warning?
		c.config.HttpProxyAddr = "127.0.0.1:1081"
	}
	
	// Add default rules if enabled
	if c.config.BlockAds {
		log.Printf("[DEBUG] Adding default ads rules")
		c.ruleEngine.AddRule(&Rule{
			ID:       "default-block-ads",
			Name:     "Block Ads",
			Priority: 1000, // High priority
			Enabled:  true,
			Action:   ActionBlock,
			Matches: []MatchCondition{
				{Type: MatchGeoSite, Value: "category-ads-all"},
			},
		})
	}
	
	if c.config.BypassCN {
		log.Printf("[DEBUG] Adding default bypass-cn rules")
		c.ruleEngine.AddRule(&Rule{
			ID:       "default-bypass-cn",
			Name:     "Bypass China",
			Priority: 900,
			Enabled:  true,
			Action:   ActionDirect,
			Matches: []MatchCondition{
				{Type: MatchGeoIP, Value: "CN"},
			},
		})
		c.ruleEngine.AddRule(&Rule{
			ID:       "default-bypass-cn-site",
			Name:     "Bypass China Sites",
			Priority: 901,
			Enabled:  true,
			Action:   ActionDirect,
			Matches: []MatchCondition{
				{Type: MatchGeoSite, Value: "cn"},
			},
		})
	}

	log.Printf("[DEBUG] Initializing session manager")
	c.sessionMgr = newSessionManager(c.config, c.emit, c.metrics)
	if err := c.sessionMgr.initialize(); err != nil {
		return err
	}

	log.Printf("[DEBUG] Starting SOCKS5 server on %s", c.config.ListenAddr)
	c.socksServer = newSocks5Server(c.config.ListenAddr, c)
	if c.socksServer != nil {
		if err := c.socksServer.start(); err != nil {
			return err
		}
	}

	if c.config.HttpProxyAddr != "" {
		log.Printf("[DEBUG] Starting HTTP proxy on %s", c.config.HttpProxyAddr)
		c.httpProxyServer = newHttpProxyServer(c.config.HttpProxyAddr, c)
		if err := c.httpProxyServer.Start(); err != nil {
			return err
		}
	}

	// Connect to upstream (if configured)
	if c.config.URL != "" {
		log.Printf("[DEBUG] Connecting to upstream: %s", c.config.URL)
		if err := c.sessionMgr.connect(); err != nil {
			return err
		}
	}

	log.Printf("[DEBUG] initialize finished")
	return nil
}

// cleanup releases all resources.
func (c *Core) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Disable system proxy if it was enabled by this Core
	if c.systemProxyEnabled {
		systemproxy.DisableProxy()
		c.systemProxyEnabled = false
	}

	if c.metricsCollector != nil {
		c.metricsCollector.Stop()
	}

	if c.socksServer != nil {
		c.socksServer.stop()
	}
	if c.httpProxyServer != nil {
		c.httpProxyServer.Stop()
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
	stream, streamID, err := c.sessionMgr.OpenStream(c.ctx)
	if err != nil {
		log.Printf("[DEBUG] Open stream to %s:%d failed: %v", target.Host, target.Port, err)
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
