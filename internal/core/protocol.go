package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	ProtocolLabel      = "aether-realist-v4"
	ProtocolVersion    = 0x04
	RecordHeaderLength = 30
	TypeMetadata       = 0x01
	TypeData           = 0x02
	TypePing           = 0x03
	TypePong           = 0x04
	TypeError          = 0x7f
	MaxRecordSize      = 1 * 1024 * 1024
)

const (
	headerVersionOffset    = 0
	headerTypeOffset       = 1
	headerTimestampOffset  = 2
	headerTimestampSize    = 8
	headerPayloadLenOffset = 10
	headerPaddingLenOffset = 14
	headerIVOffset         = 18
	headerIVLength         = 12
)

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
	IV            []byte
	ErrorMessage  string
}

const (
	metadataPaddingMin = 16
	metadataPaddingMax = 256
	dataPaddingMin     = 1
	dataPaddingMax     = 32
)

// BuildMetadataRecord creates an encrypted metadata record.
func BuildMetadataRecord(host string, port uint16, maxPadding uint16, psk string) ([]byte, error) {
	plaintext, err := buildMetadataPayload(host, port, maxPadding)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	key, err := deriveKey(psk, iv)
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
	header, err := buildHeader(TypeMetadata, ciphertextLen, paddingLen, iv)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, header)

	return buildRecord(header, ciphertext, padding), nil
}

// BuildDataRecord creates a data record with optional padding.
func BuildDataRecord(payload []byte, maxPadding uint16) ([]byte, error) {
	paddingLength := randomPadding(maxPadding)
	padding := make([]byte, paddingLength)
	if paddingLength > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}

	iv := make([]byte, headerIVLength)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	header, err := buildHeader(TypeData, len(payload), len(padding), iv)
	if err != nil {
		return nil, err
	}

	return buildRecord(header, payload, padding), nil
}

// BuildPingRecord creates a ping record.
func BuildPingRecord() ([]byte, error) {
	return buildControlRecord(TypePing)
}

// BuildPongRecord creates a pong record.
func BuildPongRecord() ([]byte, error) {
	return buildControlRecord(TypePong)
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

func buildHeader(recordType byte, payloadLen, paddingLen int, iv []byte) ([]byte, error) {
	if len(iv) != headerIVLength {
		return nil, fmt.Errorf("invalid IV length: %d", len(iv))
	}

	header := make([]byte, RecordHeaderLength)
	header[headerVersionOffset] = ProtocolVersion
	header[headerTypeOffset] = recordType
	binary.BigEndian.PutUint64(header[headerTimestampOffset:headerTimestampOffset+headerTimestampSize], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint32(header[headerPayloadLenOffset:headerPayloadLenOffset+4], uint32(payloadLen))
	binary.BigEndian.PutUint32(header[headerPaddingLenOffset:headerPaddingLenOffset+4], uint32(paddingLen))
	copy(header[headerIVOffset:headerIVOffset+headerIVLength], iv)
	return header, nil
}

func buildControlRecord(recordType byte) ([]byte, error) {
	iv := make([]byte, headerIVLength)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	header, err := buildHeader(recordType, 0, 0, iv)
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
func DecryptMetadata(record *Record, psk string) (*Metadata, error) {
	if psk == "" {
		return nil, fmt.Errorf("missing psk")
	}

	// Double check header size
	if len(record.Header) != RecordHeaderLength {
		return nil, fmt.Errorf("invalid header length: %d", len(record.Header))
	}

	// Clone IV and Header to prevent potential overlap issues in GCM Open
	// In some environments, if nonce/AAD/ciphertext overlap, authentication fails.
	iv := make([]byte, len(record.IV))
	copy(iv, record.IV)

	header := make([]byte, len(record.Header))
	copy(header, record.Header)

	key, err := deriveKey(psk, iv)
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

	plaintext, err := gcm.Open(nil, iv, record.Payload, header)
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
func BuildErrorRecord(code uint16, message string) ([]byte, error) {
	messageBytes := []byte(message)
	payload := make([]byte, 4+len(messageBytes))
	binary.BigEndian.PutUint16(payload[0:2], code)
	copy(payload[4:], messageBytes)

	iv := make([]byte, headerIVLength)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	header, err := buildHeader(TypeError, len(payload), 0, iv)
	if err != nil {
		return nil, err
	}

	return buildRecord(header, payload, nil), nil
}
