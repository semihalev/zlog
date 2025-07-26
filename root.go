package zlog

import (
	"io"
	"sync/atomic"
	"unsafe"
)

// defaultLogger is the global logger instance
var defaultLogger unsafe.Pointer

func init() {
	// Initialize with a structured logger
	logger := NewStructured()

	// By default, use terminal output for stderr if it's a terminal
	// This provides a better out-of-box experience
	// Users can always change it with SetWriter()
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
func Debug(msg string, keysAndValues ...any) {
	if len(keysAndValues) == 0 {
		Default().Debug(msg)
	} else {
		Default().DebugKV(msg, keysAndValues...)
	}
}

// Info logs an info message using the default logger
func Info(msg string, keysAndValues ...any) {
	if len(keysAndValues) == 0 {
		Default().Info(msg)
	} else {
		Default().InfoKV(msg, keysAndValues...)
	}
}

// Warn logs a warning message using the default logger
func Warn(msg string, keysAndValues ...any) {
	if len(keysAndValues) == 0 {
		Default().Warn(msg)
	} else {
		Default().WarnKV(msg, keysAndValues...)
	}
}

// Error logs an error message using the default logger
func Error(msg string, keysAndValues ...any) {
	if len(keysAndValues) == 0 {
		Default().Error(msg)
	} else {
		Default().ErrorKV(msg, keysAndValues...)
	}
}

// Fatal logs a fatal message using the default logger and exits
func Fatal(msg string, keysAndValues ...any) {
	if len(keysAndValues) == 0 {
		Default().Fatal(msg)
	} else {
		Default().FatalKV(msg, keysAndValues...)
	}
}

// SetLevel sets the minimum log level for the default logger
func SetLevel(level Level) {
	Default().SetLevel(level)
}

// SetWriter sets the writer for the default logger
func SetWriter(w io.Writer) {
	Default().SetWriter(w)
}
