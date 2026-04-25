package zlog

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestLogfmtWriterDirectStructuredMatchesBinary(t *testing.T) {
	fields := []Field{
		String("user", "alice"),
		Int("attempt", 3),
		Bool("ok", true),
		Float64("latency", 12.5),
		Bytes("raw", []byte{0xab, 0xcd}),
		String("quoted", "hello \"world\"\n"),
	}

	var binary bytes.Buffer
	bufPtr := GetBuffer(512)
	buf := (*bufPtr)[:cap(*bufPtr)]
	n := formatStructuredMessage(buf, LevelInfo, "login accepted", fields)
	_, err := NewLogfmtWriter(&binary).Write(buf[:n])
	PutBuffer(bufPtr)
	if err != nil {
		t.Fatalf("binary logfmt write failed: %v", err)
	}

	var direct bytes.Buffer
	NewLogfmtWriter(&direct).writeStructured(LevelInfo, "login accepted", fields)

	if got, want := withoutLogfmtTime(direct.String()), withoutLogfmtTime(binary.String()); got != want {
		t.Fatalf("direct logfmt output mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func withoutLogfmtTime(line string) string {
	i := strings.IndexByte(line, ' ')
	if i < 0 {
		return line
	}
	return line[i:]
}

func BenchmarkLogfmtWriterComparison(b *testing.B) {
	b.Run("structured-logfmt", func(b *testing.B) {
		logger := NewStructured()
		logger.SetWriter(NewLogfmtWriter(io.Discard))

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("Request handled",
				String("method", "POST"),
				String("path", "/api/users"),
				Int("status", 200),
				Float64("duration", 1.234))
		}
	})

	b.Run("structured-logfmt-wrapped", func(b *testing.B) {
		logger := NewStructured()
		logger.SetWriter(logfmtWriteWrapper{NewLogfmtWriter(io.Discard)})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			logger.Info("Request handled",
				String("method", "POST"),
				String("path", "/api/users"),
				Int("status", 200),
				Float64("duration", 1.234))
		}
	})

	b.Run("binary-logfmt-writer", func(b *testing.B) {
		fields := []Field{
			String("method", "POST"),
			String("path", "/api/users"),
			Int("status", 200),
			Float64("duration", 1.234),
		}

		bufPtr := GetBuffer(512)
		buf := (*bufPtr)[:cap(*bufPtr)]
		n := formatStructuredMessage(buf, LevelInfo, "Request handled", fields)
		data := append([]byte(nil), buf[:n]...)
		PutBuffer(bufPtr)

		writer := NewLogfmtWriter(io.Discard)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			writer.Write(data)
		}
	})
}

type logfmtWriteWrapper struct {
	w *LogfmtWriter
}

func (w logfmtWriteWrapper) Write(p []byte) (int, error) {
	return w.w.Write(p)
}
