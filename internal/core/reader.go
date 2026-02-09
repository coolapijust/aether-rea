package core

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
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
		return nil, errors.New("record length exceeds max")
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
	iv := recordBytes[headerIVOffset : headerIVOffset+headerIVLength]

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
		IV:            iv,
	}
	if recordType == TypeError {
		if len(payload) >= 4 {
			// Skip 2 bytes error code?
			// Worker.js: writeUint16(payload, 0, code); payload.set(messageBytes, 4);
			// So payload[0-1] is code. payload[4:] is message?
			// Wait, worker.js:
			// const payload = new Uint8Array(4 + messageBytes.byteLength);
			// writeUint16(payload, 0, code);
			// payload.set(messageBytes, 4);
			// So bytes 2 and 3 are 0?
			// Yes.
			result.ErrorMessage = string(payload[4:])
		}
	}
	return result, nil
}

// RecordReadWriter provides a unified io.ReadWriteCloser interface that handles
// all Record wrapping/unwrapping automatically.
type RecordReadWriter struct {
	*RecordReader
	writer     io.Writer
	closer     io.Closer
	maxPadding uint16
}

// NewRecordReadWriter creates a new RecordReadWriter.
func NewRecordReadWriter(rw io.ReadWriteCloser, maxPadding uint16) *RecordReadWriter {
	return &RecordReadWriter{
		RecordReader: NewRecordReader(rw),
		writer:       rw,
		closer:       rw,
		maxPadding:   maxPadding,
	}
}

// Write wraps data into core.Records before writing to the underlying stream.
// Write wraps data into core.Records before writing to the underlying stream.
// It splits large buffers into randomized chunks to obfuscate traffic patterns.
func (rw *RecordReadWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	totalWritten := 0
	src := p

	// Use a local random source seeded by time to avoid global lock contention
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	for len(src) > 0 {
		// Randomize chunk size between 2KB and 16KB
		// This breaks the correlation between application writes and network packets
		chunkSize := 2048 + rnd.Intn(14*1024)
		if chunkSize > len(src) {
			chunkSize = len(src)
		}

		chunk := src[:chunkSize]

		// Build record with padding
		record, err := BuildDataRecord(chunk, rw.maxPadding)
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
