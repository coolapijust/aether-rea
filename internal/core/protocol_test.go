package core

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// TestBuildHeaderInto_MatchesBuildHeader verifies that buildHeaderInto produces
// identical bytes to the original buildHeader function.
func TestBuildHeaderInto_MatchesBuildHeader(t *testing.T) {
	sessionID := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	counter := uint64(42)
	payloadLen := 1024
	paddingLen := 0

	// Use buildHeader (allocating version)
	headerAlloc, err := buildHeader(TypeData, payloadLen, paddingLen, sessionID, counter)
	if err != nil {
		t.Fatalf("buildHeader failed: %v", err)
	}

	// Use buildHeaderInto (zero-alloc version)
	headerInline := make([]byte, RecordHeaderLength)
	if err := buildHeaderInto(headerInline, TypeData, payloadLen, paddingLen, sessionID, counter); err != nil {
		t.Fatalf("buildHeaderInto failed: %v", err)
	}

	// Compare all fields except timestamp (which uses time.Now())
	// Version
	if headerAlloc[headerVersionOffset] != headerInline[headerVersionOffset] {
		t.Errorf("Version mismatch: %x vs %x", headerAlloc[headerVersionOffset], headerInline[headerVersionOffset])
	}
	// Type
	if headerAlloc[headerTypeOffset] != headerInline[headerTypeOffset] {
		t.Errorf("Type mismatch: %x vs %x", headerAlloc[headerTypeOffset], headerInline[headerTypeOffset])
	}
	// PayloadLength
	allocPL := binary.BigEndian.Uint32(headerAlloc[headerPayloadLenOffset : headerPayloadLenOffset+4])
	inlinePL := binary.BigEndian.Uint32(headerInline[headerPayloadLenOffset : headerPayloadLenOffset+4])
	if allocPL != inlinePL {
		t.Errorf("PayloadLength mismatch: %d vs %d", allocPL, inlinePL)
	}
	// PaddingLength
	allocPad := binary.BigEndian.Uint32(headerAlloc[headerPaddingLenOffset : headerPaddingLenOffset+4])
	inlinePad := binary.BigEndian.Uint32(headerInline[headerPaddingLenOffset : headerPaddingLenOffset+4])
	if allocPad != inlinePad {
		t.Errorf("PaddingLength mismatch: %d vs %d", allocPad, inlinePad)
	}
	// SessionID
	if !bytes.Equal(headerAlloc[headerSessionIDOffset:headerSessionIDOffset+headerSessionIDLength],
		headerInline[headerSessionIDOffset:headerSessionIDOffset+headerSessionIDLength]) {
		t.Error("SessionID mismatch")
	}
	// Counter
	allocCounter := binary.BigEndian.Uint64(headerAlloc[headerCounterOffset : headerCounterOffset+headerCounterLength])
	inlineCounter := binary.BigEndian.Uint64(headerInline[headerCounterOffset : headerCounterOffset+headerCounterLength])
	if allocCounter != inlineCounter {
		t.Errorf("Counter mismatch: %d vs %d", allocCounter, inlineCounter)
	}
}

// TestBuildHeaderInto_DstTooSmall verifies error on insufficient buffer.
func TestBuildHeaderInto_DstTooSmall(t *testing.T) {
	dst := make([]byte, 10) // too small
	err := buildHeaderInto(dst, TypeData, 100, 0, []byte{1, 2, 3, 4}, 0)
	if err == nil {
		t.Fatal("expected error for dst too small")
	}
}

// TestBuildHeaderInto_InvalidSessionID verifies error on wrong SessionID length.
func TestBuildHeaderInto_InvalidSessionID(t *testing.T) {
	dst := make([]byte, RecordHeaderLength)
	err := buildHeaderInto(dst, TypeData, 100, 0, []byte{1, 2, 3}, 0) // 3 bytes, need 4
	if err == nil {
		t.Fatal("expected error for invalid SessionID length")
	}
}

// TestBuildDataRecordRoundTrip verifies that BuildDataRecord output can be
// parsed back by RecordReader.ReadNextRecord correctly.
func TestBuildDataRecordRoundTrip(t *testing.T) {
	ng, err := NewNonceGenerator()
	if err != nil {
		t.Fatalf("NewNonceGenerator: %v", err)
	}

	payload := []byte("Hello, Aether-Realist Protocol!")
	record, err := BuildDataRecord(payload, 0, ng)
	if err != nil {
		t.Fatalf("BuildDataRecord: %v", err)
	}
	defer PutBuffer(record)

	// Parse it back using RecordReader
	reader := NewRecordReader(bytes.NewReader(record))
	parsed, err := reader.ReadNextRecord()
	if err != nil {
		t.Fatalf("ReadNextRecord: %v", err)
	}

	// Verify fields
	if parsed.Version != ProtocolVersion {
		t.Errorf("Version: got %x, want %x", parsed.Version, ProtocolVersion)
	}
	if parsed.Type != TypeData {
		t.Errorf("Type: got %x, want %x", parsed.Type, TypeData)
	}
	if parsed.PayloadLength != uint32(len(payload)) {
		t.Errorf("PayloadLength: got %d, want %d", parsed.PayloadLength, len(payload))
	}
	if parsed.PaddingLength != 0 {
		t.Errorf("PaddingLength: got %d, want 0", parsed.PaddingLength)
	}
	if !bytes.Equal(parsed.Payload, payload) {
		t.Errorf("Payload: got %q, want %q", parsed.Payload, payload)
	}

	// Verify timestamp is within 5 seconds of now
	ts := time.Unix(0, int64(parsed.TimestampNano))
	if time.Since(ts) > 5*time.Second {
		t.Errorf("Timestamp too old: %v", ts)
	}
}

// TestBuildDataRecordLargePayload tests with max-size payload.
func TestBuildDataRecordLargePayload(t *testing.T) {
	ng, err := NewNonceGenerator()
	if err != nil {
		t.Fatalf("NewNonceGenerator: %v", err)
	}

	payload := make([]byte, GetMaxRecordPayload())
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	record, err := BuildDataRecord(payload, 0, ng)
	if err != nil {
		t.Fatalf("BuildDataRecord: %v", err)
	}
	defer PutBuffer(record)

	reader := NewRecordReader(bytes.NewReader(record))
	parsed, err := reader.ReadNextRecord()
	if err != nil {
		t.Fatalf("ReadNextRecord: %v", err)
	}

	if !bytes.Equal(parsed.Payload, payload) {
		t.Error("Large payload round-trip mismatch")
	}
}

// TestMultipleRecordRoundTrip tests reading multiple sequential records.
func TestMultipleRecordRoundTrip(t *testing.T) {
	ng, err := NewNonceGenerator()
	if err != nil {
		t.Fatalf("NewNonceGenerator: %v", err)
	}

	var buf bytes.Buffer
	payloads := []string{"first", "second", "third record data"}
	for _, p := range payloads {
		record, err := BuildDataRecord([]byte(p), 0, ng)
		if err != nil {
			t.Fatalf("BuildDataRecord: %v", err)
		}
		buf.Write(record)
		PutBuffer(record)
	}

	reader := NewRecordReader(&buf)
	for i, expected := range payloads {
		parsed, err := reader.ReadNextRecord()
		if err != nil {
			t.Fatalf("ReadNextRecord #%d: %v", i, err)
		}
		if string(parsed.Payload) != expected {
			t.Errorf("Record #%d: got %q, want %q", i, parsed.Payload, expected)
		}
	}
}
