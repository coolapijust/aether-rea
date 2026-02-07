package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	webtransport "github.com/quic-go/webtransport-go"
)

// sessionManager manages WebTransport sessions and their lifecycle.
type sessionManager struct {
	config       *SessionConfig
	dialer       *webtransport.Dialer
	session      *webtransport.Session
	sessionID    string
	counter      uint64
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	onEvent      func(Event)
	metrics      *Metrics
}

// newSessionManager creates a new session manager.
func newSessionManager(config *SessionConfig, onEvent func(Event), metrics *Metrics) *sessionManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &sessionManager{
		config:  config,
		onEvent: onEvent,
		metrics: metrics,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// initialize sets up the dialer without connecting.
func (sm *sessionManager) initialize() error {
	parsed, err := url.Parse(sm.config.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("url must be https")
	}

	quicConfig := &quic.Config{
		KeepAlivePeriod: 20 * time.Second,
		MaxIdleTimeout:  60 * time.Second,
		EnableDatagrams: true,
	}

	sm.dialer = &webtransport.Dialer{
		TLSClientConfig: &tls.Config{
			ServerName: parsed.Hostname(),
			NextProtos: []string{http3.NextProtoH3},
		},
		QUICConfig: quicConfig,
	}

	return nil
}

// connect establishes the initial session.
func (sm *sessionManager) connect() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.session != nil {
		return fmt.Errorf("session already exists")
	}

	session, err := sm.dialSession(sm.ctx)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	sm.session = session
	sm.sessionID = generateSessionID()
	sm.counter = 0
	sm.metrics.RecordSessionStart()

	// Emit event
	localAddr := ""
	remoteAddr := ""
	// webtransport.Session doesn't have Connection() method
	// Get address from the dial context if needed
	sm.onEvent(NewSessionEstablishedEvent(sm.sessionID, localAddr, remoteAddr))

	// Start session monitor
	go sm.monitorSession()

	return nil
}

// rotate closes current session and establishes a new one.
func (sm *sessionManager) rotate() error {
	sm.mu.Lock()
	oldSession := sm.session
	oldID := sm.sessionID
	sm.mu.Unlock()

	if oldSession != nil {
		sm.onEvent(NewSessionRotatingEvent(oldID))
		_ = oldSession.CloseWithError(0, "rotation")
	}

	// Clear session state
	sm.mu.Lock()
	sm.session = nil
	sm.counter = 0
	sm.mu.Unlock()

	// Connect new session
	return sm.connect()
}

// close gracefully closes the session.
func (sm *sessionManager) close(reason string) error {
	sm.cancel()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.session != nil {
		_ = sm.session.CloseWithError(0, reason)
		sm.onEvent(NewSessionClosedEvent(sm.sessionID, &reason, nil))
		sm.session = nil
	}

	sm.metrics.RecordSessionEnd()
	return nil
}

// getSession returns current session and increments counter.
func (sm *sessionManager) getSession(ctx context.Context) (*webtransport.Session, uint64, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.session == nil {
		return nil, 0, fmt.Errorf("no active session")
	}

	sm.counter++
	return sm.session, sm.counter, nil
}

// dialSession creates a new WebTransport session.
func (sm *sessionManager) dialSession(ctx context.Context) (*webtransport.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dialURL := sm.config.URL
	
	// Handle DialAddr override (e.g. for IP optimization)
	if sm.config.DialAddr != "" {
		host, port, err := net.SplitHostPort(sm.config.DialAddr)
		if err != nil {
			// If missing port, assume 443
			if strings.Contains(err.Error(), "missing port") {
				host = sm.config.DialAddr
				port = "443"
			} else {
				return nil, err
			}
		}
		
		parsed, err := url.Parse(sm.config.URL)
		if err == nil {
			parsed.Host = net.JoinHostPort(host, port)
			dialURL = parsed.String()
		}
	}

	_, sess, err := sm.dialer.Dial(ctx, dialURL, nil)
	if err != nil {
		return nil, err
	}

	return sess, nil
}

// monitorSession watches for session closure.
func (sm *sessionManager) monitorSession() {
	if sm.session == nil {
		return
	}

	// Wait for context cancellation or session close
	<-sm.ctx.Done()

	sm.mu.Lock()
	if sm.session != nil {
		reason := "closed"
		sm.onEvent(NewSessionClosedEvent(sm.sessionID, &reason, nil))
		sm.session = nil
	}
	sm.mu.Unlock()

	sm.metrics.RecordSessionEnd()
}

// generateSessionID creates a unique session identifier.
func generateSessionID() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}
