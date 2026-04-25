package zlog

import (
	"io"
	"os"
	"sync/atomic"
	"time"
	"unsafe"
)

// Level represents logging severity
type Level uint8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// Constants
const (
	MagicHeader   uint32 = 0x5A4C4F47 // "ZLOG"
	Version       byte   = 1
	CacheLineSize        = 64 // CPU cache line size
)

// Logger is a simple high-performance logger
type Logger struct {
	format LogFormat
	level  atomic.Uint32
	writer Writer
	// Remove pool field - using global pool now
}

// New creates a new logger with auto-detected output format
func New() *Logger {
	l := &Logger{
		format: FormatBinary,
		writer: os.Stderr,
	}
	l.level.Store(uint32(LevelInfo)) // Default to Info level
	return l
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level Level) {
	l.level.Store(uint32(level))
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() Level {
	return Level(l.level.Load())
}

// SetWriter sets the output writer
func (l *Logger) SetWriter(w Writer) {
	l.writer = w
}

// shouldLog checks if the given level should be logged
func (l *Logger) shouldLog(level Level) bool {
	return l.level.Load() <= uint32(level)
}

// getWriter returns the current writer
func (l *Logger) getWriter() Writer {
	return l.writer
}

// Debug logs a debug message
func (l *Logger) Debug(msg string) {
	if !l.shouldLog(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg)
}

// Info logs an info message
func (l *Logger) Info(msg string) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	l.log(LevelInfo, msg)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string) {
	if !l.shouldLog(LevelWarn) {
		return
	}
	l.log(LevelWarn, msg)
}

// Error logs an error message
func (l *Logger) Error(msg string) {
	if !l.shouldLog(LevelError) {
		return
	}
	l.log(LevelError, msg)
}

// Fatal logs a fatal message and exits with code 1. The level guard mirrors
// the other Logger methods so behavior is consistent — but Fatal always
// exits, even when the level is filtered out (the message just isn't
// written).
func (l *Logger) Fatal(msg string) {
	if l.shouldLog(LevelFatal) {
		l.log(LevelFatal, msg)
	}
	os.Exit(1)
}

// log is the core logging function. Always routes through the pooled buffer
// because passing a stack-allocated slice to an io.Writer interface forces it
// to escape to the heap anyway.
func (l *Logger) log(level Level, msg string) {
	msgLen := min(len(msg), 65535)
	requiredSize := 16 + msgLen

	bufPtr := GetBuffer(requiredSize)
	buf := (*bufPtr)[:requiredSize]

	l.formatMessage(buf, level, msg[:msgLen])

	if l.writer != nil {
		l.writer.Write(buf)
	}

	PutBuffer(bufPtr)
}

// formatMessage formats the log message into the buffer.
//
//go:inline
func (l *Logger) formatMessage(buf []byte, level Level, msg string) {
	*(*uint32)(unsafe.Pointer(&buf[0])) = MagicHeader
	buf[4] = Version
	buf[5] = byte(level)
	*(*uint64)(unsafe.Pointer(&buf[6])) = unixNanos()
	*(*uint16)(unsafe.Pointer(&buf[14])) = uint16(len(msg))
	copy(buf[16:], msg)
}

// LogFormat represents the output format
type LogFormat int

const (
	FormatBinary LogFormat = iota
	FormatText
	FormatJSON
)

// Writer is an alias for io.Writer to avoid interface conversions
type Writer = io.Writer

// nanotime returns monotonic nanoseconds since process start. Linkname'd
// from the runtime — this is the one runtime time symbol that's
// portable across darwin/linux/windows and stable across Go versions
// (it's used by every high-performance Go library that wants a fast
// clock). On Linux/Windows we deliberately avoid runtime.walltime
// because that symbol simply isn't defined on those platforms.
//
//go:linkname nanotime runtime.nanotime
//go:noescape
func nanotime() int64

// baseWallNs / baseMonoNs are sampled once at package init: the wall
// clock reading and the monotonic reading at the same instant.
//
// Per-call we then compute Unix nanoseconds as
//
//	unixNanos() = baseWallNs + (nanotime() - baseMonoNs)
//
// which is a single VDSO call plus two int64 ops (~5-7ns on modern
// hardware) and avoids the ~25ns time.Now() construction + monotonic-
// adjust overhead.
//
// Trade-off: the reported wall clock drifts from the kernel's wall
// clock if NTP adjusts the system time after init. For log timestamps
// this is fine — we don't synchronize across machines at sub-second
// precision, and the drift over hours is on the order of milliseconds.
var (
	baseWallNs int64
	baseMonoNs int64
)

func init() {
	baseWallNs = time.Now().UnixNano()
	baseMonoNs = nanotime()
}

// unixNanos returns the current wall-clock time as Unix nanoseconds.
//
//go:inline
func unixNanos() uint64 {
	return uint64(baseWallNs + nanotime() - baseMonoNs)
}

// StdoutWriter returns a writer to stdout
func StdoutWriter() Writer {
	return os.Stdout
}

// StderrWriter returns a writer to stderr
func StderrWriter() Writer {
	return os.Stderr
}

// DiscardWriter returns a writer that discards all output
func DiscardWriter() Writer {
	return discardWriter{}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}
