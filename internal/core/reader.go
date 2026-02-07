package core

import (
	"encoding/binary"
	"errors"
	"io"
)

// recordReader reads records from a stream.
type recordReader struct {
	reader io.Reader
	stash  []byte
}

// newRecordReader creates a new record reader.
func newRecordReader(reader io.Reader) *recordReader {
	return &recordReader{reader: reader}
}

// Read implements io.Reader, reassembling records into continuous data.
func (r *recordReader) Read(p []byte) (int, error) {
	for len(r.stash) == 0 {
		record, err := r.readRecord()
		if err != nil {
			return 0, err
		}
		if record.recordType == typeError {
			return 0, errors.New("server error: " + record.errorMessage)
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

// record represents a parsed record.
type record struct {
	recordType   byte
	payload      []byte
	errorMessage string
}

// readRecord reads and parses a single record.
func (r *recordReader) readRecord() (*record, error) {
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, lengthBytes); err != nil {
		return nil, err
	}
	
	totalLength := binary.BigEndian.Uint32(lengthBytes)
	if totalLength < recordHeaderLength {
		return nil, errors.New("invalid record length")
	}

	recordBytes := make([]byte, totalLength)
	if _, err := io.ReadFull(r.reader, recordBytes); err != nil {
		return nil, err
	}

	recordType := recordBytes[0]
	payloadLength := binary.BigEndian.Uint32(recordBytes[4:8])
	paddingLength := binary.BigEndian.Uint32(recordBytes[8:12])

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
