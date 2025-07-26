package zlog

import (
	"io"
	"sync/atomic"
	"unsafe"
)

// UltimateLogger - High-performance logger with zero allocations
type UltimateLogger struct {
	level    uint32
	writer   io.Writer
	sequence uint64
}

// NewUltimateLogger creates a zero-allocation logger
func NewUltimateLogger() *UltimateLogger {
	return &UltimateLogger{
		level:  uint32(LevelInfo),
		writer: io.Discard,
	}
}

// SetLevel sets the log level
func (l *UltimateLogger) SetLevel(level Level) {
	atomic.StoreUint32(&l.level, uint32(level))
}

// SetWriter sets the output writer
func (l *UltimateLogger) SetWriter(w io.Writer) {
	l.writer = w
}

// Info logs with zero allocations
//
//go:nosplit
func (l *UltimateLogger) Info(msg string) {
	const level = LevelInfo
	if atomic.LoadUint32(&l.level) > uint32(level) {
		return
	}
	l.log(level, msg)
}

// Debug logs a debug message
//
//go:nosplit
func (l *UltimateLogger) Debug(msg string) {
	if atomic.LoadUint32(&l.level) > uint32(LevelDebug) {
		return
	}
	l.log(LevelDebug, msg)
}

// Error logs an error message
//
//go:nosplit
func (l *UltimateLogger) Error(msg string) {
	if atomic.LoadUint32(&l.level) > uint32(LevelError) {
		return
	}
	l.log(LevelError, msg)
}

// log is the common logging function
//
//go:nosplit
func (l *UltimateLogger) log(level Level, msg string) {
	msgLen := len(msg)
	if msgLen > 200 {
		msgLen = 200
	}

	requiredSize := 23 + msgLen

	// For small messages, use stack allocation
	if requiredSize <= 128 {
		var stackBuf [128]byte
		l.formatUltimateMessage(stackBuf[:requiredSize], level, msg, msgLen)
		if l.writer != nil {
			l.writer.Write(stackBuf[:requiredSize])
		}
		return
	}

	// Get buffer from pool
	bufPtr := GetBuffer(requiredSize)
	buf := (*bufPtr)[:requiredSize]

	// Format message
	l.formatUltimateMessage(buf, level, msg, msgLen)

	// Write to output
	if l.writer != nil {
		l.writer.Write(buf)
	}

	// Return buffer to pool
	PutBuffer(bufPtr)
}

// formatUltimateMessage formats the message into the buffer
//
//go:inline
func (l *UltimateLogger) formatUltimateMessage(buf []byte, level Level, msg string, msgLen int) {
	p := unsafe.Pointer(&buf[0])

	*(*uint32)(p) = MagicHeader
	*(*byte)(unsafe.Pointer(uintptr(p) + 4)) = Version
	*(*byte)(unsafe.Pointer(uintptr(p) + 5)) = byte(level)

	seq := atomic.AddUint64(&l.sequence, 1)
	*(*uint64)(unsafe.Pointer(uintptr(p) + 6)) = seq
	*(*uint64)(unsafe.Pointer(uintptr(p) + 14)) = uint64(nanotime())
	*(*byte)(unsafe.Pointer(uintptr(p) + 22)) = byte(msgLen)

	if msgLen > 0 {
		copy(buf[23:], msg[:msgLen])
	}
}

// memmove copies memory (provided by runtime)
//
//go:linkname memmove runtime.memmove
//go:noescape
func memmove(to, from unsafe.Pointer, n uintptr)
