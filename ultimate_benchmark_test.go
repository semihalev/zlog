package zlog

import (
	"sync/atomic"
	"testing"
)

func BenchmarkUltimateLogger(b *testing.B) {
	logger := NewUltimateLogger()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}

func BenchmarkUltimateLoggerParallel(b *testing.B) {
	logger := NewUltimateLogger()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.Info("benchmark message")
		}
	})
}

// Benchmark raw operations for comparison
func BenchmarkRawMemcpy(b *testing.B) {
	src := "benchmark message"
	dst := make([]byte, 256)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Just copy the string
		copy(dst, src)
	}
}

func BenchmarkRawAtomicIncrement(b *testing.B) {
	var counter uint64

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		atomic.AddUint64(&counter, 1)
	}
}
