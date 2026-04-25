package zlog

import (
	"io"
	"os"
	"sync/atomic"
	"unsafe"
)

// globalStackFields is the largest typed-Field count for which the
// global helpers will use a stack-allocated array internally to avoid
// a heap slice. Calls with more fields fall back to a per-call
// `make([]Field, len(args))`. 16 covers the typical structured-log
// shape; 99% of real-world calls have fewer than 10 fields.
const globalStackFields = 16

// defaultLogger is the global logger instance.
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

// Default returns the current default logger.
func Default() *StructuredLogger {
	return (*StructuredLogger)(atomic.LoadPointer(&defaultLogger))
}

// SetDefault sets the default global logger.
func SetDefault(logger *StructuredLogger) {
	atomic.StorePointer(&defaultLogger, unsafe.Pointer(logger))
}

// The default-logger global helpers come in two flavors:
//
//   - Debug / Info / Warn / Error / Fatal accept either typed Field
//     values or alternating key/value pairs. They are the
//     backward-compatible API:
//
//       zlog.Info("user logged in", "user_id", 12345)             // 0 allocs (KV path)
//       zlog.Info("user logged in", zlog.Int("user_id", 12345))   // 3 allocs (Field via ...any boxes)
//
//     The typed-Field call costs three structural allocations that no
//     compiler trick can eliminate: passing a 56-byte Field through a
//     ...any parameter heap-boxes each argument at the *callsite*, before
//     the function runs.
//
//   - DebugF / InfoF / WarnF / ErrorF / FatalF take typed Fields directly
//     via ...Field, with no boxing — they are the zero-allocation path
//     for high-frequency typed logging:
//
//       zlog.InfoF("user logged in", zlog.Int("user_id", 12345))  // 0 allocs
//
// For a logger you hold yourself (logger := zlog.NewStructured()),
// logger.Info(msg, fields...) already has the zero-alloc property —
// the boxing only happens when crossing a ...any variadic.

// Debug logs a debug message via the default logger.
//
// Accepts typed Fields or alternating key/value pairs; see DebugF for
// the zero-allocation typed-Field path.
func Debug(msg string, args ...any) {
	dispatchToDefault(LevelDebug, msg, args)
}

// Info logs an info message via the default logger.
//
// Accepts typed Fields or alternating key/value pairs; see InfoF for
// the zero-allocation typed-Field path.
func Info(msg string, args ...any) {
	dispatchToDefault(LevelInfo, msg, args)
}

// Warn logs a warning message via the default logger.
//
// Accepts typed Fields or alternating key/value pairs; see WarnF for
// the zero-allocation typed-Field path.
func Warn(msg string, args ...any) {
	dispatchToDefault(LevelWarn, msg, args)
}

// Error logs an error message via the default logger.
//
// Accepts typed Fields or alternating key/value pairs; see ErrorF for
// the zero-allocation typed-Field path.
func Error(msg string, args ...any) {
	dispatchToDefault(LevelError, msg, args)
}

// Fatal logs a fatal message via the default logger and exits with
// code 1. Always exits, even when the level filters the message.
//
// Accepts typed Fields or alternating key/value pairs; see FatalF for
// the zero-allocation typed-Field path.
func Fatal(msg string, args ...any) {
	logger := Default()
	if logger.shouldLog(LevelFatal) {
		if len(args) == 0 {
			logger.logFields(LevelFatal, msg, nil)
		} else if !routeTypedFromAny(logger, LevelFatal, msg, args) {
			logger.logKV(LevelFatal, msg, args...)
		}
	}
	os.Exit(1)
}

// DebugF logs a debug message with typed fields. Zero allocations
// because ...Field forwards directly without crossing an interface.
func DebugF(msg string, fields ...Field) {
	Default().Debug(msg, fields...)
}

// InfoF logs an info message with typed fields. Zero allocations.
func InfoF(msg string, fields ...Field) {
	Default().Info(msg, fields...)
}

// WarnF logs a warning message with typed fields. Zero allocations.
func WarnF(msg string, fields ...Field) {
	Default().Warn(msg, fields...)
}

// ErrorF logs an error message with typed fields. Zero allocations.
func ErrorF(msg string, fields ...Field) {
	Default().Error(msg, fields...)
}

// FatalF logs a fatal message with typed fields and exits with code 1.
// Zero allocations.
func FatalF(msg string, fields ...Field) {
	Default().Fatal(msg, fields...)
}

// dispatchToDefault is the shared body of Debug/Info/Warn/Error.
// Routes typed-Field calls through the structured path and untyped
// key/value calls through the KV path. Bare-message calls skip
// argument inspection entirely.
//
//go:inline
func dispatchToDefault(level Level, msg string, args []any) {
	logger := Default()
	if len(args) == 0 {
		// Bare message: no allocation cost, dispatch directly to the
		// structured logger (which handles its own level check).
		switch level {
		case LevelDebug:
			logger.Debug(msg)
		case LevelInfo:
			logger.Info(msg)
		case LevelWarn:
			logger.Warn(msg)
		case LevelError:
			logger.Error(msg)
		}
		return
	}
	if !logger.shouldLog(level) {
		return
	}
	if routeTypedFromAny(logger, level, msg, args) {
		return
	}
	logger.logKV(level, msg, args...)
}

// routeTypedFromAny detects calls whose every variadic argument is a
// Field and routes them through the structured (typed) path. Returns
// true if it handled the call. Returns false if any argument is not a
// Field — caller should fall back to the KV path.
//
// The 3-alloc structural cost on this path is at the Go callsite (each
// Field boxed into a ...any slot). What we save here is the dispatch +
// fmt.Sprint cost the KV path would otherwise pay on Field arguments.
func routeTypedFromAny(logger *StructuredLogger, level Level, msg string, args []any) bool {
	for i := range args {
		if _, ok := args[i].(Field); !ok {
			return false
		}
	}

	if len(args) <= globalStackFields {
		var stack [globalStackFields]Field
		for i := range args {
			stack[i] = args[i].(Field)
		}
		logger.logFields(level, msg, stack[:len(args)])
		return true
	}

	fields := make([]Field, len(args))
	for i := range args {
		fields[i] = args[i].(Field)
	}
	logger.logFields(level, msg, fields)
	return true
}

// SetLevel sets the minimum log level for the default logger.
func SetLevel(level Level) {
	Default().SetLevel(level)
}

// SetWriter sets the writer for the default logger.
func SetWriter(w io.Writer) {
	Default().SetWriter(w)
}
