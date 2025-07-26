package zlog

import (
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// countingWriter is a test helper that counts writes
type countingWriter struct {
	count *atomic.Int32
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	cw.count.Add(1)
	return len(p), nil
}

func TestEdgeCases(t *testing.T) {
	t.Run("FieldsLogDisabled", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)
		logger.SetLevel(LevelError) // Disable Info

		// This should not log
		logger.Info("should not log", String("key", "value"))
	})

	t.Run("MessageTooLong", func(t *testing.T) {
		logger := New()
		logger.SetWriter(io.Discard)

		// Create a very long message
		longMsg := strings.Repeat("x", 300)
		logger.Info(longMsg) // Should be truncated
	})

	t.Run("TooManyFields", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)

		// Create 300 fields (more than 255 max)
		fields := make([]Field, 300)
		for i := range fields {
			fields[i] = Int("key", i)
		}

		logger.Info("many fields", fields...)
	})

	t.Run("LongFieldKey", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)

		// Create a field with very long key
		longKey := strings.Repeat("k", 300)
		logger.Info("test", String(longKey, "value"))
	})

	t.Run("LongStringValue", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)

		// Create a very long string value
		longValue := strings.Repeat("v", 70000)
		logger.Info("test", String("key", longValue))
	})

	t.Run("LongBytesValue", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)

		// Create very long bytes
		longBytes := make([]byte, 70000)
		logger.Info("test", Bytes("key", longBytes))
	})

	t.Run("BufferOverflow", func(t *testing.T) {
		logger := NewStructured()
		logger.SetWriter(io.Discard)

		// Try to overflow the 1024 byte buffer
		fields := make([]Field, 0)
		for i := 0; i < 50; i++ {
			fields = append(fields, String("very_long_key_name_here", "very_long_value_here"))
		}

		logger.Info("overflow test", fields...)
	})
}

func TestTerminalWriterEdgeCases(t *testing.T) {
	tw := &TerminalWriter{useColor: false}

	t.Run("InvalidMagic", func(t *testing.T) {
		data := make([]byte, 30)
		_, err := tw.Write(data)
		if err == nil {
			t.Error("Expected error for invalid magic")
		}
	})

	t.Run("TooShort", func(t *testing.T) {
		data := make([]byte, 10)
		_, err := tw.Write(data)
		if err == nil {
			t.Error("Expected error for too short data")
		}
	})

	t.Run("UnknownFieldType", func(t *testing.T) {
		got := tw.decodeFieldValue([]byte{}, FieldType(99))
		if got != "?" {
			t.Errorf("Expected ?, got %v", got)
		}
	})

	t.Run("ShortBuffers", func(t *testing.T) {
		// Test with buffers too short for the type
		shortBuf := make([]byte, 2)

		if got := tw.decodeFieldValue(shortBuf, FieldTypeInt); got != "?" {
			t.Errorf("Expected ?, got %v for short int buffer", got)
		}

		if got := tw.decodeFieldValue(shortBuf, FieldTypeFloat32); got != "?" {
			t.Errorf("Expected ?, got %v for short float32 buffer", got)
		}

		if got := tw.decodeFieldValue(shortBuf, FieldTypeFloat64); got != "?" {
			t.Errorf("Expected ?, got %v for short float64 buffer", got)
		}
	})

	t.Run("FieldValueSizeUnknown", func(t *testing.T) {
		size := tw.fieldValueSize(nil, FieldType(99))
		if size != 0 {
			t.Errorf("Expected 0, got %v", size)
		}
	})

	t.Run("FieldValueSizeString", func(t *testing.T) {
		// Test string/bytes with valid length prefix
		buf := make([]byte, 10)
		buf[0] = 0 // high byte
		buf[1] = 5 // low byte - length = 5
		size := tw.fieldValueSize(buf, FieldTypeString)
		if size != 7 { // 2 + 5
			t.Errorf("Expected 7, got %v", size)
		}

		// Test with short buffer
		size = tw.fieldValueSize([]byte{}, FieldTypeString)
		if size != 0 {
			t.Errorf("Expected 0, got %v", size)
		}
	})
}

func TestMMapWriterErrors(t *testing.T) {
	t.Run("InvalidPath", func(t *testing.T) {
		_, err := NewMMapWriter("/invalid/path/that/does/not/exist", 1024)
		if err == nil {
			t.Error("Expected error for invalid path")
		}
	})

	t.Run("EmptyWrite", func(t *testing.T) {
		tmpfile, _ := os.CreateTemp("", "mmap")
		defer os.Remove(tmpfile.Name())
		tmpfile.Close()

		mw, _ := NewMMapWriter(tmpfile.Name(), 1024)
		defer mw.Close()

		// Write empty data
		_, err := mw.Write([]byte{})
		if err != nil {
			t.Error("Empty write should succeed")
		}
	})

	t.Run("WrapAround", func(t *testing.T) {
		tmpfile, _ := os.CreateTemp("", "mmap")
		defer os.Remove(tmpfile.Name())
		tmpfile.Close()

		mw, _ := NewMMapWriter(tmpfile.Name(), 100) // Very small buffer
		defer mw.Close()

		// Write enough to wrap around
		for i := 0; i < 20; i++ {
			mw.Write([]byte("test data"))
		}
	})

	t.Run("CrossPageBoundary", func(t *testing.T) {
		tmpfile, _ := os.CreateTemp("", "mmap")
		defer os.Remove(tmpfile.Name())
		tmpfile.Close()

		pageSize := os.Getpagesize()
		mw, _ := NewMMapWriter(tmpfile.Name(), int64(pageSize*2))
		defer mw.Close()

		// Write data that crosses page boundary
		data := make([]byte, pageSize+100)
		mw.Write(data)
	})

	t.Run("FileCreation", func(t *testing.T) {
		// Try to create mmap with new file
		tmpDir, _ := os.MkdirTemp("", "mmap_test")
		defer os.RemoveAll(tmpDir)

		newFile := tmpDir + "/new_file.log"
		mw, err := NewMMapWriter(newFile, 1024)
		if err != nil {
			t.Errorf("Failed to create new file: %v", err)
		} else {
			mw.Close()
		}
	})
}

func TestAsyncWriterEdgeCases(t *testing.T) {
	t.Run("BufferFull", func(t *testing.T) {
		var writeCount atomic.Int32

		// Create a custom writer that counts writes
		cw := &countingWriter{
			count: &writeCount,
		}

		aw := NewAsyncWriter(cw, 16)
		defer aw.Close()

		// Write many items
		for i := 0; i < 100; i++ {
			_, err := aw.Write([]byte("test"))
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		}

		// Verify writes happened (either async or direct)
		if writeCount.Load() == 0 {
			t.Error("No writes occurred")
		}
	})

	t.Run("EmptyRingBuffer", func(t *testing.T) {
		rb := NewCompatRingBuffer(16)
		// Try to put empty data
		if !rb.Put([]byte{}) {
			t.Error("Empty put should succeed")
		}

		// Try to get from empty buffer after consuming
		data, ok := rb.Get()
		if !ok || len(data) != 0 {
			t.Error("Expected empty data")
		}
	})
}

func TestRingBufferPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic for non-power-of-2 size")
		}
	}()

	// This should panic
	NewCompatRingBuffer(15) // Not power of 2
}

func TestEscapeStringLongUnicode(t *testing.T) {
	// Test with unicode that needs allocation
	longUnicode := "hello " + strings.Repeat("世", 50) + " world" // Add spaces to trigger quoting
	result := escapeString(longUnicode)
	if !strings.HasPrefix(result, `"`) {
		t.Error("Expected quoted string")
	}
}

func TestUltimateLoggerLongMessage(t *testing.T) {
	logger := NewUltimateLogger()

	// Test with message longer than 200 chars
	longMsg := strings.Repeat("x", 250)
	logger.Info(longMsg)
	logger.Debug(longMsg)
	logger.Error(longMsg)

	// Test wrap around
	for i := 0; i < 1000000; i++ {
		logger.Info("wrap test")
	}
}

func TestStderrWriter(t *testing.T) {
	// Just verify it doesn't panic
	w := os.Stderr
	w.Write([]byte("test"))
}
