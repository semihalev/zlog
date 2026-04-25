package zlog

import (
	"fmt"
	"io"
	"strconv"
	"time"
	"unsafe"
)

// LogfmtWriter decodes binary log records and emits logfmt
// (key=value) lines. Hot path is alloc-free: pooled buffer, no
// string conversions, native byte order, classifier-table escaping.
type LogfmtWriter struct {
	out io.Writer
}

// NewLogfmtWriter creates a new logfmt writer.
func NewLogfmtWriter(out io.Writer) *LogfmtWriter {
	return &LogfmtWriter{out: out}
}

// Write decodes a binary log entry and emits logfmt output.
func (w *LogfmtWriter) Write(b []byte) (int, error) {
	if len(b) < 16 {
		return 0, fmt.Errorf("invalid log entry: too short")
	}
	magic := *(*uint32)(unsafe.Pointer(&b[0]))
	if magic != MagicHeader {
		return 0, fmt.Errorf("invalid magic header")
	}

	level := Level(b[5])
	timestamp := *(*uint64)(unsafe.Pointer(&b[6]))
	msgLen := int(*(*uint16)(unsafe.Pointer(&b[14])))
	msgStart := 16
	msgEnd := msgStart + msgLen
	if msgEnd > len(b) {
		return 0, fmt.Errorf("invalid log entry: message truncated")
	}
	pos := msgEnd

	bufPtr := GetBuffer(256)
	buf := (*bufPtr)[:0]

	buf = append(buf, "time="...)
	t := time.Unix(0, int64(timestamp))
	buf = t.AppendFormat(buf, time.RFC3339)

	buf = append(buf, " level="...)
	buf = append(buf, getLevelString(level)...)

	buf = append(buf, " msg="...)
	buf = appendQuotedBytes(buf, b[msgStart:msgEnd])

	if pos < len(b) {
		fieldCount := int(b[pos])
		pos++

		for i := 0; i < fieldCount && pos < len(b); i++ {
			keyLen := int(b[pos])
			pos++
			if pos+keyLen > len(b) {
				break
			}
			keyStart := pos
			keyEnd := pos + keyLen
			pos += keyLen

			if pos >= len(b) {
				break
			}
			ftype := FieldType(b[pos])
			pos++

			buf = append(buf, ' ')
			buf = append(buf, b[keyStart:keyEnd]...)
			buf = append(buf, '=')

			buf, pos = appendLogfmtValue(buf, b, pos, ftype)
		}
	}

	buf = append(buf, '\n')

	_, err := w.out.Write(buf)

	*bufPtr = buf[:0]
	PutBuffer(bufPtr)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// getLevelString returns the lowercase string representation of a level.
func getLevelString(level Level) string {
	switch level {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	case LevelFatal:
		return "fatal"
	default:
		return "unknown"
	}
}

// appendQuotedBytes applies logfmt quoting using the shared classifier table.
func appendQuotedBytes(buf, s []byte) []byte {
	var flags byte
	for i := 0; i < len(s); i++ {
		flags |= classify[s[i]]
	}
	needsEscape := flags&1 != 0
	hasSpace := flags&2 != 0

	if !needsEscape && !hasSpace && len(s) > 0 {
		return append(buf, s...)
	}
	if !needsEscape && len(s) > 0 {
		buf = append(buf, '"')
		buf = append(buf, s...)
		return append(buf, '"')
	}

	buf = append(buf, '"')
	for _, c := range s {
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		default:
			buf = append(buf, c)
		}
	}
	return append(buf, '"')
}

// appendLogfmtValue decodes a binary field value into buf using native byte
// order and returns the new position.
func appendLogfmtValue(buf, b []byte, pos int, ft FieldType) ([]byte, int) {
	switch ft {
	case FieldTypeInt:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		v := *(*int64)(unsafe.Pointer(&b[pos]))
		return appendInt(buf, v), pos + 8

	case FieldTypeUint:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		v := *(*uint64)(unsafe.Pointer(&b[pos]))
		return appendUint(buf, v), pos + 8

	case FieldTypeBool:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		v := *(*uint64)(unsafe.Pointer(&b[pos]))
		if v != 0 {
			return append(buf, "true"...), pos + 8
		}
		return append(buf, "false"...), pos + 8

	case FieldTypeFloat32:
		if len(b)-pos < 4 {
			return append(buf, '?'), pos + 4
		}
		f := *(*float32)(unsafe.Pointer(&b[pos]))
		return strconv.AppendFloat(buf, float64(f), 'g', -1, 32), pos + 4

	case FieldTypeFloat64:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		f := *(*float64)(unsafe.Pointer(&b[pos]))
		return strconv.AppendFloat(buf, f, 'g', -1, 64), pos + 8

	case FieldTypeString:
		if len(b)-pos < 2 {
			return append(buf, '?'), pos + 2
		}
		slen := int(*(*uint16)(unsafe.Pointer(&b[pos])))
		if len(b)-pos < 2+slen {
			return append(buf, '?'), pos + 2 + slen
		}
		return appendQuotedBytes(buf, b[pos+2:pos+2+slen]), pos + 2 + slen

	case FieldTypeBytes:
		if len(b)-pos < 2 {
			return append(buf, '?'), pos + 2
		}
		blen := int(*(*uint16)(unsafe.Pointer(&b[pos])))
		if len(b)-pos < 2+blen {
			return append(buf, '?'), pos + 2 + blen
		}
		return appendHex(buf, b[pos+2:pos+2+blen]), pos + 2 + blen

	default:
		return append(buf, '?'), pos
	}
}
