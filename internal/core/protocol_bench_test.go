package core

import (
	"testing"
)

// BenchmarkBuildDataRecord benchmarks BuildDataRecord with the optimized
// buildHeaderInto (zero-allocation header construction).
func BenchmarkBuildDataRecord(b *testing.B) {
	ng, err := NewNonceGenerator()
	if err != nil {
		b.Fatalf("NewNonceGenerator: %v", err)
	}

	payload := make([]byte, GetMaxRecordPayload()) // 16KB payload
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		record, err := BuildDataRecord(payload, 0, ng)
		if err != nil {
			b.Fatalf("BuildDataRecord: %v", err)
		}
		PutBuffer(record)
	}
}

// BenchmarkBuildHeaderInto benchmarks the zero-allocation header builder.
func BenchmarkBuildHeaderInto(b *testing.B) {
	dst := make([]byte, RecordHeaderLength)
	sessionID := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = buildHeaderInto(dst, TypeData, 16384, 0, sessionID, uint64(i))
	}
}

// BenchmarkBuildHeader benchmarks the original allocating header builder
// for comparison.
func BenchmarkBuildHeader(b *testing.B) {
	sessionID := []byte{0xCA, 0xFE, 0xBA, 0xBE}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = buildHeader(TypeData, 16384, 0, sessionID, uint64(i))
	}
}

// BenchmarkNonceGenerator benchmarks the NonceGenerator.Next() method.
func BenchmarkNonceGenerator(b *testing.B) {
	ng, err := NewNonceGenerator()
	if err != nil {
		b.Fatalf("NewNonceGenerator: %v", err)
	}
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, err := ng.Next()
		if err != nil {
			b.Fatalf("Next: %v", err)
		}
	}
}
