package core

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

// RecordReader reads records from a stream.
type RecordReader struct {
	reader io.Reader
	stash  []byte
}

// NewRecordReader creates a new record reader with a 1MB buffer.
func NewRecordReader(reader io.Reader) *RecordReader {
	// Wrap in a large 1MB buffer to reduce syscall overhead
	return &RecordReader{reader: bufio.NewReaderSize(reader, 1*1024*1024)}
}

// Read implements io.Reader, reassembling records into continuous data.
func (r *RecordReader) Read(p []byte) (int, error) {
	for len(r.stash) == 0 {
		record, err := r.ReadNextRecord()
		if err != nil {
			return 0, err
		}
		if record.Type == TypeError {
			return 0, errors.New("server error: " + record.ErrorMessage)
		}
		if record.Type != TypeData {
			continue
		}
		r.stash = record.Payload
	}

	n := copy(p, r.stash)
	r.stash = r.stash[n:]
	return n, nil
}

// ReadNextRecord reads and parses a single record.
func (r *RecordReader) ReadNextRecord() (*Record, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, lengthBytes); err != nil {
		return nil, err
	}

	totalLength := binary.BigEndian.Uint32(lengthBytes)
	if totalLength < RecordHeaderLength {
		return nil, errors.New("invalid record length")
	}
	if totalLength > MaxRecordSize {
		return nil, errors.New("handshake failed: potential PSK mismatch or server defense triggered (record length exceeds max)")
	}

	recordBytes := make([]byte, totalLength)
	if _, err := io.ReadFull(r.reader, recordBytes); err != nil {
		return nil, err
	}

	version := recordBytes[headerVersionOffset]
	if version != ProtocolVersion {
		return nil, errors.New("unsupported protocol version")
	}

	recordType := recordBytes[headerTypeOffset]
	timestamp := binary.BigEndian.Uint64(recordBytes[headerTimestampOffset : headerTimestampOffset+headerTimestampSize])
	payloadLength := binary.BigEndian.Uint32(recordBytes[headerPayloadLenOffset : headerPayloadLenOffset+4])
	paddingLength := binary.BigEndian.Uint32(recordBytes[headerPaddingLenOffset : headerPaddingLenOffset+4])
	// V5: Parse SessionID and Counter instead of IV
	sessionID := recordBytes[headerSessionIDOffset : headerSessionIDOffset+headerSessionIDLength]
	counter := binary.BigEndian.Uint64(recordBytes[headerCounterOffset : headerCounterOffset+headerCounterLength])

	if !IsTimestampValid(timestamp, time.Now(), DefaultReplayWindow) {
		return nil, errors.New("timestamp outside allowed window")
	}

	if int(RecordHeaderLength+payloadLength+paddingLength) != len(recordBytes) {
		return nil, errors.New("invalid payload length")
	}

	header := recordBytes[:RecordHeaderLength]
	payloadStart := RecordHeaderLength
	payloadEnd := payloadStart + int(payloadLength)
	payload := recordBytes[payloadStart:payloadEnd]

	result := &Record{
		Version:       version,
		Type:          recordType,
		TimestampNano: timestamp,
		PayloadLength: payloadLength,
		PaddingLength: paddingLength,
		Payload:       payload,
		Header:        header,
		SessionID:     sessionID,
		Counter:       counter,
	}
	if recordType == TypeError {
		if len(payload) >= 4 {
			result.ErrorMessage = string(payload[4:])
		}
	}
	return result, nil
}

// RecordReadWriter provides a unified io.ReadWriteCloser interface that handles
// all Record wrapping/unwrapping automatically.
// V5: Requires NonceGenerator for counter-based nonce.
type RecordReadWriter struct {
	*RecordReader
	writer     io.Writer
	closer     io.Closer
	maxPadding uint16
	nonceGen   *NonceGenerator
}

// NewRecordReadWriter creates a new RecordReadWriter.
// V5: Requires NonceGenerator for counter-based nonce.
func NewRecordReadWriter(rw io.ReadWriteCloser, maxPadding uint16, ng *NonceGenerator) *RecordReadWriter {
	return &RecordReadWriter{
		RecordReader: NewRecordReader(rw),
		writer:       rw,
		closer:       rw,
		maxPadding:   maxPadding,
		nonceGen:     ng,
	}
}

// Write wraps data into core.Records before writing to the underlying stream.
// V5: Uses NonceGenerator for counter-based nonce.
func (rw *RecordReadWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	totalWritten := 0
	src := p

	// V5.1 Optimization: 
	// Use 64KB as fixed maximum chunk size for application-layer records.
	// This avoids excessive header overhead of small chunks (V5.0 behavior)
	// while preventing AEAD latency spikes of too large records.
	const MaxRecordPayload = 64 * 1024

	for len(src) > 0 {
		chunkSize := len(src)
		if chunkSize > MaxRecordPayload {
			chunkSize = MaxRecordPayload
		}

		chunk := src[:chunkSize]

		// V5.1: Build record with NonceGenerator
		// Data records now have 0 padding for maximum throughput
		record, err := BuildDataRecord(chunk, rw.maxPadding, rw.nonceGen)
		if err != nil {
			return totalWritten, err
		}

		_, err = rw.writer.Write(record)
		if err != nil {
			return totalWritten, err
		}

		totalWritten += len(chunk)
		src = src[chunkSize:]
	}

	return totalWritten, nil
}

// Close closes the underlying stream.
func (rw *RecordReadWriter) Close() error {
	return rw.closer.Close()
}
