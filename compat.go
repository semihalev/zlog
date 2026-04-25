package zlog

import (
	"fmt"
	"os"
	"strconv"
	"unsafe"
)

// KV represents a key-value pair for compatibility
type KV struct {
	Key   string
	Value any
}

// appendAnyValue formats an `any` value into buf without allocation for
// the common scalar / string / []byte types. error and fmt.Stringer go
// through their interface methods, which may allocate inside the user's
// implementation but never in zlog itself. fmt.Sprint is the last-resort
// path for genuinely unknown types and does allocate.
//
// Used by both the *TerminalWriter and *LogfmtWriter direct-text KV
// paths so they can avoid the binary encode + decode round-trip.
func appendAnyValue(buf []byte, v any) []byte {
	if v == nil {
		return append(buf, "<nil>"...)
	}
	switch x := v.(type) {
	case string:
		return escapeStringOptimized(buf, unsafe.Slice(unsafe.StringData(x), len(x)))
	case int:
		return appendInt(buf, int64(x))
	case int64:
		return appendInt(buf, x)
	case int32:
		return appendInt(buf, int64(x))
	case int16:
		return appendInt(buf, int64(x))
	case int8:
		return appendInt(buf, int64(x))
	case uint:
		return appendUint(buf, uint64(x))
	case uint64:
		return appendUint(buf, x)
	case uint32:
		return appendUint(buf, uint64(x))
	case uint16:
		return appendUint(buf, uint64(x))
	case uint8:
		return appendUint(buf, uint64(x))
	case float64:
		return strconv.AppendFloat(buf, x, 'g', -1, 64)
	case float32:
		return strconv.AppendFloat(buf, float64(x), 'g', -1, 32)
	case bool:
		if x {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case []byte:
		return appendHex(buf, x)
	case error:
		s := x.Error()
		return escapeStringOptimized(buf, unsafe.Slice(unsafe.StringData(s), len(s)))
	case fmt.Stringer:
		s := x.String()
		return escapeStringOptimized(buf, unsafe.Slice(unsafe.StringData(s), len(s)))
	default:
		return append(buf, fmt.Sprint(v)...)
	}
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
	if l.shouldLog(LevelFatal) {
		l.logKV(LevelFatal, msg, keysAndValues...)
	}
	os.Exit(1)
}

// logKV logs with key-value pairs using simple formatting.
//
// Caveat: the fixed 256-byte field-section budget below means a KV log
// with many long string values can have its trailing fields silently
// truncated. For guaranteed completeness use the typed Field API.
//
//go:noinline
func (l *StructuredLogger) logKV(level Level, msg string, keysAndValues ...any) {
	// Direct-text fast paths skip the binary encode + re-decode.
	switch tw := l.getWriter().(type) {
	case *TerminalWriter:
		tw.writeKV(level, msg, keysAndValues)
		return
	case *LogfmtWriter:
		tw.writeKV(level, msg, keysAndValues)
		return
	}

	estimatedSize := 256 + len(msg)
	bufPtr := getStructuredBuffer(estimatedSize)
	buf := (*bufPtr)[:cap(*bufPtr)]

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

	fieldCount := min(len(keysAndValues)/2, 255)
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
			break
		}
		pos += n
	}

	if w := l.getWriter(); w != nil {
		w.Write(buf[:pos])
	}

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

// Any creates a string field from an arbitrary value via fmt.Sprint.
//
// This is a convenience that allocates — fmt.Sprint always returns a
// freshly-built string. For zero-allocation logging use the typed
// constructors (String, Int, Float64, ...) instead.
func Any(key string, value any) Field {
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
