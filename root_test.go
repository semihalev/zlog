package zlog

import (
	"bytes"
	"strings"
	"testing"
	"unsafe"
)

func TestGlobalLogger(t *testing.T) {
	// Save original logger
	original := Default()
	defer SetDefault(original)

	// Create a buffer to capture output
	var buf bytes.Buffer
	// Direct write to buffer

	// Create new logger with custom writer
	logger := NewStructured()
	logger.SetWriter(&buf)
	SetDefault(logger)

	// Test global functions
	Debug("debug message", String("key", "value"))
	Info("info message", Int("count", 42))
	Warn("warn message", Bool("flag", true))
	Error("error message", Float64("pi", 3.14159))

	// Verify output contains expected content
	output := buf.String()
	if len(output) == 0 {
		t.Error("No output captured")
	}

	// Since we're using binary format, just verify we got data. The exact
	// length depends on the header size and whether the global variadic
	// helpers route Field args through the structured or KV path; we only
	// require that something substantial was written.
	if buf.Len() < 80 {
		t.Errorf("Output too short: %d bytes", buf.Len())
	}
}

func TestGlobalSetLevel(t *testing.T) {
	// Save original logger
	original := Default()
	defer SetDefault(original)

	// Create a buffer to capture output
	var buf bytes.Buffer
	// Direct write to buffer

	// Create new logger
	logger := NewStructured()
	logger.SetWriter(&buf)
	SetDefault(logger)

	// Set level to Error
	SetLevel(LevelError)

	// These should not log
	buf.Reset()
	Debug("debug")
	Info("info")
	Warn("warn")

	if buf.Len() > 0 {
		t.Error("Lower level messages were logged")
	}

	// This should log
	buf.Reset()
	Error("error")

	if buf.Len() == 0 {
		t.Error("Error message was not logged")
	}
}

func TestGlobalSetWriter(t *testing.T) {
	// Save original logger
	original := Default()
	defer SetDefault(original)

	// Create new logger
	logger := NewStructured()
	SetDefault(logger)

	// Create a buffer to capture output
	var buf bytes.Buffer
	// Direct write to buffer

	// Set global writer
	SetWriter(&buf)

	// Log something
	Info("test message")

	// Verify output
	if buf.Len() == 0 {
		t.Error("No output captured after SetWriter")
	}
}

func TestGlobalTypedFieldsUseStructuredPath(t *testing.T) {
	original := Default()
	defer SetDefault(original)

	var buf bytes.Buffer
	logger := NewStructured()
	logger.SetWriter(&buf)
	SetDefault(logger)

	Info("typed fields", String("name", "zlog"), Int("count", 2))

	b := buf.Bytes()
	if len(b) < 17 {
		t.Fatalf("record too short: %d bytes", len(b))
	}
	msgLen := int(*(*uint16)(unsafe.Pointer(&b[14])))
	pos := 16 + msgLen
	if pos >= len(b) {
		t.Fatalf("record missing field count: len=%d pos=%d", len(b), pos)
	}
	if got := int(b[pos]); got != 2 {
		t.Fatalf("field count = %d, want 2", got)
	}
}

func TestDefaultLogger(t *testing.T) {
	// Test that default logger is initialized
	logger := Default()
	if logger == nil {
		t.Fatal("Default logger is nil")
	}

	// Test that we can use it immediately
	logger.Info("test")
}

func TestGlobalFatal(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fatal test in short mode")
	}

	// Run in subprocess to test exit
	if strings.Contains(strings.Join(callStack(), " "), "TestGlobalFatal") {
		Fatal("fatal error", String("test", "value"))
		return
	}

	// This test is similar to TestFatal in fatal_test.go
	// It would need subprocess testing to verify exit behavior
}

// Helper to get call stack
func callStack() []string {
	var stack []string
	// Simplified - in real implementation would use runtime.Callers
	return stack
}
