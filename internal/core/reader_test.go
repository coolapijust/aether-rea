package core

import (
	"bytes"
	"testing"
)

// TestRecordReaderLengthBufReuse verifies that the lengthBuf field is correctly
// reused across multiple ReadNextRecord calls without data corruption.
func TestRecordReaderLengthBufReuse(t *testing.T) {
	ng, err := NewNonceGenerator()
	if err != nil {
		t.Fatalf("NewNonceGenerator: %v", err)
	}

	// Write 5 records sequentially
	var buf bytes.Buffer
	payloads := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		payload := make([]byte, 100+i*50) // varying sizes: 100, 150, 200, 250, 300
		for j := range payload {
			payload[j] = byte(i*10 + j%10)
		}
		payloads[i] = payload

		record, err := BuildDataRecord(payload, 0, ng)
		if err != nil {
			t.Fatalf("BuildDataRecord #%d: %v", i, err)
		}
		buf.Write(record)
		PutBuffer(record)
	}

	// Read them back using the same RecordReader (which reuses lengthBuf)
	reader := NewRecordReader(&buf)
	for i := 0; i < 5; i++ {
		parsed, err := reader.ReadNextRecord()
		if err != nil {
			t.Fatalf("ReadNextRecord #%d: %v", i, err)
		}
		if !bytes.Equal(parsed.Payload, payloads[i]) {
			t.Errorf("Record #%d payload mismatch: got len=%d, want len=%d", i, len(parsed.Payload), len(payloads[i]))
		}
	}
}

// TestRecordReaderReadInterface tests the io.Reader interface that
// reassembles multiple records into continuous data.
func TestRecordReaderReadInterface(t *testing.T) {
	ng, err := NewNonceGenerator()
	if err != nil {
		t.Fatalf("NewNonceGenerator: %v", err)
	}

	// Build 3 data records
	var buf bytes.Buffer
	fullPayload := []byte("AAAA-BBBB-CCCC-DDDD")

	// Split into 3 chunks
	chunks := [][]byte{
		[]byte("AAAA-"),
		[]byte("BBBB-"),
		[]byte("CCCC-DDDD"),
	}

	for _, chunk := range chunks {
		record, err := BuildDataRecord(chunk, 0, ng)
		if err != nil {
			t.Fatalf("BuildDataRecord: %v", err)
		}
		buf.Write(record)
		PutBuffer(record)
	}

	// Read using Read() which should reassemble all chunks
	reader := NewRecordReader(&buf)
	result := make([]byte, len(fullPayload))
	totalRead := 0
	for totalRead < len(fullPayload) {
		n, err := reader.Read(result[totalRead:])
		if err != nil {
			t.Fatalf("Read at offset %d: %v", totalRead, err)
		}
		totalRead += n
	}

	if !bytes.Equal(result, fullPayload) {
		t.Errorf("Reassembled data: got %q, want %q", result, fullPayload)
	}
}
