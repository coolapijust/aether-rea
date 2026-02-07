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
	protocolLabel      = "aether-realist-v3"
	recordHeaderLength = 24
	typeMetadata       = 0x01
	typeData           = 0x02
	typeError          = 0x7f
)

// buildMetadataRecord creates an encrypted metadata record.
func buildMetadataRecord(host string, port uint16, maxPadding uint16, psk string, streamID uint64) ([]byte, error) {
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

	header := make([]byte, recordHeaderLength)
	header[0] = typeMetadata
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

// buildDataRecord creates a data record with optional padding.
func buildDataRecord(payload []byte, maxPadding uint16) ([]byte, error) {
	paddingLength := randomPadding(maxPadding)
	padding := make([]byte, paddingLength)
	if paddingLength > 0 {
		if _, err := rand.Read(padding); err != nil {
			return nil, err
		}
	}

	header := make([]byte, recordHeaderLength)
	header[0] = typeData
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(padding)))
	if _, err := rand.Read(header[12:24]); err != nil {
		return nil, err
	}

	return buildRecord(header, payload, padding), nil
}

// buildRecord assembles a complete record.
func buildRecord(header, payload, padding []byte) []byte {
	totalLength := recordHeaderLength + len(payload) + len(padding)
	record := make([]byte, 4+totalLength)
	binary.BigEndian.PutUint32(record[0:4], uint32(totalLength))
	copy(record[4:4+recordHeaderLength], header)
	copy(record[4+recordHeaderLength:], payload)
	copy(record[4+recordHeaderLength+len(payload):], padding)
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
	reader := hkdf.New(sha256.New, []byte(psk), []byte(protocolLabel), info)
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
