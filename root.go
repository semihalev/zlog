package zlog

import (
	"io"
	"sync/atomic"
	"unsafe"
)

// defaultLogger is the global logger instance
var defaultLogger unsafe.Pointer

func init() {
	// Initialize with a structured logger writing colored, padded output
	// to stdout. TTY/color detection is handled by NewTerminalWriter, so
	// this stays sensible when stdout is piped or redirected to a file.
	// Users can swap the writer with SetWriter().
	logger := NewStructured()
	logger.SetWriter(StdoutTerminal())

	atomic.StorePointer(&defaultLogger, unsafe.Pointer(logger))
}

// Default returns the current default logger
func Default() *StructuredLogger {
	return (*StructuredLogger)(atomic.LoadPointer(&defaultLogger))
}

// SetDefault sets the default global logger
func SetDefault(logger *StructuredLogger) {
	atomic.StorePointer(&defaultLogger, unsafe.Pointer(logger))
}

// Global logging functions that use the default logger

// Debug logs a debug message using the default logger
func Debug(msg string, fields ...Field) {
	Default().Debug(msg, fields...)
}

// Info logs an info message using the default logger
func Info(msg string, fields ...Field) {
	Default().Info(msg, fields...)
}

// Warn logs a warning message using the default logger
func Warn(msg string, fields ...Field) {
	Default().Warn(msg, fields...)
}

// Error logs an error message using the default logger
func Error(msg string, fields ...Field) {
	Default().Error(msg, fields...)
}

// Fatal logs a fatal message using the default logger and exits
func Fatal(msg string, fields ...Field) {
	Default().Fatal(msg, fields...)
}

// SetLevel sets the minimum log level for the default logger
func SetLevel(level Level) {
	Default().SetLevel(level)
}

// SetWriter sets the writer for the default logger
func SetWriter(w io.Writer) {
	Default().SetWriter(w)
}
