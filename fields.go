package zlog

import (
	"os"
	"sync/atomic"
	"unsafe"
)

// FieldType represents the type of a field
type FieldType uint8

const (
	FieldTypeInt FieldType = iota
	FieldTypeUint
	FieldTypeFloat32
	FieldTypeFloat64
	FieldTypeString
	FieldTypeBool
	FieldTypeBytes
)

// Field represents a typed field without allocations
type Field struct {
	Key  string
	Type FieldType
	// Union-like storage - only one is used based on Type
	num uint64         // For int/uint/bool
	str string         // For string
	ptr unsafe.Pointer // For bytes
}

// Int creates an int field
//
//go:inline
func Int(key string, val int) Field {
	return Field{Key: key, Type: FieldTypeInt, num: uint64(val)}
}

// Int64 creates an int64 field
//
//go:inline
func Int64(key string, val int64) Field {
	return Field{Key: key, Type: FieldTypeInt, num: uint64(val)}
}

// Uint creates a uint field
//
//go:inline
func Uint(key string, val uint) Field {
	return Field{Key: key, Type: FieldTypeUint, num: uint64(val)}
}

// Uint64 creates a uint64 field
//
//go:inline
func Uint64(key string, val uint64) Field {
	return Field{Key: key, Type: FieldTypeUint, num: val}
}

// Float32 creates a float32 field
//
//go:inline
func Float32(key string, val float32) Field {
	return Field{Key: key, Type: FieldTypeFloat32, num: uint64(*(*uint32)(unsafe.Pointer(&val)))}
}

// Float64 creates a float64 field
//
//go:inline
func Float64(key string, val float64) Field {
	return Field{Key: key, Type: FieldTypeFloat64, num: *(*uint64)(unsafe.Pointer(&val))}
}

// String creates a string field
//
//go:inline
func String(key string, val string) Field {
	return Field{Key: key, Type: FieldTypeString, str: val}
}

// Bool creates a bool field
//
//go:inline
func Bool(key string, val bool) Field {
	n := uint64(0)
	if val {
		n = 1
	}
	return Field{Key: key, Type: FieldTypeBool, num: n}
}

// Bytes creates a bytes field
//
//go:inline
func Bytes(key string, val []byte) Field {
	return Field{Key: key, Type: FieldTypeBytes, ptr: unsafe.Pointer(&val[0]), num: uint64(len(val))}
}

// getStructuredBuffer gets a buffer for structured logging
func getStructuredBuffer(estimatedSize int) *[]byte {
	// Add some overhead for field encoding
	return GetBuffer(estimatedSize + 256)
}

// putStructuredBuffer returns a buffer to the pool
func putStructuredBuffer(buf *[]byte) {
	PutBuffer(buf)
}

// StructuredLogger provides zero-allocation structured logging
type StructuredLogger struct {
	*Logger
	sequence atomic.Uint64
}

// NewStructured creates a new structured logger
func NewStructured() *StructuredLogger {
	return &StructuredLogger{Logger: New()}
}

// shouldLog checks if the given level should be logged
func (l *StructuredLogger) shouldLog(level Level) bool {
	return l.Logger.shouldLog(level)
}

// getWriter returns the current writer
func (l *StructuredLogger) getWriter() Writer {
	return l.Logger.getWriter()
}

// logFields logs with fields using a pooled buffer
//
//go:noinline
func (l *StructuredLogger) logFields(level Level, msg string, fields []Field) {
	// Estimate required size
	msgLen := len(msg)
	if msgLen > 255 {
		msgLen = 255
	}

	// Calculate size: header(22) + msgLen(1) + msg + fieldCount(1) + fields
	estimatedSize := 24 + msgLen
	for _, f := range fields {
		estimatedSize += 2 + len(f.Key) + 16 // Conservative estimate
	}

	// For small logs, use stack allocation
	if estimatedSize <= 512 {
		var stackBuf [512]byte
		n := l.formatStructuredMessage(stackBuf[:], level, msg, fields)
		if l.getWriter() != nil {
			l.getWriter().Write(stackBuf[:n])
		}
		return
	}

	// Get buffer from pool
	bufPtr := getStructuredBuffer(estimatedSize)
	buf := *bufPtr

	// Ensure capacity
	if cap(buf) < estimatedSize {
		buf = make([]byte, 0, estimatedSize)
	}

	// Format message
	n := l.formatStructuredMessage(buf[:cap(buf)], level, msg, fields)

	// Write
	if l.getWriter() != nil {
		l.getWriter().Write(buf[:n])
	}

	// Return buffer to pool
	*bufPtr = buf
	putStructuredBuffer(bufPtr)
}

// formatStructuredMessage formats the message and returns bytes written
func (l *StructuredLogger) formatStructuredMessage(buf []byte, level Level, msg string, fields []Field) int {
	pos := 0

	// Binary header
	pos += writeBinaryHeader(buf[:], level, l.sequence.Add(1))

	// Message
	msgLen := len(msg)
	if msgLen > 255 {
		msgLen = 255
	}
	buf[pos] = byte(msgLen)
	pos++
	copy(buf[pos:], msg[:msgLen])
	pos += msgLen

	// Field count
	fieldCount := len(fields)
	if fieldCount > 255 {
		fieldCount = 255
	}
	buf[pos] = byte(fieldCount)
	pos++

	// Encode fields
	for i := 0; i < fieldCount && pos < len(buf)-64; i++ {
		pos += encodeField(buf[pos:], &fields[i])
	}

	return pos
}

// writeBinaryHeader writes the standard header
//
//go:inline
func writeBinaryHeader(buf []byte, level Level, seq uint64) int {
	// Use unsafe for faster header writing
	p := unsafe.Pointer(&buf[0])
	
	// Magic
	*(*uint32)(p) = MagicHeader
	
	// Version and Level
	*(*uint8)(unsafe.Add(p, 4)) = Version
	*(*uint8)(unsafe.Add(p, 5)) = byte(level)
	
	// Sequence
	*(*uint64)(unsafe.Add(p, 6)) = seq
	
	// Timestamp
	*(*uint64)(unsafe.Add(p, 14)) = uint64(nanotime())

	return 22
}

// encodeField encodes a field to the buffer
func encodeField(buf []byte, f *Field) int {
	if len(buf) < 10 { // Minimum space needed
		return 0
	}

	pos := 0

	// Key length and key
	keyLen := len(f.Key)
	if keyLen > 255 {
		keyLen = 255
	}
	if keyLen > len(buf)-pos-2 { // Reserve space for type
		keyLen = len(buf) - pos - 2
		if keyLen < 0 {
			return 0
		}
	}
	buf[pos] = byte(keyLen)
	pos++
	copy(buf[pos:], f.Key[:keyLen])
	pos += keyLen

	// Type
	buf[pos] = byte(f.Type)
	pos++

	// Value
	switch f.Type {
	case FieldTypeInt, FieldTypeUint, FieldTypeBool:
		if len(buf)-pos < 8 {
			return pos // Not enough space
		}
		buf[pos] = byte(f.num >> 56)
		buf[pos+1] = byte(f.num >> 48)
		buf[pos+2] = byte(f.num >> 40)
		buf[pos+3] = byte(f.num >> 32)
		buf[pos+4] = byte(f.num >> 24)
		buf[pos+5] = byte(f.num >> 16)
		buf[pos+6] = byte(f.num >> 8)
		buf[pos+7] = byte(f.num)
		pos += 8

	case FieldTypeFloat32:
		if len(buf)-pos < 4 {
			return pos
		}
		v := *(*uint32)(unsafe.Pointer(&f.num))
		buf[pos] = byte(v >> 24)
		buf[pos+1] = byte(v >> 16)
		buf[pos+2] = byte(v >> 8)
		buf[pos+3] = byte(v)
		pos += 4

	case FieldTypeFloat64:
		if len(buf)-pos < 8 {
			return pos
		}
		buf[pos] = byte(f.num >> 56)
		buf[pos+1] = byte(f.num >> 48)
		buf[pos+2] = byte(f.num >> 40)
		buf[pos+3] = byte(f.num >> 32)
		buf[pos+4] = byte(f.num >> 24)
		buf[pos+5] = byte(f.num >> 16)
		buf[pos+6] = byte(f.num >> 8)
		buf[pos+7] = byte(f.num)
		pos += 8

	case FieldTypeString:
		if len(buf)-pos < 2 {
			return pos
		}
		strLen := len(f.str)
		maxLen := len(buf) - pos - 2
		if strLen > maxLen {
			strLen = maxLen
		}
		if strLen > 65535 {
			strLen = 65535
		}
		buf[pos] = byte(strLen >> 8)
		buf[pos+1] = byte(strLen)
		pos += 2
		if strLen > 0 {
			copy(buf[pos:], f.str[:strLen])
			pos += strLen
		}

	case FieldTypeBytes:
		if len(buf)-pos < 2 {
			return pos
		}
		dataLen := int(f.num)
		maxLen := len(buf) - pos - 2
		if dataLen > maxLen {
			dataLen = maxLen
		}
		if dataLen > 65535 {
			dataLen = 65535
		}
		buf[pos] = byte(dataLen >> 8)
		buf[pos+1] = byte(dataLen)
		pos += 2
		if f.ptr != nil && dataLen > 0 {
			copy(buf[pos:], (*[65535]byte)(f.ptr)[:dataLen])
			pos += dataLen
		}
	}

	return pos
}

// Debug logs a debug message with fields
//
//go:inline
func (l *StructuredLogger) Debug(msg string, fields ...Field) {
	if l.shouldLog(LevelDebug) {
		l.logFields(LevelDebug, msg, fields)
	}
}

// Info logs an info message with fields
//
//go:inline
func (l *StructuredLogger) Info(msg string, fields ...Field) {
	if l.shouldLog(LevelInfo) {
		l.logFields(LevelInfo, msg, fields)
	}
}

// Warn logs a warning message with fields
//
//go:inline
func (l *StructuredLogger) Warn(msg string, fields ...Field) {
	if l.shouldLog(LevelWarn) {
		l.logFields(LevelWarn, msg, fields)
	}
}

// Error logs an error message with fields
//
//go:inline
func (l *StructuredLogger) Error(msg string, fields ...Field) {
	if l.shouldLog(LevelError) {
		l.logFields(LevelError, msg, fields)
	}
}

// Fatal logs a fatal message with fields and exits
//
//go:inline
func (l *StructuredLogger) Fatal(msg string, fields ...Field) {
	if l.shouldLog(LevelFatal) {
		l.logFields(LevelFatal, msg, fields)
		os.Exit(1)
	}
}
