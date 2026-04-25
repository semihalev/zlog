package zlog

import (
	"io"
	"os"
	"sync/atomic"
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

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string) {
	l.log(LevelFatal, msg)
	os.Exit(1)
}

// log is the core logging function. Always routes through the pooled buffer
// because passing a stack-allocated slice to an io.Writer interface forces it
// to escape to the heap anyway.
func (l *Logger) log(level Level, msg string) {
	requiredSize := 16 + len(msg)

	bufPtr := GetBuffer(requiredSize)
	buf := (*bufPtr)[:requiredSize]

	l.formatMessage(buf, level, msg)

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

// walltime returns the wall-clock time as (seconds, nanoseconds-within-second)
// since the Unix epoch. Linkname'd to runtime.walltime so we go straight
// through the VDSO without the time.Time allocation/conversion overhead.
// Requires Go 1.26+ — earlier toolchains don't resolve this linkname target.
//
//go:linkname walltime runtime.walltime
//go:noescape
func walltime() (sec int64, nsec int32)

// unixNanos folds walltime() into a single uint64 of Unix nanoseconds. Used
// as the binary log timestamp; readers display this as a wall-clock time.
//
//go:inline
func unixNanos() uint64 {
	sec, nsec := walltime()
	return uint64(sec)*1_000_000_000 + uint64(nsec)
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
