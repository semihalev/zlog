package zlog

import (
	"io"
	"testing"
)

// BenchmarkAsyncWriterDirect tests AsyncWriter directly without logger overhead
func BenchmarkAsyncWriterDirect(b *testing.B) {
	aw := NewAsyncWriter(io.Discard, 1024)
	defer aw.Close()

	// Pre-format a log message
	msg := []byte("INFO [01-04|18:58:03] benchmark message with some content")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		aw.Write(msg)
	}
}

// BenchmarkGenericBufferPool tests the new generic buffer pool
func BenchmarkGenericBufferPool(b *testing.B) {
	b.Run("Get/Put-Small", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := GetBuffer(100)
			*buf = (*buf)[:100]
			PutBuffer(buf)
		}
	})

	b.Run("Get/Put-Medium", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := GetBuffer(1024)
			*buf = (*buf)[:1024]
			PutBuffer(buf)
		}
	})

	b.Run("Get/Put-Large", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := GetBuffer(8192)
			*buf = (*buf)[:8192]
			PutBuffer(buf)
		}
	})
}

// BenchmarkZeroCopyString tests zero-copy string conversions
func BenchmarkZeroCopyString(b *testing.B) {
	testString := "This is a test string for zero-copy conversion"
	testBytes := []byte(testString)

	b.Run("StringToBytes", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = StringToBytes(testString)
		}
	})

	b.Run("BytesToString", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = BytesToString(testBytes)
		}
	})

	b.Run("Traditional-StringToBytes", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = []byte(testString)
		}
	})

	b.Run("Traditional-BytesToString", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = string(testBytes)
		}
	})
}
