package zlog

import (
	"os"
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

// getStructuredBuffer gets a buffer for structured logging. The caller
// already computes a tight upper bound; no extra padding needed.
//
//go:inline
func getStructuredBuffer(estimatedSize int) *[]byte {
	return GetBuffer(estimatedSize)
}

//go:inline
func putStructuredBuffer(buf *[]byte) {
	PutBuffer(buf)
}

// StructuredLogger provides zero-allocation structured logging.
//
// The previous version held an atomic sequence counter and stamped every
// record with it. That counter was unread by any writer in the package
// and was the dominant source of cross-core cache traffic — dropping it
// removed the parallel-scaling cliff (~2.5×). If you need ordering, the
// timestamp is monotonic enough at nanosecond precision.
type StructuredLogger struct {
	*Logger
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

// logFields logs with fields using a pooled buffer. A stack buffer would
// escape to the heap as soon as the slice crosses the io.Writer interface
// boundary, so we always use the pool — that's where zero-alloc actually
// holds (warm sync.Pool).
//
// logFields encodes a structured record and writes it. If the writer is a
// *TerminalWriter we skip the binary intermediate entirely and format text
// straight into the pooled buffer — the most common case (humans reading
// logs in a terminal) is also the fastest.
//
//go:noinline
func (l *StructuredLogger) logFields(level Level, msg string, fields []Field) {
	w := l.getWriter()
	if tw, ok := w.(*TerminalWriter); ok {
		tw.writeStructured(level, msg, fields)
		return
	}

	msgLen := min(len(msg), 65535)
	// 16-byte header + msgLen + 1-byte fieldCount + per-field upper bound.
	// Per field: 1 (keyLen) + key + 1 (type) + value. Value is 8 bytes for
	// numerics (the max), or 2 + payload length for string/bytes. f.num is
	// the byte count only for FieldTypeBytes; for everything else it's the
	// numeric value, so we must not blindly add it to the size.
	estimatedSize := 17 + msgLen
	for _, f := range fields {
		s := 3 + len(f.Key)
		switch f.Type {
		case FieldTypeString:
			s += 2 + len(f.str)
		case FieldTypeBytes:
			s += 2 + int(f.num)
		default:
			s += 8
		}
		estimatedSize += s
	}

	bufPtr := getStructuredBuffer(estimatedSize)
	buf := (*bufPtr)[:cap(*bufPtr)]

	n := formatStructuredMessage(buf, level, msg, fields)

	if w != nil {
		w.Write(buf[:n])
	}

	putStructuredBuffer(bufPtr)
}

// formatStructuredMessage encodes a structured record into buf using the
// unified 16-byte header (magic4 + ver1 + lvl1 + ts8 + msgLen2), followed
// by msg, a 1-byte field count, and the encoded fields. Native byte order:
// readers in this package are the only consumers, so big-endian buys
// nothing and costs a bswap per field.
func formatStructuredMessage(buf []byte, level Level, msg string, fields []Field) int {
	p := unsafe.Pointer(&buf[0])
	*(*uint32)(p) = MagicHeader
	*(*uint8)(unsafe.Add(p, 4)) = Version
	*(*uint8)(unsafe.Add(p, 5)) = byte(level)
	*(*uint64)(unsafe.Add(p, 6)) = unixNanos()

	msgLen := min(len(msg), 65535)
	*(*uint16)(unsafe.Add(p, 14)) = uint16(msgLen)
	pos := 16
	copy(buf[pos:], msg[:msgLen])
	pos += msgLen

	fieldCount := min(len(fields), 255)
	buf[pos] = byte(fieldCount)
	pos++

	for i := 0; i < fieldCount && pos < len(buf)-32; i++ {
		pos += encodeField(buf[pos:], &fields[i])
	}

	return pos
}

// encodeField encodes a field to the buffer in native byte order.
// Layout: keyLen(1) + key + type(1) + value (variable per type).
// Length-prefixed values use a uint16 length in native byte order; numeric
// values use a single 8-byte (or 4-byte) store. The big-endian byte-by-byte
// version was ~5× slower per field on amd64/arm64.
func encodeField(buf []byte, f *Field) int {
	if len(buf) < 10 {
		return 0
	}

	keyLen := min(len(f.Key), 255)
	if keyLen > len(buf)-2 {
		keyLen = len(buf) - 2
		if keyLen < 0 {
			return 0
		}
	}
	buf[0] = byte(keyLen)
	copy(buf[1:1+keyLen], f.Key[:keyLen])
	pos := 1 + keyLen
	buf[pos] = byte(f.Type)
	pos++

	switch f.Type {
	case FieldTypeInt, FieldTypeUint, FieldTypeBool, FieldTypeFloat64:
		if len(buf)-pos < 8 {
			return pos
		}
		*(*uint64)(unsafe.Pointer(&buf[pos])) = f.num
		pos += 8

	case FieldTypeFloat32:
		if len(buf)-pos < 4 {
			return pos
		}
		*(*uint32)(unsafe.Pointer(&buf[pos])) = uint32(f.num)
		pos += 4

	case FieldTypeString:
		if len(buf)-pos < 2 {
			return pos
		}
		strLen := min(len(f.str), len(buf)-pos-2)
		if strLen > 65535 {
			strLen = 65535
		}
		*(*uint16)(unsafe.Pointer(&buf[pos])) = uint16(strLen)
		pos += 2
		if strLen > 0 {
			copy(buf[pos:], f.str[:strLen])
			pos += strLen
		}

	case FieldTypeBytes:
		if len(buf)-pos < 2 {
			return pos
		}
		dataLen := min(int(f.num), len(buf)-pos-2)
		if dataLen > 65535 {
			dataLen = 65535
		}
		*(*uint16)(unsafe.Pointer(&buf[pos])) = uint16(dataLen)
		pos += 2
		if f.ptr != nil && dataLen > 0 {
			copy(buf[pos:], unsafe.Slice((*byte)(f.ptr), dataLen))
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
