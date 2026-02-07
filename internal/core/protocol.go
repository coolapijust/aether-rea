package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"golang.org/x/crypto/hkdf"
)

const (
	ProtocolLabel      = "aether-realist-v3"
	RecordHeaderLength = 24
	TypeMetadata       = 0x01
	TypeData           = 0x02
	TypeError          = 0x7f
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
	Type         byte
	Payload      []byte
	Header       []byte
	IV           []byte
	ErrorMessage string
}

// BuildMetadataRecord creates an encrypted metadata record.
func BuildMetadataRecord(host string, port uint16, maxPadding uint16, psk string, streamID uint64) ([]byte, error) {
	plaintext, err := buildMetadataPayload(host, port, maxPadding)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	key, err := deriveKey(psk, streamID)
	if err != nil {
		return nil, err
	}

	header := make([]byte, RecordHeaderLength)
	header[0] = TypeMetadata
	binary.BigEndian.PutUint32(header[4:8], uint32(len(plaintext)))
	binary.BigEndian.PutUint32(header[8:12], 0)
	copy(header[12:24], iv)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, iv, plaintext, header)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(ciphertext)))

	return buildRecord(header, ciphertext, nil), nil
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

	header := make([]byte, RecordHeaderLength)
	header[0] = TypeData
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(padding)))
	if _, err := rand.Read(header[12:24]); err != nil {
		return nil, err
	}

	return buildRecord(header, payload, padding), nil
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
func deriveKey(psk string, streamID uint64) ([]byte, error) {
	info := []byte(fmt.Sprintf("%d", streamID))
	reader := hkdf.New(sha256.New, []byte(psk), []byte(ProtocolLabel), info)
	key := make([]byte, 16)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// randomPadding generates random padding length.
func randomPadding(maxPadding uint16) int {
	if maxPadding == 0 {
		return 0
	}
	n := make([]byte, 1)
	if _, err := rand.Read(n); err != nil {
		return 0
	}
	return int(n[0]) % int(maxPadding+1)
}

// DecryptMetadata decrypts the metadata record
func DecryptMetadata(record *Record, psk string, streamID uint64) (*Metadata, error) {
	if psk == "" {
		return nil, fmt.Errorf("missing psk")
	}
	key, err := deriveKey(psk, streamID)
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

	plaintext, err := gcm.Open(nil, record.IV, record.Payload, record.Header)
	if err != nil {
		return nil, err
	}

	return ParseMetadata(plaintext)
}

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

	// Error records have 0 padding
	// Header: Type(1) + PayloadLen(4) + PaddingLen(4) + IV(12)
	header := make([]byte, RecordHeaderLength)
	header[0] = TypeError
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint32(header[8:12], 0)
	// IV is zero for error records in worker.js implementation or random? 
	// Worker js: generic build logic uses random IV, but writeError uses explicit zero IV in parts? 
	// Wait, worker.js writeError:
	// Record byte 0-4: length
	// byte 4: TYPE_ERROR
	// ...
	// iv = new Uint8Array(12); // zeroes
	// record.set(iv, 16); 
	
	// Our buildRecord handles header assembly, but assumes we pass a header with IV already set?
	// The protocol seems to say IV is part of header. 
	// Let's manually build it to match worker.js strictness if needed.
	
	iv := make([]byte, 12) // Zero IV
	copy(header[12:24], iv)
	
	return buildRecord(header, payload, nil), nil
}
