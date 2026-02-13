package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	config    *SessionConfig
	dialer    *webtransport.Dialer
	session   *webtransport.Session
	sessionID string
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	onEvent   func(Event)
	metrics   *Metrics
	nonceGen  *NonceGenerator // V5: Counter-based nonce generator
	streamSeq uint64
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

func init() {
	rand.Seed(time.Now().UnixNano())
}

// updateConfig updates the session manager's configuration.
func (sm *sessionManager) updateConfig(config *SessionConfig) {
	sm.mu.Lock()
	oldProfile := ""
	if sm.config != nil {
		oldProfile = sm.config.WindowProfile
	}
	sm.config = config
	newProfile := config.WindowProfile
	sm.mu.Unlock()

	// If window profile changed, we need to recreate the dialer
	// so that the next session (after rotation) uses the new window settings.
	if oldProfile != newProfile {
		log.Printf("[DEBUG] Window profile changed from '%s' to '%s', reinitializing dialer", oldProfile, newProfile)
		if err := sm.initialize(); err != nil {
			log.Printf("[ERROR] Failed to reinitialize dialer after config change: %v", err)
		}
	}
}

// initialize sets up the dialer without connecting.
func (sm *sessionManager) initialize() error {
	if sm.config.URL == "" {
		return nil
	}
	if sm.config.RecordPayloadBytes > 0 {
		applied := SetRecordPayloadBytes(sm.config.RecordPayloadBytes)
		log.Printf("[DEBUG] V5.1 Config: record payload bytes=%d", applied)
	}

	parsed, err := url.Parse(sm.config.URL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("url must be https")
	}

	// V5.2: Profile defaults + optional explicit QUIC window overrides.
	windowCfg, err := ResolveQUICWindowConfig(sm.config.WindowProfile)
	if err != nil {
		return fmt.Errorf("invalid QUIC window config: %w", err)
	}
	if windowCfg.OverrideApplied {
		log.Printf(
			"[DEBUG] V5.2 Config: WINDOW_PROFILE=%s + manual QUIC windows (init_stream=%d init_conn=%d max_stream=%d max_conn=%d)",
			windowCfg.Profile,
			windowCfg.InitialStreamReceiveWindow,
			windowCfg.InitialConnectionReceiveWindow,
			windowCfg.MaxStreamReceiveWindow,
			windowCfg.MaxConnectionReceiveWindow,
		)
	} else {
		log.Printf(
			"[DEBUG] V5.2 Config: WINDOW_PROFILE=%s (init_stream=%d init_conn=%d max_stream=%d max_conn=%d)",
			windowCfg.Profile,
			windowCfg.InitialStreamReceiveWindow,
			windowCfg.InitialConnectionReceiveWindow,
			windowCfg.MaxStreamReceiveWindow,
			windowCfg.MaxConnectionReceiveWindow,
		)
	}

	quicConfig := &quic.Config{
		KeepAlivePeriod:                20 * time.Second,
		MaxIdleTimeout:                 60 * time.Second,
		EnableDatagrams:                true,
		EnableStreamResetPartialDelivery: true,
		Allow0RTT:                      true,
		MaxIncomingStreams:             1000,
		InitialStreamReceiveWindow:     windowCfg.InitialStreamReceiveWindow,
		InitialConnectionReceiveWindow: windowCfg.InitialConnectionReceiveWindow,
		MaxStreamReceiveWindow:         windowCfg.MaxStreamReceiveWindow,
		MaxConnectionReceiveWindow:     windowCfg.MaxConnectionReceiveWindow,
	}

	// V5.1 Performance Fix: Create a dedicated UDP socket with massive buffers
	// This ensures the client can absorb 16KB high-frequency bursts from the server
	// without kernel-level drops, which is critical for 8Mbps+ throughput.
	udpAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return fmt.Errorf("failed to resolve local udp: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("failed to bind local udp: %w", err)
	}

	const bufSize = 32 * 1024 * 1024 // 32MB Read Buffer
	if err := udpConn.SetReadBuffer(bufSize); err != nil {
		log.Printf("Warning: Failed to set client UDP read buffer: %v", err)
	}
	if err := udpConn.SetWriteBuffer(bufSize); err != nil {
		log.Printf("Warning: Failed to set client UDP write buffer: %v", err)
	}

	// Create a transport that uses this optimized connection
	tr := &quic.Transport{
		Conn: udpConn,
	}

	sm.dialer = &webtransport.Dialer{
		TLSClientConfig: &tls.Config{
			ServerName:         parsed.Hostname(),
			NextProtos:         []string{http3.NextProtoH3},
			InsecureSkipVerify: sm.config.AllowInsecure,
		},
		QUICConfig: quicConfig,
		DialAddr: func(ctx context.Context, addr string, tlsCfg *tls.Config, cfg *quic.Config) (*quic.Conn, error) {
			// Resolve the target address manually to ensure we dial correctly.
			udpAddr, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				return nil, err
			}
			return tr.DialEarly(ctx, udpAddr, tlsCfg, cfg)
		},
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
	return sm.connectLocked()
}

// connectLocked establishes session while holding the lock.
func (sm *sessionManager) connectLocked() error {
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

	// V5: Initialize NonceGenerator for counter-based nonce
	sm.nonceGen, err = NewNonceGenerator()
	if err != nil {
		_ = session.CloseWithError(0, "nonce generator failed")
		return fmt.Errorf("nonce generator failed: %w", err)
	}

	sm.metrics.RecordSessionStart()

	// Emit event
	localAddr := ""
	remoteAddr := ""
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

// OpenStream opens a new stream and returns it with a synchronized counter.
func (sm *sessionManager) OpenStream(ctx context.Context) (*webtransport.Stream, uint64, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.session == nil {
		if err := sm.connectLocked(); err != nil {
			return nil, 0, err
		}
	}

	stream, err := sm.session.OpenStreamSync(ctx)
	if err != nil {
		// If session error, try to reconnect and retry once
		log.Printf("[DEBUG] Open stream failed (session might be dead), retrying: %v", err)
		sm.session = nil
		if err := sm.connectLocked(); err != nil {
			return nil, 0, err
		}
		stream, err = sm.session.OpenStreamSync(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	sm.streamSeq++
	return stream, sm.streamSeq, nil
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

	// Periodic ping loop with jitter
	for {
		select {
		case <-sm.ctx.Done():
			return
		case <-time.After(jitterDuration(4*time.Second, 7*time.Second)):
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

	stream, _, err := sm.OpenStream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()

	pingRecord, err := BuildPingRecord(sm.nonceGen)
	if err != nil {
		return
	}
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

func jitterDuration(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	diff := max - min
	return min + time.Duration(rand.Int63n(int64(diff)+1))
}

