package zlog

import (
	"io"
	"testing"
)

// Benchmark comparison focusing on the critical path
func BenchmarkCriticalPath(b *testing.B) {
	b.Run("zlog-UltimateLogger", func(b *testing.B) {
		logger := NewUltimateLogger()
		logger.SetWriter(io.Discard)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("The quick brown fox jumps over the lazy dog")
		}
	})

	b.Run("zlog-StructuredLogger", func(b *testing.B) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("The quick brown fox jumps over the lazy dog",
				String("file", "server.go"),
				Int("line", 42))
		}
	})

	b.Run("zlog-BasicLogger", func(b *testing.B) {
		logger := New()
		logger.SetWriter(io.Discard)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info("The quick brown fox jumps over the lazy dog")
		}
	})
}

// Benchmark with real writers (not just discard)
func BenchmarkRealWorldWriters(b *testing.B) {
	msg := "The quick brown fox jumps over the lazy dog"

	b.Run("TerminalWriter", func(b *testing.B) {
		logger := NewStructured()
		// Create a terminal writer that discards output
		tw := &TerminalWriter{
			out:      DiscardWriter(),
			useColor: false,
			buf:      make([]byte, 0, 4096),
		}
		logger.SetWriter(tw)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info(msg, String("index", "value"))
		}
	})

	b.Run("MMapWriter", func(b *testing.B) {
		mw, err := NewMMapWriter("/tmp/bench_real.log", 100*1024*1024)
		if err != nil {
			b.Skip("MMapWriter not available")
		}
		defer mw.Close()

		logger := NewStructured()
		logger.SetWriter(mw)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Info(msg, String("index", "value"))
		}
	})
}

// Benchmark disabled logging (level filtering)
func BenchmarkDisabledLogging(b *testing.B) {
	b.Run("zlog-disabled", func(b *testing.B) {
		logger := NewUltimateLogger()
		logger.SetWriter(io.Discard)
		logger.SetLevel(LevelError) // Set to Error, log Debug
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			logger.Debug("This message should be filtered")
		}
	})
}

// Benchmark parallel logging (contention test)
func BenchmarkParallelLogging(b *testing.B) {
	b.Run("zlog-parallel", func(b *testing.B) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)
		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				logger.Info("Parallel log message",
					String("goroutine", "test"),
					Int("iteration", 1))
			}
		})
	})
}
