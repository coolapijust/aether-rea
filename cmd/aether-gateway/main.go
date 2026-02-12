package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"aether-rea/internal/core"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/webtransport-go"
)

// BufferedWriteCloser buffers writes and flushes on close
type BufferedWriteCloser struct {
	*bufio.Writer
	closer io.Closer
}

func NewBufferedWriteCloser(writer *bufio.Writer, closer io.Closer) *BufferedWriteCloser {
	return &BufferedWriteCloser{
		Writer: writer,
		closer: closer,
	}
}

func (b *BufferedWriteCloser) Close() error {
	if err := b.Writer.Flush(); err != nil {
		return err
	}
	return b.closer.Close()
}

var (
	listenAddr = flag.String("listen", ":8080", "Listen address")
	certFile   = flag.String("cert", "cert.pem", "TLS certificate file")
	keyFile    = flag.String("key", "key.pem", "TLS key file")
	psk        = flag.String("psk", "", "Pre-shared key")
	secretPath = flag.String("path", "/v1/api/sync", "Secret path for WebTransport")
	decoyRoot  = flag.String("decoy", "", "Path to the decoy/masquerade static website root")
)

type gatewayPerfStats struct {
	wtToTCPBytes atomic.Uint64
	wtToTCPWrites atomic.Uint64
	wtToTCPWriteNanos atomic.Uint64

	tcpToWTBytes atomic.Uint64
	tcpToWTWrites atomic.Uint64
	tcpToWTWriteNanos atomic.Uint64

	tcpToWTReadWaitCalls atomic.Uint64
	tcpToWTReadWaitNanos atomic.Uint64
	tcpToWTBuildCalls atomic.Uint64
	tcpToWTBuildNanos atomic.Uint64
	tcpToWTFlushCalls atomic.Uint64
	tcpToWTFlushBytes atomic.Uint64
}

var gwPerf gatewayPerfStats

func (s *gatewayPerfStats) observeWTToTCP(bytes int, d time.Duration) {
	if bytes <= 0 {
		return
	}
	s.wtToTCPBytes.Add(uint64(bytes))
	s.wtToTCPWrites.Add(1)
	s.wtToTCPWriteNanos.Add(uint64(d.Nanoseconds()))
}

func (s *gatewayPerfStats) observeTCPToWT(bytes int, d time.Duration) {
	if bytes <= 0 {
		return
	}
	s.tcpToWTBytes.Add(uint64(bytes))
	s.tcpToWTWrites.Add(1)
	s.tcpToWTWriteNanos.Add(uint64(d.Nanoseconds()))
}

func (s *gatewayPerfStats) observeTCPReadWait(d time.Duration) {
	s.tcpToWTReadWaitCalls.Add(1)
	s.tcpToWTReadWaitNanos.Add(uint64(d.Nanoseconds()))
}

func (s *gatewayPerfStats) observeTCPBuild(d time.Duration) {
	s.tcpToWTBuildCalls.Add(1)
	s.tcpToWTBuildNanos.Add(uint64(d.Nanoseconds()))
}

func (s *gatewayPerfStats) observeTCPFlush(bytes int) {
	if bytes <= 0 {
		return
	}
	s.tcpToWTFlushCalls.Add(1)
	s.tcpToWTFlushBytes.Add(uint64(bytes))
}

func startGatewayPerfReporter() {
	if os.Getenv("PERF_DIAG_ENABLE") != "1" {
		return
	}

	interval := 10 * time.Second
	if v := os.Getenv("PERF_DIAG_INTERVAL_SEC"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			interval = time.Duration(sec) * time.Second
		}
	}

	log.Printf("[PERF-GW] enabled=true interval=%s", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var prevWTToTCPBytes, prevWTToTCPWrites, prevWTToTCPNanos uint64
		var prevTCPToWTBytes, prevTCPToWTWrites, prevTCPToWTNanos uint64
		var prevTCPReadWaitCalls, prevTCPReadWaitNanos uint64
		var prevTCPBuildCalls, prevTCPBuildNanos uint64
		var prevTCPFlushCalls, prevTCPFlushBytes uint64

		for range ticker.C {
			curWTToTCPBytes := gwPerf.wtToTCPBytes.Load()
			curWTToTCPWrites := gwPerf.wtToTCPWrites.Load()
			curWTToTCPNanos := gwPerf.wtToTCPWriteNanos.Load()
			curTCPToWTBytes := gwPerf.tcpToWTBytes.Load()
			curTCPToWTWrites := gwPerf.tcpToWTWrites.Load()
			curTCPToWTNanos := gwPerf.tcpToWTWriteNanos.Load()
			curTCPReadWaitCalls := gwPerf.tcpToWTReadWaitCalls.Load()
			curTCPReadWaitNanos := gwPerf.tcpToWTReadWaitNanos.Load()
			curTCPBuildCalls := gwPerf.tcpToWTBuildCalls.Load()
			curTCPBuildNanos := gwPerf.tcpToWTBuildNanos.Load()
			curTCPFlushCalls := gwPerf.tcpToWTFlushCalls.Load()
			curTCPFlushBytes := gwPerf.tcpToWTFlushBytes.Load()

			dWTToTCPBytes := curWTToTCPBytes - prevWTToTCPBytes
			dWTToTCPWrites := curWTToTCPWrites - prevWTToTCPWrites
			dWTToTCPNanos := curWTToTCPNanos - prevWTToTCPNanos
			dTCPToWTBytes := curTCPToWTBytes - prevTCPToWTBytes
			dTCPToWTWrites := curTCPToWTWrites - prevTCPToWTWrites
			dTCPToWTNanos := curTCPToWTNanos - prevTCPToWTNanos
			dTCPReadWaitCalls := curTCPReadWaitCalls - prevTCPReadWaitCalls
			dTCPReadWaitNanos := curTCPReadWaitNanos - prevTCPReadWaitNanos
			dTCPBuildCalls := curTCPBuildCalls - prevTCPBuildCalls
			dTCPBuildNanos := curTCPBuildNanos - prevTCPBuildNanos
			dTCPFlushCalls := curTCPFlushCalls - prevTCPFlushCalls
			dTCPFlushBytes := curTCPFlushBytes - prevTCPFlushBytes

			prevWTToTCPBytes, prevWTToTCPWrites, prevWTToTCPNanos = curWTToTCPBytes, curWTToTCPWrites, curWTToTCPNanos
			prevTCPToWTBytes, prevTCPToWTWrites, prevTCPToWTNanos = curTCPToWTBytes, curTCPToWTWrites, curTCPToWTNanos
			prevTCPReadWaitCalls, prevTCPReadWaitNanos = curTCPReadWaitCalls, curTCPReadWaitNanos
			prevTCPBuildCalls, prevTCPBuildNanos = curTCPBuildCalls, curTCPBuildNanos
			prevTCPFlushCalls, prevTCPFlushBytes = curTCPFlushCalls, curTCPFlushBytes

			sec := interval.Seconds()
			ulMbps := float64(dWTToTCPBytes*8) / 1_000_000.0 / sec
			dlMbps := float64(dTCPToWTBytes*8) / 1_000_000.0 / sec

			ulWriteUs := 0.0
			if dWTToTCPWrites > 0 {
				ulWriteUs = (float64(dWTToTCPNanos) / float64(dWTToTCPWrites)) / 1000.0
			}
			dlWriteUs := 0.0
			if dTCPToWTWrites > 0 {
				dlWriteUs = (float64(dTCPToWTNanos) / float64(dTCPToWTWrites)) / 1000.0
			}

			log.Printf(
				"[PERF-GW] window=%s dl{mbps=%.2f writes=%d write_us=%.1f} ul{mbps=%.2f writes=%d write_us=%.1f}",
				interval, dlMbps, dTCPToWTWrites, dlWriteUs, ulMbps, dWTToTCPWrites, ulWriteUs,
			)

			readWaitUs := 0.0
			if dTCPReadWaitCalls > 0 {
				readWaitUs = (float64(dTCPReadWaitNanos) / float64(dTCPReadWaitCalls)) / 1000.0
			}
			buildUs := 0.0
			if dTCPBuildCalls > 0 {
				buildUs = (float64(dTCPBuildNanos) / float64(dTCPBuildCalls)) / 1000.0
			}
			flushAvgBytes := 0.0
			if dTCPFlushCalls > 0 {
				flushAvgBytes = float64(dTCPFlushBytes) / float64(dTCPFlushCalls)
			}
			log.Printf(
				"[PERF-GW2] window=%s dl_stage{read_wait_us=%.1f reads=%d build_us=%.1f builds=%d write_block_us=%.1f writes=%d flush_avg_bytes=%.1f flushes=%d}",
				interval,
				readWaitUs, dTCPReadWaitCalls,
				buildUs, dTCPBuildCalls,
				dlWriteUs, dTCPToWTWrites,
				flushAvgBytes, dTCPFlushCalls,
			)
		}
	}()
}

func main() {
	flag.Parse()
	mathrand.Seed(time.Now().UnixNano())
	startGatewayPerfReporter()

	log.Printf("Aether Gateway 3.2.0 starting")

	// Support $PORT or $LISTEN_ADDR environment variables
	if envPort := os.Getenv("PORT"); envPort != "" {
		*listenAddr = "0.0.0.0:" + envPort
		log.Printf("Config: Using PORT environment variable: %s", *listenAddr)
	} else if envAddr := os.Getenv("LISTEN_ADDR"); envAddr != "" {
		*listenAddr = envAddr
		log.Printf("Config: Using LISTEN_ADDR environment variable: %s", *listenAddr)
	} else {
		// Ensure we bind to all interfaces if just a port is provided or default
		if strings.HasPrefix(*listenAddr, ":") {
			*listenAddr = "0.0.0.0" + *listenAddr
		}
		log.Printf("Config: Using default/flag listen address: %s", *listenAddr)
	}

	// Support $PSK and $DOMAIN environment variables
	if envPSK := os.Getenv("PSK"); envPSK != "" && *psk == "" {
		*psk = envPSK
	}
	domainEnv := os.Getenv("DOMAIN")

	// Support $SSL_CERT_FILE and $SSL_KEY_FILE for platform managed certs
	if envCert := os.Getenv("SSL_CERT_FILE"); envCert != "" {
		*certFile = envCert
	}
	if envKey := os.Getenv("SSL_KEY_FILE"); envKey != "" {
		*keyFile = envKey
	}
	if envDecoy := os.Getenv("DECOY_ROOT"); envDecoy != "" {
		*decoyRoot = envDecoy
	}

	if *psk == "" {
		log.Println("ERROR: PSK is required. Please set -psk flag or PSK environment variable.")
		os.Exit(1)
	}
	// Normalize PSK (Trim whitespace to avoid common config issues)
	*psk = strings.TrimSpace(*psk)
	if len(*psk) > 4 {
		log.Printf("Config: PSK loaded (Length: %d, Prefix: %s...)", len(*psk), (*psk)[:4])
	} else {
		log.Printf("Config: PSK loaded (Length: %d)", len(*psk))
	}

	// Initialize Certificate Loader for hot-reloading
	certLoader, err := NewCertificateLoader(*certFile, *keyFile)
	if err != nil {
		// Fallback to self-signed if loading failed
		// V5: We always generate a 10-year self-signed cert if the provided path is missing
		log.Printf("TLS certificates not found or invalid (%v). Generating 10-year self-signed certificate...", err)
		certs, err := generateSelfSignedCert(domainEnv)
		if err != nil {
			log.Fatalf("Failed to generate self-signed cert: %v", err)
		}
		certLoader = &CertificateLoader{cert: &certs, certFile: *certFile, keyFile: *keyFile}
	} else {
		log.Printf("TLS certificates loaded successfully from %s", *certFile)
	}

	tlsConfig := &tls.Config{
		GetCertificate: certLoader.GetCertificate,
		// QUIC listener should advertise only HTTP/3 ALPN.
		// Mixing legacy h3 drafts or HTTP/1.1 here can cause capability negotiation ambiguity.
		NextProtos: []string{http3.NextProtoH3},
		MinVersion:     tls.VersionTLS13,                    // Enforce TLS 1.3 for security
	}

	var tracer func(context.Context, bool, quic.ConnectionID) qlogwriter.Trace
	if os.Getenv("QLOG") == "1" {
		log.Println("Config: QLOG tracing enabled")
		tracer = func(ctx context.Context, isClient bool, connID quic.ConnectionID) qlogwriter.Trace {
			perspective := "server"
			if isClient {
				perspective = "client"
			}
			filename := fmt.Sprintf("%s_%x.qlog", perspective, connID)
			f, err := os.Create(filename)
			if err != nil {
				log.Printf("Failed to create qlog file: %v", err)
				return nil
			}
			log.Printf("Writing qlog to %s", filename)
			fileSeq := qlogwriter.NewConnectionFileSeq(
				NewBufferedWriteCloser(bufio.NewWriter(f), f),
				isClient,
				connID,
				[]string{qlog.EventSchema},
			)
			go fileSeq.Run()
			return fileSeq
		}
	}

	// V5.2: Profile defaults + optional explicit QUIC window overrides.
	profile := os.Getenv("WINDOW_PROFILE")
	windowCfg, err := core.ResolveQUICWindowConfig(profile)
	if err != nil {
		log.Fatalf("Invalid QUIC window config: %v", err)
	}
	if windowCfg.OverrideApplied {
		log.Printf(
			"V5.2 Config: WINDOW_PROFILE=%s + manual QUIC windows (init_stream=%d init_conn=%d max_stream=%d max_conn=%d)",
			windowCfg.Profile,
			windowCfg.InitialStreamReceiveWindow,
			windowCfg.InitialConnectionReceiveWindow,
			windowCfg.MaxStreamReceiveWindow,
			windowCfg.MaxConnectionReceiveWindow,
		)
	} else {
		log.Printf(
			"V5.2 Config: WINDOW_PROFILE=%s (init_stream=%d init_conn=%d max_stream=%d max_conn=%d)",
			windowCfg.Profile,
			windowCfg.InitialStreamReceiveWindow,
			windowCfg.InitialConnectionReceiveWindow,
			windowCfg.MaxStreamReceiveWindow,
			windowCfg.MaxConnectionReceiveWindow,
		)
	}

	quicConfig := &quic.Config{
		EnableDatagrams:                true,
		EnableStreamResetPartialDelivery: true,
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                10 * time.Second,
		Allow0RTT:                      true,
		MaxIncomingStreams:             1000,
		InitialStreamReceiveWindow:     windowCfg.InitialStreamReceiveWindow,
		InitialConnectionReceiveWindow: windowCfg.InitialConnectionReceiveWindow,
		MaxStreamReceiveWindow:         windowCfg.MaxStreamReceiveWindow,
		MaxConnectionReceiveWindow:     windowCfg.MaxConnectionReceiveWindow,
		Tracer:                         tracer,
	}

	server := webtransport.Server{
		H3: &http3.Server{
			Addr:            *listenAddr,
			TLSConfig:       tlsConfig,
			QUICConfig:      quicConfig,
			EnableDatagrams: true,
		},
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	// Ensure HTTP/3 SETTINGS always advertise WebTransport capabilities.
	// This is required for clients that validate SETTINGS before sending CONNECT.
	webtransport.ConfigureHTTP3Server(server.H3)
	log.Printf("WebTransport capability: H3 datagrams enabled=%v, QUIC datagrams enabled=%v", server.H3.EnableDatagrams, quicConfig.EnableDatagrams)
	// V5: ReplayCache removed - using counter-based anti-replay

	http.HandleFunc(*secretPath, func(w http.ResponseWriter, r *http.Request) {
		// Log every attempt to the secret path
		log.Printf("[DEBUG] connection attempt from %s to %s (Method: %s)", r.RemoteAddr, r.URL.Path, r.Method)

		session, err := server.Upgrade(w, r)
		if err != nil {
			log.Printf("[DEBUG] WebTransport upgrade failed (likely non-WT request): %v", err)
			// Decoy: Return a standard API 401 for unauthorized/non-protocol probes
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code": 40101, "message": "Invalid authentication token", "status": "error"}`))
			return
		}

		state := session.SessionState().ConnectionState.TLS
		log.Printf("[INFO] WebTransport session upgraded for %s (ALPN: %s)", r.RemoteAddr, state.NegotiatedProtocol)
		// V5: Create NonceGenerator per session for counter-based nonce
		ng, err := core.NewNonceGenerator()
		if err != nil {
			log.Printf("[ERROR] Failed to create NonceGenerator: %v", err)
			return
		}
		handleSession(session, *psk, ng)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If decoyRoot is specified and index.html exists, serve static files
		if *decoyRoot != "" {
			index := fmt.Sprintf("%s/index.html", strings.TrimSuffix(*decoyRoot, "/"))
			if _, err := os.Stat(index); err == nil {
				http.FileServer(http.Dir(*decoyRoot)).ServeHTTP(w, r)
				return
			}
		}

		// Fallback: Nginx 403 Forbidden Simulation
		// CRITICAL: Align Status Code and Headers to prevent fingerprinting
		w.Header().Set("Server", "nginx/1.18.0 (Ubuntu)")
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`<html>
<head><title>403 Forbidden</title></head>
<body bgcolor="white">
<center><h1>403 Forbidden</h1></center>
<hr><center>nginx/1.18.0 (Ubuntu)</center>
</body>
</html>`))
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Health check must return 200 OK for load balancers
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 1. Start HTTP/3 (UDP) Server for WebTransport
	go func() {
		log.Printf("Starting HTTP/3 (UDP) server on %s", *listenAddr)

		udpAddr, err := net.ResolveUDPAddr("udp", *listenAddr)
		if err != nil {
			log.Fatalf("Failed to resolve UDP addr: %v", err)
		}

		conn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			log.Fatalf("Failed to listen UDP: %v", err)
		}

		// V5.1 Performance Fix: Increase UDP buffers to 32MB to absorb ISP bursts
		// This prevents kernel-level packet drops during token bucket refills
		const bufSize = 32 * 1024 * 1024 // 32MB
		if err := conn.SetReadBuffer(bufSize); err != nil {
			log.Printf("Warning: Failed to set UDP read buffer: %v", err)
		}
		if err := conn.SetWriteBuffer(bufSize); err != nil {
			log.Printf("Warning: Failed to set UDP write buffer: %v", err)
		}
		log.Printf("UDP Send/Recv buffers set to %d bytes", bufSize)

		if err := server.Serve(conn); err != nil {
			log.Fatalf("HTTP/3 server failed: %v", err)
		}
	}()

	// 2. Start HTTP/1.1 (TCP) Server for Health Checks & Alt-Svc
	// This is CRITICAL for PaaS health checks which use TCP
	httpServer := &http.Server{
		Addr: *listenAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add Alt-Svc header to advertise HTTP/3 capability
			// This tells clients "I speak H3 on this same port"
			port := "443"
			if _, p, err := net.SplitHostPort(*listenAddr); err == nil {
				port = p
			}
			w.Header().Set("Alt-Svc", fmt.Sprintf(`h3=":%s"; ma=2592000`, port))

			// Delegate to default mux (handles /health, /, /v1/api/sync)
			http.DefaultServeMux.ServeHTTP(w, r)
		}),
	}

	// Create a TCP listener explicitly to get the actual bound port
	tcpListener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen on TCP %s: %v", *listenAddr, err)
	}
	log.Printf("HTTP/1.1 (TCP+TLS) server listening on %s", tcpListener.Addr().String())

	// Clone TLS config for TCP, setting correct ALPN for HTTP/1.1 and HTTP/2
	tcpTLSConfig := tlsConfig.Clone()
	tcpTLSConfig.NextProtos = []string{"h2", "http/1.1"}

	// Enable TLS on TCP listener using the tcp specific config
	tlsListener := tls.NewListener(tcpListener, tcpTLSConfig)

	if err := httpServer.Serve(tlsListener); err != nil {
		log.Fatalf("TCP server failed: %v", err)
	}
}

// handleSession processes incoming streams for a WebTransport session.
// V5: Uses NonceGenerator for counter-based nonce instead of ReplayCache.
func handleSession(session *webtransport.Session, psk string, ng *core.NonceGenerator) {
	log.Println("New session established")
	var streamID uint64

	for {
		stream, err := session.AcceptStream(context.Background())
		if err != nil {
			log.Printf("AcceptStream failed: %v", err)
			break
		}

		streamID++
		go handleStream(stream, psk, streamID, ng)
	}
}

// handleStream processes a single bidirectional stream.
// V5: Uses counter-based anti-replay with per-stream lastCounter tracking.
func handleStream(stream *webtransport.Stream, psk string, streamID uint64, ng *core.NonceGenerator) {
	defer stream.Close()

	reader := core.NewRecordReader(stream)
	var lastCounter uint64 = 0 // V5: Per-stream counter tracking

	// Read Metadata
	readTimeout := jitterDuration(4*time.Second, 6*time.Second)
	if err := stream.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		log.Printf("[SECURITY] [Stream %d] Failed to set metadata read deadline: %v", streamID, err)
		return
	}
	record, err := reader.ReadNextRecord()
	_ = stream.SetReadDeadline(time.Time{})
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			handleHandshakeFailure(stream, streamID, "Metadata read timed out")
			return
		}
		handleHandshakeFailure(stream, streamID, fmt.Sprintf("Failed to read metadata record: %v", err))
		return
	}

	if record.Type == core.TypePing {
		// V5: BuildPongRecord requires NonceGenerator
		pongRecord, err := core.BuildPongRecord(ng)
		if err != nil {
			return
		}
		_, _ = stream.Write(pongRecord)
		return
	}

	if record.Type != core.TypeMetadata {
		handleHandshakeFailure(stream, streamID, fmt.Sprintf("Invalid record type: %d", record.Type))
		return
	}

	if !core.IsTimestampValid(record.TimestampNano, time.Now(), core.DefaultReplayWindow) {
		handleHandshakeFailure(stream, streamID, "Timestamp outside allowed window")
		return
	}

	// V5: Counter-based anti-replay (first record counter must be 0 or strictly increasing)
	if record.Counter != 0 && record.Counter <= lastCounter {
		handleHandshakeFailure(stream, streamID, "Counter not strictly increasing")
		return
	}
	lastCounter = record.Counter

	meta, err := core.DecryptMetadata(record, psk)
	if err != nil {
		handleHandshakeFailure(stream, streamID, fmt.Sprintf("Decrypt failed: %v", err))
		return
	}

	targetAddr := fmt.Sprintf("%s:%d", meta.Host, meta.Port)
	log.Printf("[Stream %d] Connecting to %s", streamID, targetAddr)

	conn, err := net.DialTimeout("tcp", targetAddr, 10*time.Second)
	if err != nil {
		log.Printf("[Stream %d] Connect failed: %v", streamID, err)
		// V5: writeError now requires NonceGenerator
		writeError(stream, 0x0004, "connect failed", ng)
		return
	}
	defer conn.Close()

	// Bidirectional pipe
	errCh := make(chan error, 2)

	// WebTransport -> TCP
	go func() {
		buf := make([]byte, 512*1024)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				writeStart := time.Now()
				if _, wErr := conn.Write(buf[:n]); wErr != nil {
					errCh <- wErr
					return
				}
				gwPerf.observeWTToTCP(n, time.Since(writeStart))
			}
			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					errCh <- nil
				}
				return
			}
		}
	}()

	// TCP -> WebTransport
	go func() {
		buf := make([]byte, 512*1024)
		maxPayload := core.GetMaxRecordPayload()
		coalesceWait := 5 * time.Millisecond
		if v := os.Getenv("TCP_TO_WT_COALESCE_MS"); v != "" {
			if ms, pErr := strconv.Atoi(v); pErr == nil && ms >= 0 && ms <= 200 {
				coalesceWait = time.Duration(ms) * time.Millisecond
			}
		}
		// Flush threshold controls when we stop waiting for more small TCP reads.
		flushThreshold := maxPayload
		if v := os.Getenv("TCP_TO_WT_FLUSH_THRESHOLD"); v != "" {
			if parsed, pErr := strconv.Atoi(v); pErr == nil && parsed >= 1024 && parsed <= core.MaxRecordSize-core.RecordHeaderLength {
				flushThreshold = parsed
			}
		}
		adaptiveEnabled := true
		if v := os.Getenv("TCP_TO_WT_ADAPTIVE"); v != "" {
			adaptiveEnabled = v == "1" || strings.EqualFold(v, "true")
		}
		const (
			minCoalesceWait = 2 * time.Millisecond
			maxCoalesceWait = 40 * time.Millisecond
		)
		minFlushThreshold := 4096
		maxFlushThreshold := maxPayload * 2
		if maxFlushThreshold > core.MaxRecordSize-core.RecordHeaderLength {
			maxFlushThreshold = core.MaxRecordSize - core.RecordHeaderLength
		}
		adjustAdaptive := func(writeDur time.Duration, chunkSize int) {
			if !adaptiveEnabled {
				return
			}
			writeUs := float64(writeDur.Nanoseconds()) / 1000.0
			switch {
			case writeUs > 12000:
				if coalesceWait < maxCoalesceWait {
					coalesceWait += 2 * time.Millisecond
					if coalesceWait > maxCoalesceWait {
						coalesceWait = maxCoalesceWait
					}
				}
				if flushThreshold < maxFlushThreshold {
					flushThreshold += 1024
					if flushThreshold > maxFlushThreshold {
						flushThreshold = maxFlushThreshold
					}
				}
			case writeUs < 3000:
				// If writes are fast but chunks are tiny, aggregate a bit more.
				if chunkSize < maxPayload/2 {
					if coalesceWait < maxCoalesceWait {
						coalesceWait += 1 * time.Millisecond
						if coalesceWait > maxCoalesceWait {
							coalesceWait = maxCoalesceWait
						}
					}
				} else {
					if coalesceWait > minCoalesceWait {
						coalesceWait -= 1 * time.Millisecond
						if coalesceWait < minCoalesceWait {
							coalesceWait = minCoalesceWait
						}
					}
				}
				if flushThreshold > minFlushThreshold && chunkSize >= flushThreshold/2 {
					flushThreshold -= 512
					if flushThreshold < minFlushThreshold {
						flushThreshold = minFlushThreshold
					}
				}
			}
		}
		pending := make([]byte, 0, maxPayload*2)

		flushPending := func() error {
			for len(pending) > 0 {
				chunkSize := len(pending)
				if chunkSize > maxPayload {
					chunkSize = maxPayload
				}
				chunk := pending[:chunkSize]
				gwPerf.observeTCPFlush(chunkSize)
				buildStart := time.Now()
				recordBytes, buildErr := core.BuildDataRecord(chunk, meta.Options.MaxPadding, ng)
				if buildErr != nil {
					return buildErr
				}
				gwPerf.observeTCPBuild(time.Since(buildStart))
				writeStart := time.Now()
				if _, wErr := stream.Write(recordBytes); wErr != nil {
					core.PutBuffer(recordBytes)
					return wErr
				}
				writeDur := time.Since(writeStart)
				gwPerf.observeTCPToWT(len(recordBytes), writeDur)
				adjustAdaptive(writeDur, chunkSize)
				core.PutBuffer(recordBytes)
				pending = pending[chunkSize:]
			}
			pending = pending[:0]
			return nil
		}

		for {
			if len(pending) > 0 {
				_ = conn.SetReadDeadline(time.Now().Add(coalesceWait))
			} else {
				_ = conn.SetReadDeadline(time.Time{})
			}
			readStart := time.Now()
			n, err := conn.Read(buf)
			gwPerf.observeTCPReadWait(time.Since(readStart))
			if n > 0 {
				pending = append(pending, buf[:n]...)
				if len(pending) >= flushThreshold {
					if fErr := flushPending(); fErr != nil {
						errCh <- fErr
						return
					}
				}
			}
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					if len(pending) > 0 {
						if fErr := flushPending(); fErr != nil {
							errCh <- fErr
							return
						}
					}
					continue
				}
				if len(pending) > 0 {
					if fErr := flushPending(); fErr != nil {
						errCh <- fErr
						return
					}
				}
				if err != io.EOF {
					// Ignore "use of closed network connection" if caused by other side closing
					if !strings.Contains(err.Error(), "closed network connection") {
						errCh <- err
					} else {
						errCh <- nil
					}
				} else {
					errCh <- nil
				}
				return
			}
		}
	}()

	select {
	case err := <-errCh:
		if err != nil {
			log.Printf("[Stream %d] Stream error: %v", streamID, err)
		}
	}
	// Cleanup happens via defer stream.Close() and defer conn.Close()
}

// V5: writeError now requires NonceGenerator
func writeError(w io.Writer, code uint16, msg string, ng *core.NonceGenerator) {
	record, _ := core.BuildErrorRecord(code, msg, ng)
	w.Write(record)
}

func handleHandshakeFailure(stream *webtransport.Stream, streamID uint64, reason string) {
	log.Printf("[SECURITY] [Stream %d] %s", streamID, reason)
	time.Sleep(jitterDuration(100*time.Millisecond, 1000*time.Millisecond))
	decoyLen, err := randomIntRange(32, 128)
	if err != nil {
		decoyLen = 64
	}
	decoy := make([]byte, decoyLen)
	if _, err := rand.Read(decoy); err == nil {
		_, _ = stream.Write(decoy)
	}
}

func randomIntRange(min, max int) (int, error) {
	if min < 0 || max < min {
		return 0, fmt.Errorf("invalid range: %d-%d", min, max)
	}
	if min == max {
		return min, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return min + int(n.Int64()), nil
}

func jitterDuration(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	diff := max - min
	return min + time.Duration(mathrand.Int63n(int64(diff)+1))
}

func generateSelfSignedCert(domain string) (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	subject := pkix.Name{
		Organization: []string{"Aether Edge Relay Self-Signed"},
	}
	if domain != "" {
		subject.CommonName = domain
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      subject,
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour * 24 * 365 * 10), // V5: 10 Years Validity

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{domain},
	}
	if domain == "" {
		template.DNSNames = []string{"localhost"}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certBuf := &bytes.Buffer{}
	pem.Encode(certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyBuf := &bytes.Buffer{}
	pem.Encode(keyBuf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certBuf.Bytes(), keyBuf.Bytes())
}

// CertificateLoader handles dynamic reloading of TLS certificates via signal
type CertificateLoader struct {
	certFile string
	keyFile  string
	cert     *tls.Certificate
	mu       sync.RWMutex
}

func NewCertificateLoader(certFile, keyFile string) (*CertificateLoader, error) {
	loader := &CertificateLoader{
		certFile: certFile,
		keyFile:  keyFile,
	}
	// Initial load
	if err := loader.forceReload(); err != nil {
		return nil, err
	}

	// Start signal listener
	go loader.listenForSignal()

	return loader, nil
}

func (l *CertificateLoader) listenForSignal() {
	// Listen for SIGHUP (standard reload signal)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for range c {
			log.Println("[INFO] Received SIGHUP, reloading TLS certificates...")
			if err := l.forceReload(); err != nil {
				log.Printf("[ERROR] Failed to reload certificate on signal: %v", err)
			}
		}
	}()
}

func (l *CertificateLoader) forceReload() error {
	kp, err := tls.LoadX509KeyPair(l.certFile, l.keyFile)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.cert = &kp
	l.mu.Unlock()
	log.Printf("[INFO] Reloaded TLS certificate from %s", l.certFile)
	return nil
}

// GetCertificate implements tls.Config.GetCertificate
func (l *CertificateLoader) GetCertificate(clientHello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cert, nil
}

