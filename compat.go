package zlog

import (
	"fmt"
	"os"
)

// Small buffer pool for integer conversions (removed - not needed with current optimization)

// KV represents a key-value pair for compatibility
type KV struct {
	Key   string
	Value any
}

// Logger compatibility methods that accept any type

// DebugKV logs debug with key-value pairs (backward compatible)
func (l *StructuredLogger) DebugKV(msg string, keysAndValues ...any) {
	if !l.shouldLog(LevelDebug) {
		return
	}
	l.logKV(LevelDebug, msg, keysAndValues...)
}

// InfoKV logs info with key-value pairs (backward compatible)
func (l *StructuredLogger) InfoKV(msg string, keysAndValues ...any) {
	if !l.shouldLog(LevelInfo) {
		return
	}
	l.logKV(LevelInfo, msg, keysAndValues...)
}

// WarnKV logs warning with key-value pairs (backward compatible)
func (l *StructuredLogger) WarnKV(msg string, keysAndValues ...any) {
	if !l.shouldLog(LevelWarn) {
		return
	}
	l.logKV(LevelWarn, msg, keysAndValues...)
}

// ErrorKV logs error with key-value pairs (backward compatible)
func (l *StructuredLogger) ErrorKV(msg string, keysAndValues ...any) {
	if !l.shouldLog(LevelError) {
		return
	}
	l.logKV(LevelError, msg, keysAndValues...)
}

// FatalKV logs fatal with key-value pairs and exits (backward compatible)
func (l *StructuredLogger) FatalKV(msg string, keysAndValues ...any) {
	l.logKV(LevelFatal, msg, keysAndValues...)
	os.Exit(1)
}

// logKV logs with key-value pairs using simple formatting
//
//go:noinline
func (l *StructuredLogger) logKV(level Level, msg string, keysAndValues ...any) {
	// Estimate size
	estimatedSize := 256 + len(msg)
	// Get buffer from pool
	bufPtr := getStructuredBuffer(estimatedSize)
	buf := *bufPtr

	// Ensure capacity
	if cap(buf) < estimatedSize {
		buf = make([]byte, estimatedSize, estimatedSize)
	} else {
		buf = buf[:estimatedSize]
	}
	pos := 0

	// Binary header
	pos += writeBinaryHeader(buf, level, l.sequence.Add(1))

	// Message
	msgLen := len(msg)
	if msgLen > 255 {
		msgLen = 255
	}
	buf[pos] = byte(msgLen)
	pos++
	copy(buf[pos:], msg[:msgLen])
	pos += msgLen

	// Convert KV pairs to fields
	fieldCount := len(keysAndValues) / 2
	if fieldCount > 255 {
		fieldCount = 255
	}
	buf[pos] = byte(fieldCount)
	pos++

	// Encode each KV pair as a string field
	for i := 0; i < len(keysAndValues)-1 && i/2 < fieldCount; i += 2 {
		if pos >= len(buf)-64 {
			break
		}

		// Convert key to string efficiently
		key := toString(keysAndValues[i])

		// Create appropriate field based on value type
		var field Field
		value := keysAndValues[i+1]

		// Handle nil specially
		if value == nil {
			field = String(key, "<nil>")
		} else {
			switch v := value.(type) {
			case string:
				field = String(key, v)
			case int:
				field = Int(key, v)
			case int64:
				field = Int64(key, v)
			case int32:
				field = Int(key, int(v))
			case int16:
				field = Int(key, int(v))
			case int8:
				field = Int(key, int(v))
			case uint:
				field = Uint(key, v)
			case uint64:
				field = Uint64(key, v)
			case uint32:
				field = Uint(key, uint(v))
			case uint16:
				field = Uint(key, uint(v))
			case uint8:
				field = Uint(key, uint(v))
			case float64:
				field = Float64(key, v)
			case float32:
				field = Float32(key, v)
			case bool:
				field = Bool(key, v)
			case []byte:
				field = Bytes(key, v)
			case error:
				field = String(key, v.Error())
			case fmt.Stringer:
				field = String(key, v.String())
			default:
				// Only use fmt.Sprint for unknown types
				field = String(key, fmt.Sprint(v))
			}
		}
		n := encodeField(buf[pos:], &field)
		if n == 0 {
			break // No more space
		}
		pos += n
	}

	// Write
	w := l.getWriter()
	w.Write(buf[:pos])

	// Return buffer to pool
	*bufPtr = buf
	putStructuredBuffer(bufPtr)
}

// Global compatibility functions that accept any type

// DebugKV logs debug with key-value pairs
func DebugKV(msg string, keysAndValues ...any) {
	Default().DebugKV(msg, keysAndValues...)
}

// InfoKV logs info with key-value pairs
func InfoKV(msg string, keysAndValues ...any) {
	Default().InfoKV(msg, keysAndValues...)
}

// WarnKV logs warning with key-value pairs
func WarnKV(msg string, keysAndValues ...any) {
	Default().WarnKV(msg, keysAndValues...)
}

// ErrorKV logs error with key-value pairs
func ErrorKV(msg string, keysAndValues ...any) {
	Default().ErrorKV(msg, keysAndValues...)
}

// FatalKV logs fatal with key-value pairs and exits
func FatalKV(msg string, keysAndValues ...any) {
	Default().FatalKV(msg, keysAndValues...)
}

// Helper to create field from any type (for convenience)
func Any(key string, value any) Field {
	// Use string representation for simplicity
	return String(key, fmt.Sprint(value))
}

// toString converts common types to string without allocation
func toString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		// Only allocate for non-string types
		return fmt.Sprint(v)
	}
}
