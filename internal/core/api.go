package core

import (
	"context"
	"fmt"
	"sync"
)

// SessionConfig is JSON-serializable configuration for Core.
type SessionConfig struct {
	URL            string `json:"url"`                      // https://host/path
	PSK            string `json:"psk"`                      // Pre-shared key
	ListenAddr     string `json:"listenAddr"`               // SOCKS5 listen address
	DialAddr       string `json:"dialAddr,omitempty"`       // Override dial address (optional)
	RotateInterval int    `json:"rotateInterval,omitempty"` // Milliseconds, 0 = no auto-rotation
	MaxPadding     int    `json:"maxPadding,omitempty"`     // 0-65535, default 0
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

// CoreState represents the current FSM state (string for JSON).
type CoreState = string

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
	handlers     map[string]EventHandler
	handlerMu    sync.RWMutex
	subCounter   int64
	
	// Internal components (not exposed)
	sessionMgr       *sessionManager
	socksServer      *socks5Server
	metrics          *Metrics
	metricsCollector *MetricsCollector
	eventBus         chan Event
	ctx              context.Context
	cancel           context.CancelFunc
}

// New creates a new Core instance.
func New() *Core {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Core{
		handlers:   make(map[string]EventHandler),
		eventBus:   make(chan Event, 100), // Buffered to prevent blocking
		ctx:        ctx,
		cancel:     cancel,
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
func (c *Core) GetState() CoreState {
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
		// Channel full, drop event (or log)
	}
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
	// TODO: Initialize session manager, socks5 server
	go c.runEventLoop()
	return nil
}

// cleanup releases all resources.
func (c *Core) cleanup() {
	// TODO: Close all streams, close session, stop socks5 server
}

// performRotation handles session rotation.
func (c *Core) performRotation() error {
	// TODO: Implement rotation logic
	return nil
}

// openStreamInternal creates a stream (protocol internal).
func (c *Core) openStreamInternal(target TargetAddress, options map[string]interface{}) (StreamHandle, error) {
	// TODO: Implement stream creation
	return StreamHandle{ID: "stream-1"}, nil
}

// closeStreamInternal closes a stream.
func (c *Core) closeStreamInternal(handle StreamHandle) error {
	// TODO: Implement stream close
	return nil
}
