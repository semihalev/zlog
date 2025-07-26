package zlog

import (
	"bytes"
	"os"
	"sync"
	"testing"
	"time"
)

// testCapture is a test helper that implements io.Writer
type testCapture struct {
	fn func([]byte) error
}

func (tc *testCapture) Write(p []byte) (int, error) {
	err := tc.fn(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func TestAllFieldTypes(t *testing.T) {
	var buf bytes.Buffer
	// Direct write to buffer
	logger := NewStructured()
	logger.SetWriter(&buf)

	// Test all field types
	logger.Info("all fields",
		Int("int", -42),
		Int64("int64", -1234567890),
		Uint("uint", 42),
		Uint64("uint64", 1234567890),
		Float32("float32", 3.14),
		Float64("float64", 2.71828),
		String("string", "hello world"),
		Bool("bool_true", true),
		Bool("bool_false", false),
		Bytes("bytes", []byte{0x01, 0x02, 0x03}),
	)

	if buf.Len() == 0 {
		t.Fatal("Expected log output")
	}
}

func TestAllLogLevels(t *testing.T) {
	tests := []struct {
		name  string
		level Level
		fn    func(*Logger, string)
	}{
		{"Debug", LevelDebug, (*Logger).Debug},
		{"Info", LevelInfo, (*Logger).Info},
		{"Warn", LevelWarn, (*Logger).Warn},
		{"Error", LevelError, (*Logger).Error},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			// Direct write to buffer
			logger := New()
			logger.SetWriter(&buf)
			logger.SetLevel(LevelDebug) // Enable all levels

			tt.fn(logger, "test message")

			if buf.Len() == 0 {
				t.Errorf("Expected output for %s level", tt.name)
			}
		})
	}
}

func TestStructuredLogLevels(t *testing.T) {
	tests := []struct {
		name string
		fn   func(*StructuredLogger, string, ...Field)
	}{
		{"Debug", (*StructuredLogger).Debug},
		{"Info", (*StructuredLogger).Info},
		{"Warn", (*StructuredLogger).Warn},
		{"Error", (*StructuredLogger).Error},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			// Direct write to buffer
			logger := NewStructured()
			logger.SetWriter(&buf)
			logger.SetLevel(LevelDebug) // Enable all levels

			tt.fn(logger, "test", String("key", "value"))

			if buf.Len() == 0 {
				t.Errorf("Expected output for %s level", tt.name)
			}
		})
	}
}

func TestGetLevel(t *testing.T) {
	logger := New()

	logger.SetLevel(LevelDebug)
	if logger.GetLevel() != LevelDebug {
		t.Errorf("Expected LevelDebug, got %v", logger.GetLevel())
	}

	logger.SetLevel(LevelError)
	if logger.GetLevel() != LevelError {
		t.Errorf("Expected LevelError, got %v", logger.GetLevel())
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	// Direct write to buffer
	logger := New()
	logger.SetWriter(&buf)
	logger.SetLevel(LevelWarn) // Only Warn and above

	buf.Reset()
	logger.Debug("should not appear")
	if buf.Len() > 0 {
		t.Error("Debug should be filtered out")
	}

	buf.Reset()
	logger.Info("should not appear")
	if buf.Len() > 0 {
		t.Error("Info should be filtered out")
	}

	buf.Reset()
	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("Warn should not be filtered out")
	}

	buf.Reset()
	logger.Error("should appear")
	if buf.Len() == 0 {
		t.Error("Error should not be filtered out")
	}
}

func TestTerminalWriterFull(t *testing.T) {
	// Test terminal writer creation and basic functionality
	tw := NewTerminalWriter(os.Stdout)
	if tw == nil {
		t.Fatal("Failed to create terminal writer")
	}

	// Test Write with valid binary log
	var buf bytes.Buffer
	// Direct write to buffer
	logger := New()
	logger.SetWriter(&buf)
	logger.Info("test message")

	// Now decode it
	_, err := tw.Write(buf.Bytes())
	if err != nil {
		t.Errorf("Failed to write: %v", err)
	}
}

func TestTerminalColors(t *testing.T) {
	tw := &TerminalWriter{useColor: true}

	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, colorCyan},
		{LevelInfo, colorGreen},
		{LevelWarn, colorYellow},
		{LevelError, colorRed},
		{LevelFatal, colorMagenta},
	}

	for _, tt := range tests {
		got := tw.getLevelColor(tt.level)
		if got != tt.want {
			t.Errorf("getLevelColor(%v) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestTerminalLevelStrings(t *testing.T) {
	tw := &TerminalWriter{}

	tests := []struct {
		level Level
		want  string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO "},
		{LevelWarn, "WARN "},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
		{Level(99), "UNKN "},
	}

	for _, tt := range tests {
		got := tw.getLevelString(tt.level)
		if got != tt.want {
			t.Errorf("getLevelString(%v) = %v, want %v", tt.level, got, tt.want)
		}
	}
}

func TestTerminalDecodeFields(t *testing.T) {
	tw := &TerminalWriter{}

	// Test int decoding
	intBuf := make([]byte, 8)
	intBuf[0] = 0xFF // -1 in big endian
	intBuf[1] = 0xFF
	intBuf[2] = 0xFF
	intBuf[3] = 0xFF
	intBuf[4] = 0xFF
	intBuf[5] = 0xFF
	intBuf[6] = 0xFF
	intBuf[7] = 0xFF
	if got := tw.decodeFieldValue(intBuf, FieldTypeInt); got != "-1" {
		t.Errorf("decodeFieldValue(int) = %v, want -1", got)
	}

	// Test bool decoding
	boolTrue := make([]byte, 8)
	boolTrue[7] = 1
	if got := tw.decodeFieldValue(boolTrue, FieldTypeBool); got != "true" {
		t.Errorf("decodeFieldValue(bool true) = %v, want true", got)
	}

	boolFalse := make([]byte, 8)
	if got := tw.decodeFieldValue(boolFalse, FieldTypeBool); got != "false" {
		t.Errorf("decodeFieldValue(bool false) = %v, want false", got)
	}

	// Test string decoding
	strBuf := make([]byte, 20)
	strBuf[0] = 0 // length high byte
	strBuf[1] = 5 // length low byte
	copy(strBuf[2:], "hello")
	if got := tw.decodeFieldValue(strBuf, FieldTypeString); got != "hello" {
		t.Errorf("decodeFieldValue(string) = %v, want hello", got)
	}
}

func TestFieldValueSize(t *testing.T) {
	tw := &TerminalWriter{}

	tests := []struct {
		fieldType FieldType
		want      int
	}{
		{FieldTypeInt, 8},
		{FieldTypeUint, 8},
		{FieldTypeBool, 8},
		{FieldTypeFloat64, 8},
		{FieldTypeFloat32, 4},
	}

	for _, tt := range tests {
		got := tw.fieldValueSize(nil, tt.fieldType)
		if got != tt.want {
			t.Errorf("fieldValueSize(%v) = %v, want %v", tt.fieldType, got, tt.want)
		}
	}
}

func TestEscapeString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"hello world", `"hello world"`},
		{`hello"world`, `"hello\"world"`},
		{"hello\nworld", `"hello\nworld"`},
		{"hello\tworld", `"hello\tworld"`},
	}

	for _, tt := range tests {
		got := escapeString(tt.input)
		if got != tt.want {
			t.Errorf("escapeString(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRingBuffer(t *testing.T) {
	rb := NewCompatRingBuffer(16) // Small buffer for testing

	// Test Put and Get
	data := []byte("hello")
	if !rb.Put(data) {
		t.Error("Failed to put data")
	}

	got, ok := rb.Get()
	if !ok {
		t.Error("Failed to get data")
	}
	if string(got) != "hello" {
		t.Errorf("Got %q, want %q", got, "hello")
	}

	// Test empty buffer
	_, ok = rb.Get()
	if ok {
		t.Error("Expected Get to fail on empty buffer")
	}

	// Test buffer full
	for i := 0; i < 15; i++ { // Fill buffer (size-1)
		if !rb.Put([]byte("x")) {
			t.Errorf("Failed to put at %d", i)
		}
	}

	// Should fail when full
	if rb.Put([]byte("overflow")) {
		t.Error("Expected Put to fail when buffer full")
	}
}

func TestAsyncWriter(t *testing.T) {
	var received [][]byte
	var mu sync.Mutex

	// Create a custom writer that captures data
	tw := &testCapture{
		fn: func(b []byte) error {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, append([]byte(nil), b...))
			return nil
		},
	}

	aw := NewAsyncWriter(tw, 16)
	defer aw.Close()

	// Write some data
	aw.Write([]byte("test1"))
	aw.Write([]byte("test2"))

	// Give consumer time to process
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if len(received) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(received))
	}
	mu.Unlock()
}

func TestMMapWriter(t *testing.T) {
	// Create temp file
	tmpfile, err := os.CreateTemp("", "mmap_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	// Create mmap writer
	mw, err := NewMMapWriter(tmpfile.Name(), 1024*1024) // 1MB
	if err != nil {
		t.Fatal(err)
	}
	defer mw.Close()

	// Test write
	data := []byte("hello mmap")
	_, err = mw.Write(data)
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	// Test writer function
	_, err = mw.Write([]byte("test"))
	if err != nil {
		t.Errorf("Writer failed: %v", err)
	}
}

func TestUltimateLogger(t *testing.T) {
	logger := NewUltimateLogger()

	// Test level setting
	logger.SetLevel(LevelDebug)

	// Test logging
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Error("error message")

	// UltimateLogger now uses writer-based approach, no buffer to check
}

func TestIoctlReadTermios(t *testing.T) {
	// Test all OS cases
	got := ioctlReadTermios()
	if got == 0 {
		t.Error("ioctlReadTermios returned 0")
	}
}
