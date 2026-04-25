package zlog

import (
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

// logfmtTSWidth is the byte length of an RFC3339 timestamp without the
// fractional seconds (e.g. "2006-01-02T15:04:05+07:00"). Used to size
// the per-writer timestamp cache.
const logfmtTSWidth = 25

// LogfmtWriter decodes binary log records and emits logfmt
// (key=value) lines. Hot path is alloc-free: pooled buffer pre-sized
// from a one-pass scan of the binary record, native byte order,
// classifier-table escaping, and a per-second timestamp cache so
// AppendFormat runs once per second instead of once per log.
type LogfmtWriter struct {
	out io.Writer

	mu        sync.Mutex
	cachedSec int64
	cachedTS  []byte // formatted RFC3339, written-into "time=..." prefix
}

// NewLogfmtWriter creates a new logfmt writer.
func NewLogfmtWriter(out io.Writer) *LogfmtWriter {
	return &LogfmtWriter{
		out:      out,
		cachedTS: make([]byte, 0, logfmtTSWidth),
	}
}

// appendCachedTime writes a "time=<rfc3339>" prefix into buf, refreshing
// the per-writer cache when the Unix second has changed. Caller must
// hold w.mu.
func (w *LogfmtWriter) appendCachedTime(buf []byte, ns uint64) []byte {
	sec := int64(ns / 1_000_000_000)
	if sec != w.cachedSec {
		w.cachedSec = sec
		w.cachedTS = time.Unix(sec, 0).AppendFormat(w.cachedTS[:0], time.RFC3339)
	}
	buf = append(buf, "time="...)
	buf = append(buf, w.cachedTS...)
	return buf
}

// writeStructured is the direct structured fast path used by StructuredLogger.
// It skips binary encode + decode and formats the already typed fields straight
// into the logfmt output buffer.
//
// Pre-sizes the pooled buffer with an upper bound so a long message or a
// large string field can't trigger an append-grow allocation; that's the
// only thing standing between this path and zero-alloc on big records.
func (w *LogfmtWriter) writeStructured(level Level, msg string, fields []Field) {
	// 64 covers "time=" + RFC3339 (~25) + " level=fatal" + " msg=" framing.
	// 2*len(msg) is the worst case where every byte gets backslash-escaped
	// inside quotes; +2 for the surrounding quotes.
	estimatedSize := 64 + 2*len(msg) + 2
	fieldCount := min(len(fields), 255)
	for i := 0; i < fieldCount; i++ {
		f := &fields[i]
		s := 3 + len(f.Key) // " key="
		switch f.Type {
		case FieldTypeString:
			s += 2 + 2*len(f.str) // worst-case escape doubles every byte
		case FieldTypeBytes:
			n := 65535
			if f.num < 65535 {
				n = int(f.num)
			}
			s += 2 * n // hex doubles
		default:
			s += 32 // numeric upper bound (covers float 'g' formatting)
		}
		estimatedSize += s
	}

	bufPtr := GetBuffer(estimatedSize)
	buf := (*bufPtr)[:0]

	w.mu.Lock()
	buf = w.appendCachedTime(buf, unixNanos())

	buf = append(buf, " level="...)
	buf = append(buf, getLevelString(level)...)

	buf = append(buf, " msg="...)
	buf = appendQuotedBytes(buf, StringToBytes(msg))

	for i := 0; i < fieldCount; i++ {
		f := &fields[i]
		buf = append(buf, ' ')
		buf = append(buf, f.Key...)
		buf = append(buf, '=')
		buf = appendLogfmtFieldValue(buf, f)
	}

	buf = append(buf, '\n')
	w.out.Write(buf)
	w.mu.Unlock()

	*bufPtr = buf[:0]
	PutBuffer(bufPtr)
}

// writeKV is the direct-text fast path for the KV (untyped) compatibility
// API. Mirrors writeStructured but consumes the alternating key/value
// pairs untouched.
func (w *LogfmtWriter) writeKV(level Level, msg string, kv []any) {
	pairs := len(kv) / 2
	// Upper bound: framing + worst-case escape doubling for the message,
	// plus per-pair " key=" + 32 bytes of value (covers numeric / bool /
	// short string). Long string/bytes values can still overshoot 32 and
	// trigger an append-grow alloc; for guaranteed zero-alloc on long
	// values, use the typed Field API.
	estimatedSize := 64 + 2*len(msg) + 2 + pairs*64
	bufPtr := GetBuffer(estimatedSize)
	buf := (*bufPtr)[:0]

	w.mu.Lock()
	buf = w.appendCachedTime(buf, unixNanos())

	buf = append(buf, " level="...)
	buf = append(buf, getLevelString(level)...)

	buf = append(buf, " msg="...)
	buf = appendQuotedBytes(buf, StringToBytes(msg))

	for i := 0; i < pairs; i++ {
		key := toString(kv[2*i])
		buf = append(buf, ' ')
		buf = append(buf, key...)
		buf = append(buf, '=')
		buf = appendAnyValue(buf, kv[2*i+1])
	}

	buf = append(buf, '\n')
	w.out.Write(buf)
	w.mu.Unlock()

	*bufPtr = buf[:0]
	PutBuffer(bufPtr)
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

	// Pre-pass: compute an upper bound on the rendered output size.
	// "time=" + RFC3339 (~25) + " level=fatal" + " msg=" framing fits in
	// ~64 bytes; quoting can double the message; per-field framing is
	// 3 + keyLen + value-rendering. Walking the field section here is
	// cheap and lets us request a buffer big enough that no append-grow
	// happens, keeping this path zero-alloc on long records.
	estimatedSize := 64 + 2*msgLen + 2
	if msgEnd < len(b) {
		// Field section present.
		fieldCount := int(b[msgEnd])
		scan := msgEnd + 1
		for i := 0; i < fieldCount && scan < len(b); i++ {
			if scan >= len(b) {
				break
			}
			keyLen := int(b[scan])
			scan++
			if scan+keyLen > len(b) {
				break
			}
			scan += keyLen
			if scan >= len(b) {
				break
			}
			ftype := FieldType(b[scan])
			scan++
			estimatedSize += 3 + keyLen
			switch ftype {
			case FieldTypeString:
				if scan+2 > len(b) {
					break
				}
				slen := int(*(*uint16)(unsafe.Pointer(&b[scan])))
				scan += 2 + slen
				estimatedSize += 2 + 2*slen // worst-case escape doubles
			case FieldTypeBytes:
				if scan+2 > len(b) {
					break
				}
				blen := int(*(*uint16)(unsafe.Pointer(&b[scan])))
				scan += 2 + blen
				estimatedSize += 2 * blen // hex doubles
			case FieldTypeFloat32:
				scan += 4
				estimatedSize += 32
			default:
				scan += 8
				estimatedSize += 32
			}
		}
	}

	bufPtr := GetBuffer(estimatedSize)
	buf := (*bufPtr)[:0]

	w.mu.Lock()
	buf = w.appendCachedTime(buf, timestamp)

	buf = append(buf, " level="...)
	buf = append(buf, getLevelString(level)...)

	buf = append(buf, " msg="...)
	buf = appendQuotedBytes(buf, b[msgStart:msgEnd])

	pos := msgEnd
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
	w.mu.Unlock()

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

func appendLogfmtFieldValue(buf []byte, f *Field) []byte {
	switch f.Type {
	case FieldTypeInt:
		return appendInt(buf, int64(f.num))
	case FieldTypeUint:
		return appendUint(buf, f.num)
	case FieldTypeBool:
		if f.num != 0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case FieldTypeFloat32:
		v := *(*float32)(unsafe.Pointer(&f.num))
		return strconv.AppendFloat(buf, float64(v), 'g', -1, 32)
	case FieldTypeFloat64:
		v := *(*float64)(unsafe.Pointer(&f.num))
		return strconv.AppendFloat(buf, v, 'g', -1, 64)
	case FieldTypeString:
		return appendQuotedBytes(buf, StringToBytes(f.str))
	case FieldTypeBytes:
		dataLen := 65535
		if f.num < 65535 {
			dataLen = int(f.num)
		}
		if f.ptr == nil || dataLen == 0 {
			return buf
		}
		return appendHex(buf, unsafe.Slice((*byte)(f.ptr), dataLen))
	default:
		return append(buf, '?')
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
