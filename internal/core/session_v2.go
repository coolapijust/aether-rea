package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	webtransport "github.com/quic-go/webtransport-go"
)

// sessionV2 represents a managed WebTransport session with lifecycle metadata.
type sessionV2 struct {
	id        string
	session   *webtransport.Session
	createdAt time.Time
	state     sessionState
	counter   uint64
}

type sessionState int

const (
	sessionStateActive sessionState = iota
	sessionStateDraining // Accepting existing streams, no new streams
	sessionStateClosed
)

// sessionManagerV2 manages multiple sessions for seamless rotation.
type sessionManagerV2 struct {
	config    *SessionConfig
	onEvent   func(Event)
	metrics   *Metrics
	
	// Session management
	mu          sync.RWMutex
	sessions    map[string]*sessionV2
	primaryID   string // Current active session for new streams
	warmingID   string // Session being pre-warmed
	
	// Rotation
	scheduler *rotationScheduler
	
	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// newSessionManagerV2 creates a new session manager with rotation support.
func newSessionManagerV2(config *SessionConfig, onEvent func(Event), metrics *Metrics) *sessionManagerV2 {
	ctx, cancel := context.WithCancel(context.Background())
	
	sm := &sessionManagerV2{
		config:   config,
		onEvent:  onEvent,
		metrics:  metrics,
		sessions: make(map[string]*sessionV2),
		ctx:      ctx,
		cancel:   cancel,
	}
	
	// Setup rotation scheduler
	if config.Rotation.Enabled {
		policy := config.Rotation.toPolicy()
		sm.scheduler = newRotationScheduler(policy, sm.preWarmSession, sm.performRotation, sm.onRotationScheduled)
	}
	
	return sm
}

// initialize sets up the dialer.
func (sm *sessionManagerV2) initialize() error {
	// Initialize base dialer (similar to v1)
	return nil // TODO: implement
}

// start establishes the initial session and starts rotation scheduler.
func (sm *sessionManagerV2) start() error {
	// Create initial session
	session, err := sm.createSession()
	if err != nil {
		return fmt.Errorf("failed to create initial session: %w", err)
	}
	
	sm.mu.Lock()
	sm.sessions[session.id] = session
	sm.primaryID = session.id
	sm.mu.Unlock()
	
	// Emit event
	sm.emitSessionEstablished(session)
	
	// Start rotation scheduler
	if sm.scheduler != nil {
		sm.scheduler.start()
	}
	
	return nil
}

// getSessionForNewStream returns the current primary session for new streams.
func (sm *sessionManagerV2) getSessionForNewStream() (*sessionV2, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	if sm.primaryID == "" {
		return nil, fmt.Errorf("no active session")
	}
	
	session, ok := sm.sessions[sm.primaryID]
	if !ok || session.state != sessionStateActive {
		return nil, fmt.Errorf("primary session not available")
	}
	
	session.counter++
	return session, nil
}

// getSessionByID returns a session by ID (for existing streams).
func (sm *sessionManagerV2) getSessionByID(id string) (*sessionV2, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	session, ok := sm.sessions[id]
	if !ok || session.state == sessionStateClosed {
		return nil, fmt.Errorf("session not found or closed")
	}
	
	return session, nil
}

// preWarmSession creates a new session in preparation for rotation.
// This is called by the rotation scheduler.
func (sm *sessionManagerV2) preWarmSession() {
	newSession, err := sm.createSession()
	if err != nil {
		sm.onEvent(NewCoreErrorEvent(ErrNetwork, fmt.Sprintf("pre-warm failed: %v", err), false))
		return
	}
	
	sm.mu.Lock()
	sm.sessions[newSession.id] = newSession
	sm.warmingID = newSession.id
	sm.mu.Unlock()
	
	sm.onEvent(NewRotationPreWarmStartedEvent(newSession.id))
	sm.emitSessionEstablished(newSession)
}

// performRotation switches to the pre-warmed session.
// This is called by the rotation scheduler.
func (sm *sessionManagerV2) performRotation() {
	sm.mu.Lock()
	oldPrimaryID := sm.primaryID
	newPrimaryID := sm.warmingID
	
	if newPrimaryID == "" {
		sm.mu.Unlock()
		sm.onEvent(NewCoreErrorEvent(ErrNetwork, "rotation failed: no pre-warmed session", false))
		return
	}
	
	// Switch primary
	sm.primaryID = newPrimaryID
	sm.warmingID = ""
	
	// Mark old session as draining
	if oldSession, ok := sm.sessions[oldPrimaryID]; ok {
		oldSession.state = sessionStateDraining
		// Schedule cleanup after drain period
		go sm.drainAndCloseSession(oldPrimaryID, 2*time.Minute)
	}
	
	sm.mu.Unlock()
	
	sm.onEvent(NewRotationCompletedEvent(oldPrimaryID, newPrimaryID, 2*time.Minute))
}

// drainAndCloseSession waits for existing streams to close, then closes the session.
func (sm *sessionManagerV2) drainAndCloseSession(id string, timeout time.Duration) {
	// Wait for drain timeout or all streams to close
	time.Sleep(timeout)
	
	sm.mu.Lock()
	session, ok := sm.sessions[id]
	if !ok {
		sm.mu.Unlock()
		return
	}
	sm.mu.Unlock()
	
	// Close the session
	if session.session != nil {
		_ = session.session.CloseWithError(0, "drained")
	}
	
	sm.mu.Lock()
	session.state = sessionStateClosed
	delete(sm.sessions, id)
	sm.mu.Unlock()
	
	sm.onEvent(NewSessionClosedEvent(id, strPtr("drained"), nil))
}

// onRotationScheduled is called when next rotation is scheduled.
func (sm *sessionManagerV2) onRotationScheduled(nextRotation time.Time) {
	policy := sm.config.Rotation.toPolicy()
	sm.onEvent(NewRotationScheduledEvent(nextRotation, policy.MinInterval, policy.MaxInterval))
}

// manualRotate triggers an immediate rotation.
func (sm *sessionManagerV2) manualRotate() error {
	if sm.scheduler != nil {
		sm.scheduler.stop()
		defer sm.scheduler.start()
	}
	
	sm.preWarmSession()
	
	// Small delay to ensure pre-warm completes
	time.Sleep(100 * time.Millisecond)
	
	sm.performRotation()
	return nil
}

// close gracefully closes all sessions.
func (sm *sessionManagerV2) close(reason string) error {
	if sm.scheduler != nil {
		sm.scheduler.stop()
	}
	
	sm.cancel()
	
	sm.mu.Lock()
	sessions := make([]*sessionV2, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		sessions = append(sessions, s)
	}
	sm.mu.Unlock()
	
	// Close all sessions
	for _, s := range sessions {
		if s.session != nil {
			_ = s.session.CloseWithError(0, reason)
		}
		sm.onEvent(NewSessionClosedEvent(s.id, &reason, nil))
	}
	
	return nil
}

// createSession creates a new WebTransport session.
func (sm *sessionManagerV2) createSession() (*sessionV2, error) {
	// TODO: Implement actual session creation using dialer
	return &sessionV2{
		id:        generateSessionID(),
		createdAt: time.Now(),
		state:     sessionStateActive,
	}, nil
}

// emitSessionEstablished emits the session.established event.
func (sm *sessionManagerV2) emitSessionEstablished(s *sessionV2) {
	localAddr := ""
	remoteAddr := ""
	// webtransport.Session doesn't have Connection() method
	sm.onEvent(NewSessionEstablishedEvent(s.id, localAddr, remoteAddr))
}

// Helper functions
func strPtr(s string) *string {
	return &s
}
