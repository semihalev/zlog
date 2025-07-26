package zlog

import (
	"io"
	"os"
	"testing"
)

// BenchmarkWriters tests the performance of different writers
func BenchmarkWriters(b *testing.B) {
	msg := []byte("INFO [01-02|15:04:05] Test log message with some content\n")

	b.Run("DiscardWriter", func(b *testing.B) {
		w := DiscardWriter()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			w.Write(msg)
		}
	})

	b.Run("StdoutWriter", func(b *testing.B) {
		// Redirect stdout to discard during benchmark
		old := os.Stdout
		os.Stdout, _ = os.Open(os.DevNull)
		defer func() { os.Stdout = old }()

		w := StdoutWriter()
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			w.Write(msg)
		}
	})

	b.Run("io.Discard", func(b *testing.B) {
		w := io.Discard
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			w.Write(msg)
		}
	})
}

// BenchmarkLoggerWithDifferentWriters tests logger performance with different writers
func BenchmarkLoggerWithDifferentWriters(b *testing.B) {
	b.Run("Logger+DiscardWriter", func(b *testing.B) {
		logger := NewUltimateLogger()
		logger.SetWriter(DiscardWriter())
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("Benchmark log message")
		}
	})

	b.Run("Logger+io.Discard", func(b *testing.B) {
		logger := NewUltimateLogger()
		logger.SetWriter(io.Discard)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("Benchmark log message")
		}
	})

	b.Run("StructuredLogger+DiscardWriter", func(b *testing.B) {
		logger := NewStructured()
		logger.SetWriter(DiscardWriter())
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("Benchmark log message",
				String("key1", "value1"),
				Int("key2", 42))
		}
	})
}

// BenchmarkWriterThroughput tests raw throughput
func BenchmarkWriterThroughput(b *testing.B) {
	// 100 byte message (typical log line)
	msg := make([]byte, 100)
	for i := range msg {
		msg[i] = byte('a' + i%26)
	}

	b.Run("DiscardWriter-100bytes", func(b *testing.B) {
		w := DiscardWriter()
		b.SetBytes(int64(len(msg)))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			w.Write(msg)
		}
	})

	b.Run("MMapWriter-100bytes", func(b *testing.B) {
		mw, err := NewMMapWriter("/tmp/bench_throughput.log", 10*1024*1024)
		if err != nil {
			b.Skip("MMapWriter not available")
		}
		defer mw.Close()

		b.SetBytes(int64(len(msg)))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			mw.Write(msg)
		}
	})
}
