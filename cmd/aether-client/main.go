package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/armon/go-socks5"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	webtransport "github.com/quic-go/webtransport-go"
	"golang.org/x/crypto/hkdf"

	"aether-rea/internal/core"
)

const (
	protocolLabel      = "aether-realist-v5"
	protocolVersion    = 0x05
	recordHeaderLength = 30
	maxRecordSize      = 1 * 1024 * 1024
	typeMetadata       = 0x01
	typeData           = 0x02
	typeError          = 0x7f
	timestampWindow    = 30 * time.Second
	metadataPaddingMin = 16
	metadataPaddingMax = 256
	dataPaddingMin     = 1
	dataPaddingMax     = 32
	// V5: Maximum counter value before rekey
	maxCounterValue    = 1 << 32
)

const (
	headerVersionOffset     = 0
	headerTypeOffset        = 1
	headerTimestampOffset   = 2
	headerTimestampSize     = 8
	headerPayloadLenOffset  = 10
	headerPaddingLenOffset  = 14
	// V5: SessionID + Counter instead of IV
	headerSessionIDOffset   = 18
	headerSessionIDLength   = 4
	headerCounterOffset     = 22
	headerCounterLength     = 8
	nonceLength             = 12 // SessionID(4) + Counter(8)
)

type clientOptions struct {
	serverURL   string
	psk         string
	listenAddr  string
	dialAddr    string
	rotateAfter time.Duration
	maxPadding  uint16
	autoIP      bool
	skipVerify  bool
	windowProfile string
}

func main() {
	var opts clientOptions
	flag.StringVar(&opts.serverURL, "url", "https://example.com/aether", "WebTransport endpoint URL")
	flag.StringVar(&opts.psk, "psk", "", "pre-shared key for metadata encryption")
	flag.StringVar(&opts.listenAddr, "listen", "127.0.0.1:1080", "local SOCKS5 listen address")
	flag.StringVar(&opts.dialAddr, "dial-addr", "", "override dial address for QUIC (e.g. 203.0.113.10:443)")
	flag.DurationVar(&opts.rotateAfter, "rotate", 20*time.Minute, "session rotation interval")
	var maxPadding uint
	flag.UintVar(&maxPadding, "max-padding", 128, "maximum random padding per record")
	flag.BoolVar(&opts.autoIP, "auto-ip", false, "auto select optimized IP from https://ip.v2too.top/")
	flag.BoolVar(&opts.skipVerify, "skip-verify", false, "skip TLS certificate verification (INSECURE)")
	flag.StringVar(&opts.windowProfile, "window-profile", "normal", "transmission profile (conservative, normal, aggressive)")
	flag.Parse()
	opts.maxPadding = uint16(maxPadding)

	if opts.psk == "" {
		fmt.Fprintln(os.Stderr, "Error: missing --psk")
		fmt.Fprintln(os.Stderr, "\nUsage: aether-client.exe --psk <pre-shared-key> [options]")
		fmt.Fprintln(os.Stderr, "\nOptions:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nPress Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	if opts.dialAddr == "" && opts.autoIP {
		ip, err := selectOptimizedIP()
		if err != nil {
			log.Printf("auto-ip failed: %v", err)
		} else {
			opts.dialAddr = fmt.Sprintf("%s:443", ip)
			log.Printf("auto-ip selected %s", opts.dialAddr)
		}
	}

	manager, err := newSessionManager(opts)
	if err != nil {
		log.Fatalf("session manager init failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.startRotation(ctx)

	socksConf := &socks5.Config{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, portStr, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			port, err := parsePort(portStr)
			if err != nil {
				return nil, err
			}
			return manager.openStream(ctx, host, port)
		},
	}

	server, err := socks5.New(socksConf)
	if err != nil {
		log.Fatalf("socks5 init failed: %v", err)
	}

	log.Printf("Aether client listening on %s", opts.listenAddr)
	if err := server.ListenAndServe("tcp", opts.listenAddr); err != nil {
		log.Fatalf("socks5 server stopped: %v", err)
	}
}

type sessionManager struct {
	opts        clientOptions
	url         *url.URL
	mu          sync.Mutex
	session     *webtransport.Session
	counter     uint64
	dialer      *webtransport.Dialer
	closeSignal chan struct{}
	nonceGen    *nonceGenerator // V5: Counter-based nonce generator
}

// nonceGenerator generates unique nonces using SessionID + monotonic counter.
type nonceGenerator struct {
	sessionID [4]byte
	counter   uint64
	mu        sync.Mutex
}

var errCounterExhausted = errors.New("counter exhausted, session rekey required")

// newNonceGenerator creates a new NonceGenerator with random SessionID.
func newNonceGenerator() (*nonceGenerator, error) {
	ng := &nonceGenerator{}
	if _, err := rand.Read(ng.sessionID[:]); err != nil {
		return nil, err
	}
	return ng, nil
}

// next returns the next nonce (12 bytes) and counter value.
func (ng *nonceGenerator) next() ([12]byte, uint64, error) {
	ng.mu.Lock()
	defer ng.mu.Unlock()

	if ng.counter >= maxCounterValue {
		return [12]byte{}, 0, errCounterExhausted
	}

	ng.counter++
	var nonce [12]byte
	copy(nonce[0:4], ng.sessionID[:])
	binary.BigEndian.PutUint64(nonce[4:12], ng.counter)
	return nonce, ng.counter, nil
}

// getSessionID returns the session ID.
func (ng *nonceGenerator) getSessionID() [4]byte {
	return ng.sessionID
}

func newSessionManager(opts clientOptions) (*sessionManager, error) {
	parsed, err := url.Parse(opts.serverURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" {
		return nil, fmt.Errorf("url must be https")
	}

	// V5.2: Apply window profile
	windowCfg, err := core.ResolveQUICWindowConfig(opts.windowProfile)
	if err != nil {
		log.Printf("Warning: failed to resolve window profile: %v", err)
	}

	quicConfig := &quic.Config{
		KeepAlivePeriod:                20 * time.Second,
		MaxIdleTimeout:                 60 * time.Second,
		EnableDatagrams:                true,
		EnableStreamResetPartialDelivery: true,
		InitialStreamReceiveWindow:     windowCfg.InitialStreamReceiveWindow,
		InitialConnectionReceiveWindow: windowCfg.InitialConnectionReceiveWindow,
		MaxStreamReceiveWindow:         windowCfg.MaxStreamReceiveWindow,
		MaxConnectionReceiveWindow:     windowCfg.MaxConnectionReceiveWindow,
	}

	dialer := &webtransport.Dialer{
		TLSClientConfig: (&tlsConfig{
			serverName: parsed.Hostname(),
			skipVerify: opts.skipVerify,
		}).toTLSConfig(),
		QUICConfig: quicConfig,
	}

	return &sessionManager{
		opts:        opts,
		url:         parsed,
		dialer:      dialer,
		closeSignal: make(chan struct{}),
	}, nil
}

func (m *sessionManager) startRotation(ctx context.Context) {
	if m.opts.rotateAfter <= 0 {
		return
	}
	ticker := time.NewTicker(m.opts.rotateAfter)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.resetSession()
			case <-ctx.Done():
				m.resetSession()
				return
			}
		}
	}()
}

func (m *sessionManager) resetSession() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		_ = m.session.CloseWithError(0, "rotation")
	}
	m.session = nil
	m.counter = 0
	m.nonceGen = nil // V5: Reset nonce generator
}

func (m *sessionManager) openStream(ctx context.Context, host string, port uint16) (net.Conn, error) {
	session, ng, err := m.getSession(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := session.OpenStreamSync(ctx)
	if err != nil {
		m.resetSession()
		return nil, err
	}

	// V5: Pass NonceGenerator to buildMetadataRecord
	metadata, err := buildMetadataRecord(host, port, m.opts.maxPadding, m.opts.psk, ng)
	if err != nil {
		return nil, err
	}
	if _, err := stream.Write(metadata); err != nil {
		return nil, err
	}

	// V5: Pass NonceGenerator to webTransportConn for data records
	return newWebTransportConn(stream, m.opts, ng), nil
}

func (m *sessionManager) getSession(ctx context.Context) (*webtransport.Session, *nonceGenerator, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.session == nil {
		session, err := m.dialSession(ctx)
		if err != nil {
			return nil, nil, err
		}
		m.session = session
		m.counter = 0
		// V5: Initialize NonceGenerator for counter-based nonce
		m.nonceGen, err = newNonceGenerator()
		if err != nil {
			_ = session.CloseWithError(0, "nonce generator failed")
			return nil, nil, err
		}
	}
	m.counter += 1
	return m.session, m.nonceGen, nil
}

func (m *sessionManager) dialSession(ctx context.Context) (*webtransport.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Determine the URL to dial
	dialURL := m.url.String()

	// If dialAddr is specified, construct URL with override address
	if m.opts.dialAddr != "" {
		// Parse dialAddr to ensure it has port
		host, port, err := net.SplitHostPort(m.opts.dialAddr)
		if err != nil {
			// If no port specified, add default 443
			if strings.Contains(err.Error(), "missing port in address") {
				m.opts.dialAddr = net.JoinHostPort(m.opts.dialAddr, "443")
			}
			host, port, _ = net.SplitHostPort(m.opts.dialAddr)
		}

		// Construct new URL with override host:port
		parsedCopy := *m.url
		parsedCopy.Host = net.JoinHostPort(host, port)
		dialURL = parsedCopy.String()
	}

	_, sess, err := m.dialer.Dial(ctx, dialURL, nil)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

type webTransportConn struct {
	stream     *webtransport.Stream
	reader     *recordReader
	options    clientOptions
	localAddr  net.Addr
	remoteAddr net.Addr
	nonceGen   *nonceGenerator // V5: Counter-based nonce generator
}

func newWebTransportConn(stream *webtransport.Stream, opts clientOptions, ng *nonceGenerator) *webTransportConn {
	return &webTransportConn{
		stream:     stream,
		reader:     newRecordReader(stream),
		options:    opts,
		localAddr:  dummyAddr("aether-local"),
		remoteAddr: dummyAddr("aether-remote"),
		nonceGen:   ng,
	}
}

func (c *webTransportConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *webTransportConn) Write(p []byte) (int, error) {
	// V5: Pass NonceGenerator to buildDataRecord
	record, err := buildDataRecord(p, c.options.maxPadding, c.nonceGen)
	if err != nil {
		return 0, err
	}
	if _, err := c.stream.Write(record); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *webTransportConn) Close() error {
	return c.stream.Close()
}

func (c *webTransportConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *webTransportConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *webTransportConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *webTransportConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *webTransportConn) SetWriteDeadline(t time.Time) error {
	return nil
}

type recordReader struct {
	reader io.Reader
	stash  []byte
}

func newRecordReader(reader io.Reader) *recordReader {
	return &recordReader{reader: reader}
}

func (r *recordReader) Read(p []byte) (int, error) {
	for len(r.stash) == 0 {
		record, err := readRecord(r.reader)
		if err != nil {
			return 0, err
		}
		if record.recordType == typeError {
			return 0, fmt.Errorf("server error: %s", record.errorMessage)
		}
		if record.recordType != typeData {
			continue
		}
		r.stash = record.payload
	}

	n := copy(p, r.stash)
	r.stash = r.stash[n:]
	return n, nil
}

type record struct {
	recordType   byte
	payload      []byte
	errorMessage string
}

func readRecord(reader io.Reader) (*record, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(reader, lengthBytes); err != nil {
		return nil, err
	}
	totalLength := binary.BigEndian.Uint32(lengthBytes)
	if totalLength < recordHeaderLength {
		return nil, errors.New("invalid record length")
	}
	if totalLength > maxRecordSize {
		return nil, errors.New("record length exceeds max")
	}

	recordBytes := make([]byte, totalLength)
	if _, err := io.ReadFull(reader, recordBytes); err != nil {
		return nil, err
	}

	version := recordBytes[headerVersionOffset]
	if version != protocolVersion {
		return nil, errors.New("unsupported protocol version")
	}

	recordType := recordBytes[headerTypeOffset]
	timestamp := binary.BigEndian.Uint64(recordBytes[headerTimestampOffset : headerTimestampOffset+headerTimestampSize])
	payloadLength := binary.BigEndian.Uint32(recordBytes[headerPayloadLenOffset : headerPayloadLenOffset+4])
	paddingLength := binary.BigEndian.Uint32(recordBytes[headerPaddingLenOffset : headerPaddingLenOffset+4])

	if !isTimestampValid(timestamp, time.Now(), timestampWindow) {
		return nil, errors.New("timestamp outside allowed window")
	}

	if int(recordHeaderLength+payloadLength+paddingLength) != len(recordBytes) {
		return nil, errors.New("invalid payload length")
	}

	payloadStart := recordHeaderLength
	payloadEnd := payloadStart + int(payloadLength)
	payload := recordBytes[payloadStart:payloadEnd]

	result := &record{recordType: recordType, payload: payload}
	if recordType == typeError {
		if len(payload) >= 4 {
			result.errorMessage = string(payload[4:])
		}
	}
	return result, nil
}

// V5: buildMetadataRecord uses NonceGenerator for counter-based nonce
func buildMetadataRecord(host string, port uint16, maxPadding uint16, psk string, ng *nonceGenerator) ([]byte, error) {
	plaintext, err := buildMetadataPayload(host, port, maxPadding)
	if err != nil {
		return nil, err
	}

	// V5: Get nonce from generator
	nonce, counter, err := ng.next()
	if err != nil {
		return nil, err
	}
	sessionID := ng.getSessionID()

	// V5: Use SessionID as HKDF salt
	key, err := deriveKey(psk, sessionID[:])
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertextLen := len(plaintext) + gcm.Overhead()
	paddingLen, err := randomPaddingRange(metadataPaddingMin, metadataPaddingMax)
	if err != nil {
		return nil, err
	}
	padding := make([]byte, paddingLen)
	if _, err := rand.Read(padding); err != nil {
		return nil, err
	}
	// V5: buildHeader takes SessionID and Counter
	header, err := buildHeader(typeMetadata, uint32(ciphertextLen), uint32(paddingLen), sessionID[:], counter)
	if err != nil {
		return nil, err
	}

	// V5: Use nonce (SessionID || Counter) for encryption
	ciphertext := gcm.Seal(nil, nonce[:], plaintext, header)

	return buildRecord(header, ciphertext, padding), nil
}

// V5: buildDataRecord uses NonceGenerator for counter-based nonce
func buildDataRecord(payload []byte, maxPadding uint16, ng *nonceGenerator) ([]byte, error) {
	paddingLength := randomPadding(maxPadding)
	padding := make([]byte, paddingLength)
	if paddingLength > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}

	// V5: Get nonce from generator
	_, counter, err := ng.next()
	if err != nil {
		return nil, err
	}
	sessionID := ng.getSessionID()

	// V5: buildHeader takes SessionID and Counter
	header, err := buildHeader(typeData, uint32(len(payload)), uint32(len(padding)), sessionID[:], counter)
	if err != nil {
		return nil, err
	}

	return buildRecord(header, payload, padding), nil
}

func buildRecord(header, payload, padding []byte) []byte {
	totalLength := recordHeaderLength + len(payload) + len(padding)
	record := make([]byte, 4+totalLength)
	binary.BigEndian.PutUint32(record[0:4], uint32(totalLength))
	copy(record[4:4+recordHeaderLength], header)
	copy(record[4+recordHeaderLength:], payload)
	copy(record[4+recordHeaderLength+len(payload):], padding)
	return record
}

// V5: buildHeader uses SessionID + Counter instead of IV
func buildHeader(recordType byte, payloadLength uint32, paddingLength uint32, sessionID []byte, counter uint64) ([]byte, error) {
	if len(sessionID) != headerSessionIDLength {
		return nil, fmt.Errorf("invalid SessionID length: %d", len(sessionID))
	}
	header := make([]byte, recordHeaderLength)
	header[headerVersionOffset] = protocolVersion
	header[headerTypeOffset] = recordType
	binary.BigEndian.PutUint64(header[headerTimestampOffset:headerTimestampOffset+headerTimestampSize], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(header[headerPayloadLenOffset:headerPayloadLenOffset+4], payloadLength)
	binary.BigEndian.PutUint32(header[headerPaddingLenOffset:headerPaddingLenOffset+4], paddingLength)
	// V5: Copy SessionID and Counter instead of IV
	copy(header[headerSessionIDOffset:headerSessionIDOffset+headerSessionIDLength], sessionID)
	binary.BigEndian.PutUint64(header[headerCounterOffset:headerCounterOffset+headerCounterLength], counter)
	return header, nil
}

func buildMetadataPayload(host string, port uint16, maxPadding uint16) ([]byte, error) {
	var addrType byte
	var addrBytes []byte

	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			addrType = 0x01
			addrBytes = ip.To4()
		} else {
			addrType = 0x02
			addrBytes = ip.To16()
		}
	} else {
		addrType = 0x03
		if len(host) > 255 {
			return nil, errors.New("domain too long")
		}
		addrBytes = append([]byte{byte(len(host))}, []byte(host)...)
	}

	options := buildOptions(maxPadding)
	payload := make([]byte, 0, 1+2+len(addrBytes)+2+len(options))
	payload = append(payload, addrType)
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, port)
	payload = append(payload, portBytes...)
	payload = append(payload, addrBytes...)

	optionsLen := make([]byte, 2)
	binary.BigEndian.PutUint16(optionsLen, uint16(len(options)))
	payload = append(payload, optionsLen...)
	payload = append(payload, options...)
	return payload, nil
}

func buildOptions(maxPadding uint16) []byte {
	if maxPadding == 0 {
		return nil
	}
	option := make([]byte, 4)
	option[0] = 0x01
	option[1] = 0x02
	binary.BigEndian.PutUint16(option[2:4], maxPadding)
	return option
}

func deriveKey(psk string, salt []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, []byte(psk), salt, []byte(protocolLabel))
	key := make([]byte, 16)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func isTimestampValid(timestampNano uint64, now time.Time, window time.Duration) bool {
	if timestampNano == 0 {
		return false
	}
	ts := time.Unix(0, int64(timestampNano))
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	return delta <= window
}

func randomPadding(maxPadding uint16) int {
	maxAllowed := int(maxPadding)
	minCap := dataPaddingMax
	if maxAllowed > 0 && maxAllowed < minCap {
		minCap = maxAllowed
	}
	minLen, err := randomPaddingRange(dataPaddingMin, minCap)
	if err != nil {
		return 0
	}
	if maxAllowed == 0 || maxAllowed <= minLen {
		return minLen
	}
	extra, err := randomPaddingRange(0, maxAllowed-minLen)
	if err != nil {
		return minLen
	}
	return minLen + extra
}

func randomPaddingRange(min, max int) (int, error) {
	if min < 0 || max < min {
		return 0, fmt.Errorf("invalid padding range: %d-%d", min, max)
	}
	if min == max {
		return min, nil
	}
	maxInt := big.NewInt(int64(max - min + 1))
	n, err := rand.Int(rand.Reader, maxInt)
	if err != nil {
		return 0, err
	}
	return min + int(n.Int64()), nil
}

func parsePort(portStr string) (uint16, error) {
	port, err := net.LookupPort("tcp", portStr)
	if err == nil {
		return uint16(port), nil
	}
	var value uint64
	_, err = fmt.Sscanf(portStr, "%d", &value)
	if err != nil || value > 65535 {
		return 0, errors.New("invalid port")
	}
	return uint16(value), nil
}

func selectOptimizedIP() (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("https://ip.v2too.top/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	ips := strings.Fields(string(body))
	if len(ips) == 0 {
		return "", errors.New("empty ip list")
	}

	bestIP := ""
	bestLatency := 5 * time.Second
	for _, ip := range ips {
		latency, err := probeIP(ip)
		if err != nil {
			continue
		}
		if latency < bestLatency {
			bestLatency = latency
			bestIP = ip
		}
	}

	if bestIP == "" {
		return "", errors.New("no reachable ip")
	}
	return bestIP, nil
}

func probeIP(ip string) (time.Duration, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:443", ip), 800*time.Millisecond)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return time.Since(start), nil
}

type dummyAddr string

func (d dummyAddr) Network() string { return string(d) }
func (d dummyAddr) String() string  { return string(d) }

// tlsConfig wraps a minimal TLS config definition to avoid relying on global defaults.
type tlsConfig struct {
	serverName string
	skipVerify bool
}

func (t *tlsConfig) toTLSConfig() *tls.Config {
	return &tls.Config{
		ServerName:         t.serverName,
		InsecureSkipVerify: t.skipVerify,
		NextProtos:         []string{http3.NextProtoH3},
	}
}

