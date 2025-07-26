package zlog

import (
	"io"
	"testing"
)

func BenchmarkUltralog(b *testing.B) {
	logger := NewUltimateLogger()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}

func BenchmarkUltralogParallel(b *testing.B) {
	logger := NewUltimateLogger()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.Info("benchmark message")
		}
	})
}

func BenchmarkStructuredLogger(b *testing.B) {
	logger := NewStructured()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark",
			String("key1", "value1"),
			Int("key2", 42),
			Bool("key3", true))
	}
}

func BenchmarkStructuredLoggerParallel(b *testing.B) {
	logger := NewStructured()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.Info("benchmark",
				String("key1", "value1"),
				Int("key2", 42),
				Bool("key3", true))
		}
	})
}

func BenchmarkStructuredLogger5Fields(b *testing.B) {
	logger := NewStructured()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark",
			String("key1", "value1"),
			Int("key2", 42),
			Bool("key3", true),
			Float64("key4", 3.14159),
			Uint64("key5", 999999))
	}
}

func BenchmarkStructuredLogger10Fields(b *testing.B) {
	logger := NewStructured()
	logger.SetWriter(io.Discard)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark",
			String("key1", "value1"),
			Int("key2", 42),
			Bool("key3", true),
			Float64("key4", 3.14159),
			Uint64("key5", 999999),
			String("key6", "value6"),
			Int64("key7", -12345),
			Float32("key8", 2.718),
			Bool("key9", false),
			Uint("key10", 42))
	}
}

func BenchmarkDisabledDebug(b *testing.B) {
	logger := New()
	logger.SetWriter(io.Discard)
	logger.SetLevel(LevelInfo) // Debug disabled

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Debug("this should not be logged")
	}
}

func BenchmarkAsyncWriter(b *testing.B) {
	aw := NewAsyncWriter(io.Discard, 1024)
	defer aw.Close()

	logger := New()
	logger.SetWriter(aw)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}

func BenchmarkMMapWriter(b *testing.B) {
	// Create temporary file for mmap
	mw, err := NewMMapWriter("/tmp/bench_log.bin", 10*1024*1024) // 10MB
	if err != nil {
		b.Fatal(err)
	}
	defer mw.Close()

	logger := New()
	logger.SetWriter(mw)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}
