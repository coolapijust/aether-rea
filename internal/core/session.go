package core

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log"
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

// updateConfig updates the session manager's configuration.
func (sm *sessionManager) updateConfig(config *SessionConfig) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	sm.config = config
	// Force dialer re-initialization on next connect
	sm.dialer = nil
}

// initialize sets up the dialer without connecting.
func (sm *sessionManager) initialize() error {
	if sm.config.URL == "" {
		return nil
	}

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
			ServerName:         parsed.Hostname(),
			NextProtos:         []string{http3.NextProtoH3},
			InsecureSkipVerify: sm.config.AllowInsecure,
		},
		QUICConfig: quicConfig,
	}

	if sm.config.AllowInsecure {
		log.Printf("[WARNING] TLS InsecureSkipVerify is ENABLED. This is intended for debugging or private gateways ONLY.")
	}
	log.Printf("[DEBUG] WebTransport dialer initialized for %s", parsed.Hostname())

	return nil
}

// connect establishes the initial session.
func (sm *sessionManager) connect() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// If no URL configured, stay idle
	if sm.config.URL == "" {
		return nil
	}

	if sm.dialer == nil {
		// Try to initialize if needed (e.g. config updated)
		if err := sm.initialize(); err != nil {
			return err
		}
		if sm.dialer == nil {
			return fmt.Errorf("dialer not initialized")
		}
	}

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

	// Parse and normalize base URL
	u, err := url.Parse(sm.config.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid config url: %w", err)
	}

	// Ensure port is present in the URL host (required by quic-go/webtransport)
	if u.Port() == "" {
		// Robustness: Check if port was accidentally appended to the path
		// e.g., https://example.com/v1/api/sync:8080
		if lastColon := strings.LastIndex(u.Path, ":"); lastColon != -1 {
			possiblePort := u.Path[lastColon+1:]
			// Check if it's a numeric port
			isPort := true
			for _, c := range possiblePort {
				if c < '0' || c > '9' {
					isPort = false
					break
				}
			}
			if isPort && possiblePort != "" {
				newPort := possiblePort
				u.Path = u.Path[:lastColon]
				u.Host = net.JoinHostPort(u.Hostname(), newPort)
				log.Printf("[WARNING] Misplaced port detected in URL path. Auto-corrected to %s (Path: %s)", u.Host, u.Path)
			}
		}

		// If still no port, use default
		if u.Port() == "" {
			defaultPort := "443"
			if u.Scheme == "http" {
				defaultPort = "80"
			}
			u.Host = net.JoinHostPort(u.Hostname(), defaultPort)
		}
	}
	
	// Handle DialAddr override (e.g. for IP optimization)
	finalAddr := u.Host
	if sm.config.DialAddr != "" {
		host, port, err := net.SplitHostPort(sm.config.DialAddr)
		if err != nil {
			// Handle missing port error (common for raw IPs/domains)
			if strings.Contains(err.Error(), "missing port") || strings.Contains(err.Error(), "too many colons") {
				host = sm.config.DialAddr
				port = "443" 
			} else {
				return nil, fmt.Errorf("invalid dial addr: %w", err)
			}
		}
		u.Host = net.JoinHostPort(host, port)
		finalAddr = u.Host
	}

	log.Printf("[DEBUG] Dialing WebTransport: %s (Target Host: %s)", u.String(), finalAddr)
	_, sess, err := sm.dialer.Dial(ctx, u.String(), nil)
	if err != nil {
		log.Printf("[DEBUG] Dial failed: %v", err)
		return nil, fmt.Errorf("dial to %s failed: %w", u.Host, err)
	}

	return sess, nil
}

func (sm *sessionManager) monitorSession() {
	if sm.session == nil {
		return
	}
	// Wait for context cancellation or session close in background
	go func() {
		<-sm.ctx.Done()
		sm.mu.Lock()
		if sm.session != nil {
			reason := "closed"
			log.Printf("[DEBUG] Session %s closed (reason: context done)", sm.sessionID)
			sm.onEvent(NewSessionClosedEvent(sm.sessionID, &reason, nil))
			sm.session = nil
		}
		sm.mu.Unlock()
		sm.metrics.RecordSessionEnd()
	}()

	// Periodic ping loop
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-ticker.C:
			sm.pingOnce()
		}
	}
}

// pingOnce performs a single latency measurement.
func (sm *sessionManager) pingOnce() {
	sm.mu.RLock()
	sess := sm.session
	sm.mu.RUnlock()

	if sess == nil {
		return
	}

	start := time.Now()
	
	// Create a short-lived stream for ping
	ctx, cancel := context.WithTimeout(sm.ctx, 5*time.Second)
	defer cancel()
	
	stream, err := sess.OpenStreamSync(ctx)
	if err != nil {
		return
	}
	defer stream.Close()

	// Send Ping record
	pingRecord := make([]byte, 4+RecordHeaderLength)
	binary.BigEndian.PutUint32(pingRecord[0:4], uint32(RecordHeaderLength))
	pingRecord[4] = TypePing
	
	if _, err := stream.Write(pingRecord); err != nil {
		return
	}

	// Read response (Pong or Error)
	buf := make([]byte, 4+RecordHeaderLength)
	if _, err := io.ReadFull(stream, buf); err != nil {
		// Even if it fails, the time taken is a rough indicator of latency
		// but we prefer clean responses
		return
	}

	latency := time.Since(start).Milliseconds()
	sm.metrics.RecordLatency(latency)
}

// generateSessionID creates a unique session identifier.
func generateSessionID() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}
