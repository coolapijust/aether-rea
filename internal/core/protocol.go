package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	ProtocolLabel      = "aether-realist-v5"
	ProtocolVersion    = 0x05
	RecordHeaderLength = 30
	TypeMetadata       = 0x01
	TypeData           = 0x02
	TypePing           = 0x03
	TypePong           = 0x04
	TypeError          = 0x7f
	MaxRecordSize      = 1 * 1024 * 1024
	MaxCounterValue    = uint64(1 << 32) // 2^32 rekey threshold
	// DefaultMaxRecordPayload is the default data record chunk size.
	DefaultMaxRecordPayload = 16 * 1024
)

var (
	// recordPayloadBytes stores current max data payload size per record.
	recordPayloadBytes atomic.Int64
	// poolBufferBytes stores current pooled buffer capacity.
	poolBufferBytes atomic.Int64
)

func init() {
	payload := DefaultMaxRecordPayload
	if v := os.Getenv("RECORD_PAYLOAD_BYTES"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			payload = parsed
		}
	}
	_ = SetRecordPayloadBytes(payload)
}

func clampRecordPayload(size int) int {
	if size < 1024 {
		size = 1024
	}
	maxPayload := MaxRecordSize - RecordHeaderLength
	if size > maxPayload {
		size = maxPayload
	}
	return size
}

// SetRecordPayloadBytes updates record payload and pool size atomically.
func SetRecordPayloadBytes(size int) int {
	normalized := clampRecordPayload(size)
	recordPayloadBytes.Store(int64(normalized))
	poolBufferBytes.Store(int64(normalized + RecordHeaderLength + 32))
	return normalized
}

// GetMaxRecordPayload returns current max data payload size.
func GetMaxRecordPayload() int {
	return int(recordPayloadBytes.Load())
}

// GetPoolBufferSize returns current pooled buffer capacity.
func GetPoolBufferSize() int {
	return int(poolBufferBytes.Load())
}

var recordPool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate buffer with enough capacity for a full record
		return make([]byte, GetPoolBufferSize())
	},
}

// GetBuffer returns a clean buffer from the pool.
func GetBuffer() []byte {
	buf := recordPool.Get().([]byte)
	return buf[:0] // Reset length while keeping capacity
}

// PutBuffer returns a buffer to the pool.
func PutBuffer(buf []byte) {
	if cap(buf) < GetPoolBufferSize() {
		return // Protection against resizing
	}
	recordPool.Put(buf[:0])
}

const (
	headerVersionOffset      = 0
	headerTypeOffset         = 1
	headerTimestampOffset    = 2
	headerTimestampSize      = 8
	headerPayloadLenOffset   = 10
	headerPaddingLenOffset   = 14
	headerSessionIDOffset    = 18
	headerSessionIDLength    = 4
	headerCounterOffset      = 22
	headerCounterLength      = 8
	nonceLength              = 12 // SessionID(4) + Counter(8)
)

// ErrCounterExhausted is returned when the counter reaches MaxCounterValue.
var ErrCounterExhausted = errors.New("counter exhausted, session rekey required")

// Metadata represents the connection target information
type Metadata struct {
	Host    string
	Port    uint16
	Options Options
}

// Options represents the connection options
type Options struct {
	MaxPadding uint16
}

// Record represents a parsed record
type Record struct {
	Version       byte
	Type          byte
	TimestampNano uint64
	PayloadLength uint32
	PaddingLength uint32
	Payload       []byte
	Header        []byte
	SessionID     []byte
	Counter       uint64
	ErrorMessage  string
	RawBuffer     []byte // Original pooled buffer for later release
}

// NonceGenerator generates unique nonces using SessionID + monotonic counter.
type NonceGenerator struct {
	sessionID [4]byte
	counter   atomic.Uint64
}

// NewNonceGenerator creates a new NonceGenerator with a random SessionID.
func NewNonceGenerator() (*NonceGenerator, error) {
	ng := &NonceGenerator{}
	if _, err := rand.Read(ng.sessionID[:]); err != nil {
		return nil, err
	}
	return ng, nil
}

// Next returns the next nonce (12 bytes) and the current counter value.
// Returns ErrCounterExhausted if the counter reaches MaxCounterValue.
func (ng *NonceGenerator) Next() ([12]byte, uint64, error) {
	for {
		current := ng.counter.Load()
		if current >= MaxCounterValue {
			return [12]byte{}, 0, ErrCounterExhausted
		}
		if !ng.counter.CompareAndSwap(current, current+1) {
			continue
		}

		var nonce [12]byte
		copy(nonce[0:4], ng.sessionID[:])
		binary.BigEndian.PutUint64(nonce[4:12], current)
		return nonce, current, nil
	}
}

// SessionID returns the session ID.
func (ng *NonceGenerator) SessionID() [4]byte {
	return ng.sessionID
}

// Counter returns the current counter value (for monitoring).
func (ng *NonceGenerator) Counter() uint64 {
	return ng.counter.Load()
}

const (
	metadataPaddingMin = 16
	metadataPaddingMax = 256
	dataPaddingMin     = 1
	dataPaddingMax     = 32
)

// BuildMetadataRecord creates an encrypted metadata record.
// V5: Requires NonceGenerator for counter-based nonce.
func BuildMetadataRecord(host string, port uint16, maxPadding uint16, psk string, ng *NonceGenerator) ([]byte, error) {
	plaintext, err := buildMetadataPayload(host, port, maxPadding)
	if err != nil {
		return nil, err
	}

	// V5: Get nonce from generator
	nonce, counter, err := ng.Next()
	if err != nil {
		return nil, err
	}
	sessionID := nonce[0:4]

	// V5: Use SessionID as HKDF salt
	key, err := deriveKey(psk, sessionID)
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

	// Use final ciphertext length for AAD consistency
	ciphertextLen := len(plaintext) + gcm.Overhead()
	paddingLen, err := randomPaddingRange(metadataPaddingMin, metadataPaddingMax)
	if err != nil {
		return nil, err
	}
	padding := make([]byte, paddingLen)
	if _, err := rand.Read(padding); err != nil {
		return nil, err
	}
	header, err := buildHeader(TypeMetadata, ciphertextLen, paddingLen, sessionID, counter)
	if err != nil {
		return nil, err
	}

	// V5: Nonce = SessionID || Counter
	ciphertext := gcm.Seal(nil, nonce[:], plaintext, header)

	return buildRecord(header, ciphertext, padding), nil
}

// BuildDataRecord creates a data record with optional padding using pooled buffers.
// V5.1: Automatically forces padding to 0 for TypeData to maximize throughput.
// V5: Requires NonceGenerator for counter-based nonce.
func BuildDataRecord(payload []byte, _ uint16, ng *NonceGenerator) ([]byte, error) {
	// V5.1 Optimization: Data records MUST NOT have padding.
	const paddingLength = 0
	
	// V5.1: Get nonce from generator
	nonce, counter, err := ng.Next()
	if err != nil {
		return nil, err
	}
	sessionID := nonce[0:4]

	totalLength := RecordHeaderLength + len(payload)
	// Use pool for data records which are the bulk of traffic
	buf := GetBuffer()
	
	// Ensure we have enough capacity (should always be true with 16KB limit)
	if cap(buf) < 4+totalLength {
		buf = make([]byte, 4+totalLength)
	} else {
		buf = buf[:4+totalLength]
	}

	binary.BigEndian.PutUint32(buf[0:4], uint32(totalLength))
	// Zero-alloc: build header directly into pool buffer
	if err := buildHeaderInto(buf[4:4+RecordHeaderLength], TypeData, len(payload), paddingLength, sessionID, counter); err != nil {
		PutBuffer(buf)
		return nil, err
	}
	copy(buf[4+RecordHeaderLength:], payload)
	
	return buf, nil
}

// BuildPingRecord creates a ping record.
// V5: Requires NonceGenerator for counter-based nonce.
func BuildPingRecord(ng *NonceGenerator) ([]byte, error) {
	return buildControlRecord(TypePing, ng)
}

// BuildPongRecord creates a pong record.
// V5: Requires NonceGenerator for counter-based nonce.
func BuildPongRecord(ng *NonceGenerator) ([]byte, error) {
	return buildControlRecord(TypePong, ng)
}

// buildRecord assembles a complete record.
func buildRecord(header, payload, padding []byte) []byte {
	totalLength := RecordHeaderLength + len(payload) + len(padding)
	record := make([]byte, 4+totalLength)
	binary.BigEndian.PutUint32(record[0:4], uint32(totalLength))
	copy(record[4:4+RecordHeaderLength], header)
	copy(record[4+RecordHeaderLength:], payload)
	copy(record[4+RecordHeaderLength+len(payload):], padding)
	return record
}

func buildHeader(recordType byte, payloadLen, paddingLen int, sessionID []byte, counter uint64) ([]byte, error) {
	if len(sessionID) != headerSessionIDLength {
		return nil, fmt.Errorf("invalid SessionID length: %d", len(sessionID))
	}

	header := make([]byte, RecordHeaderLength)
	header[headerVersionOffset] = ProtocolVersion
	header[headerTypeOffset] = recordType
	binary.BigEndian.PutUint64(header[headerTimestampOffset:headerTimestampOffset+headerTimestampSize], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(header[headerPayloadLenOffset:headerPayloadLenOffset+4], uint32(payloadLen))
	binary.BigEndian.PutUint32(header[headerPaddingLenOffset:headerPaddingLenOffset+4], uint32(paddingLen))
	copy(header[headerSessionIDOffset:headerSessionIDOffset+headerSessionIDLength], sessionID)
	binary.BigEndian.PutUint64(header[headerCounterOffset:headerCounterOffset+headerCounterLength], counter)
	return header, nil
}

// buildHeaderInto writes a record header directly into dst (must be >= RecordHeaderLength bytes).
// Zero-allocation alternative to buildHeader for hot paths.
func buildHeaderInto(dst []byte, recordType byte, payloadLen, paddingLen int, sessionID []byte, counter uint64) error {
	if len(dst) < RecordHeaderLength {
		return fmt.Errorf("dst too small for header: %d < %d", len(dst), RecordHeaderLength)
	}
	if len(sessionID) != headerSessionIDLength {
		return fmt.Errorf("invalid SessionID length: %d", len(sessionID))
	}
	dst[headerVersionOffset] = ProtocolVersion
	dst[headerTypeOffset] = recordType
	binary.BigEndian.PutUint64(dst[headerTimestampOffset:headerTimestampOffset+headerTimestampSize], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(dst[headerPayloadLenOffset:headerPayloadLenOffset+4], uint32(payloadLen))
	binary.BigEndian.PutUint32(dst[headerPaddingLenOffset:headerPaddingLenOffset+4], uint32(paddingLen))
	copy(dst[headerSessionIDOffset:headerSessionIDOffset+headerSessionIDLength], sessionID)
	binary.BigEndian.PutUint64(dst[headerCounterOffset:headerCounterOffset+headerCounterLength], counter)
	return nil
}

func buildControlRecord(recordType byte, ng *NonceGenerator) ([]byte, error) {
	nonce, counter, err := ng.Next()
	if err != nil {
		return nil, err
	}
	sessionID := nonce[0:4]
	header, err := buildHeader(recordType, 0, 0, sessionID, counter)
	if err != nil {
		return nil, err
	}
	return buildRecord(header, nil, nil), nil
}

// buildMetadataPayload creates the plaintext metadata.
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
			return nil, fmt.Errorf("domain too long")
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

// buildOptions creates the options TLV.
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

// deriveKey derives AES key from PSK using HKDF.
func deriveKey(psk string, salt []byte) ([]byte, error) {
	psk = strings.TrimSpace(psk)
	reader := hkdf.New(sha256.New, []byte(psk), salt, []byte(ProtocolLabel))
	key := make([]byte, 16)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// randomPadding generates random padding length.
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

// DecryptMetadata decrypts the metadata record
// V5: Uses SessionID as salt and SessionID||Counter as nonce
func DecryptMetadata(record *Record, psk string) (*Metadata, error) {
	if psk == "" {
		return nil, fmt.Errorf("missing psk")
	}

	// Double check header size
	if len(record.Header) != RecordHeaderLength {
		return nil, fmt.Errorf("invalid header length: %d", len(record.Header))
	}

	// V5: Reconstruct nonce from SessionID + Counter
	if len(record.SessionID) != headerSessionIDLength {
		return nil, fmt.Errorf("invalid SessionID length: %d", len(record.SessionID))
	}
	var nonce [12]byte
	copy(nonce[0:4], record.SessionID)
	binary.BigEndian.PutUint64(nonce[4:12], record.Counter)

	header := make([]byte, len(record.Header))
	copy(header, record.Header)

	// V5: Use SessionID as HKDF salt
	key, err := deriveKey(psk, record.SessionID)
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

	decryptStart := time.Now()
	plaintext, err := gcm.Open(nil, nonce[:], record.Payload, header)
	perfObserveDownDecrypt(time.Since(decryptStart))
	if err != nil {
		return nil, err
	}

	return ParseMetadata(plaintext)
}

// IsTimestampValid validates timestamp against a time window.
func IsTimestampValid(timestampNano uint64, now time.Time, window time.Duration) bool {
	if timestampNano == 0 {
		return false
	}
	timestamp := time.Unix(0, int64(timestampNano))
	delta := now.Sub(timestamp)
	if delta < 0 {
		delta = -delta
	}
	return delta <= window
}

// ... (ParseMetadata and parseOptions remain same) ...

// ParseMetadata parses the decrypted metadata payload
func ParseMetadata(buffer []byte) (*Metadata, error) {
	if len(buffer) < 3 {
		return nil, fmt.Errorf("metadata too short")
	}
	addressType := buffer[0]
	port := binary.BigEndian.Uint16(buffer[1:3])
	offset := 3

	var host string
	if addressType == 0x01 { // IPv4
		if len(buffer) < offset+4 {
			return nil, fmt.Errorf("invalid ipv4 length")
		}
		host = net.IP(buffer[offset : offset+4]).String()
		offset += 4
	} else if addressType == 0x02 { // IPv6
		if len(buffer) < offset+16 {
			return nil, fmt.Errorf("invalid ipv6 length")
		}
		host = net.IP(buffer[offset : offset+16]).String()
		offset += 16
	} else if addressType == 0x03 { // Domain
		if len(buffer) < offset+1 {
			return nil, fmt.Errorf("invalid domain length")
		}
		domainLen := int(buffer[offset])
		offset += 1
		if len(buffer) < offset+domainLen {
			return nil, fmt.Errorf("invalid domain content length")
		}
		host = string(buffer[offset : offset+domainLen])
		offset += domainLen
	} else {
		return nil, fmt.Errorf("unsupported address type: %d", addressType)
	}

	if len(buffer) < offset+2 {
		return nil, fmt.Errorf("missing options length")
	}
	optionsLength := binary.BigEndian.Uint16(buffer[offset : offset+2])
	offset += 2

	if len(buffer) < offset+int(optionsLength) {
		return nil, fmt.Errorf("missing options payload")
	}
	optionsPayload := buffer[offset : offset+int(optionsLength)]

	options := parseOptions(optionsPayload)

	return &Metadata{
		Host:    host,
		Port:    port,
		Options: options,
	}, nil
}

func parseOptions(buffer []byte) Options {
	opts := Options{}
	offset := 0
	for offset+2 <= len(buffer) {
		typ := buffer[offset]
		length := int(buffer[offset+1])
		offset += 2
		if offset+length > len(buffer) {
			break
		}
		value := buffer[offset : offset+length]
		offset += length

		if typ == 0x01 && len(value) == 2 {
			opts.MaxPadding = binary.BigEndian.Uint16(value)
		}
	}
	return opts
}

// BuildErrorRecord creates an error record
// V5: Requires NonceGenerator for counter-based nonce.
func BuildErrorRecord(code uint16, message string, ng *NonceGenerator) ([]byte, error) {
	messageBytes := []byte(message)
	payload := make([]byte, 4+len(messageBytes))
	binary.BigEndian.PutUint16(payload[0:2], code)
	copy(payload[4:], messageBytes)

	// V5: Get nonce from generator
	nonce, counter, err := ng.Next()
	if err != nil {
		return nil, err
	}
	sessionID := nonce[0:4]

	header, err := buildHeader(TypeError, len(payload), 0, sessionID, counter)
	if err != nil {
		return nil, err
	}

	return buildRecord(header, payload, nil), nil
}

