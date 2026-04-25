package zlog

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

const (
	termTimeFormat = "01-02|15:04:05"
	termMsgJust    = 40
	termTSWidth    = 17 // "[MM-DD|HH:MM:SS] "
)

// ANSI color codes used by the terminal writer.
const (
	colorReset   = "\x1b[0m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorMagenta = "\x1b[35m"
	colorCyan    = "\x1b[36m"
)

var spaces = []byte("                                        ") // 40

// Pre-built level prefixes. One append instead of three (color + text +
// reset) per log line. Keys also reuse the per-level color, with a single
// reset appended after the whole record (not per-field).
var (
	levelStrings = [6][]byte{
		[]byte("DEBUG"),
		[]byte("INFO "),
		[]byte("WARN "),
		[]byte("ERROR"),
		[]byte("FATAL"),
		[]byte("UNKN "),
	}

	levelStringsColored = [6][]byte{
		[]byte(colorCyan + "DEBUG" + colorReset),
		[]byte(colorGreen + "INFO " + colorReset),
		[]byte(colorYellow + "WARN " + colorReset),
		[]byte(colorRed + "ERROR" + colorReset),
		[]byte(colorMagenta + "FATAL" + colorReset),
		[]byte("UNKN "),
	}

	levelColors = [6][]byte{
		[]byte(colorCyan),
		[]byte(colorGreen),
		[]byte(colorYellow),
		[]byte(colorRed),
		[]byte(colorMagenta),
		nil,
	}

	colorResetBytes = []byte(colorReset)
)

// classify[c] is a 2-bit flag: bit0 = "needs backslash escape",
// bit1 = "is whitespace requiring quoting". One pass over the input ORs
// the table values together; the result tells us whether to take the fast
// no-quote path, the quote-only path, or the full escape path. The Go
// compiler converts byte LUT loads to NEON tbl / SSSE3 pshufb on
// supported architectures, giving ~1ns/byte throughput.
var classify = func() [256]byte {
	var t [256]byte
	t['"'] = 1
	t['\\'] = 1
	t['\n'] = 1
	t['\r'] = 1
	t['\t'] = 1
	t[' '] = 2
	t['='] = 2
	return t
}()

// TerminalWriter decodes binary log records and emits a colored terminal
// representation. It owns a reusable byte buffer guarded by mu so that
// every Write reuses the previous capacity (no per-call alloc).
//
// Hot-path optimizations:
//   - timestamp cache keyed by Unix second (AppendFormat runs once per
//     second instead of once per log).
//   - explicit Unlock at the bottom of Write (defer adds ~6ns).
//   - pre-built level prefix table (one append vs. three).
//   - byte-LUT classifier for escape detection (branchless).
//   - native byte order field decoding to match the encoder.
type TerminalWriter struct {
	out        io.Writer
	useColor   bool
	timeFormat string

	mu  sync.Mutex
	buf []byte

	cachedSec  int64
	cachedTime [termTSWidth]byte // "[MM-DD|HH:MM:SS] "
}

// NewTerminalWriter creates a new terminal writer.
func NewTerminalWriter(out io.Writer) *TerminalWriter {
	useColor := false
	if f, ok := out.(*os.File); ok {
		useColor = isTerminal(f.Fd())
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		useColor = false
	}

	return &TerminalWriter{
		out:        out,
		useColor:   useColor,
		timeFormat: termTimeFormat,
		buf:        make([]byte, 0, 2048),
	}
}

// Write decodes a binary log record and emits a formatted line.
//
// Layout (16-byte header):
//
//	0-3  magic 'ZLOG'
//	4    version
//	5    level
//	6-13 unix nanoseconds
//	14-15 msgLen (uint16, native order)
//	16+  msg, then optional fieldCount + fields
func (w *TerminalWriter) Write(b []byte) (int, error) {
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

	w.mu.Lock()

	buf := w.buf[:0]

	// Level prefix (color-baked variant when colors are on).
	if w.useColor && level < 5 {
		buf = append(buf, levelStringsColored[level]...)
	} else if level < 5 {
		buf = append(buf, levelStrings[level]...)
	} else {
		buf = append(buf, levelStrings[5]...)
	}
	buf = append(buf, ' ')

	// Cached timestamp: format only when the second changes.
	sec := int64(timestamp / 1_000_000_000)
	if sec != w.cachedSec {
		w.cachedSec = sec
		formatTimePrefix(w.cachedTime[:], time.Unix(sec, 0))
	}
	buf = append(buf, w.cachedTime[:]...)

	if msgEnd > msgStart {
		buf = append(buf, b[msgStart:msgEnd]...)
	}

	pos := msgEnd

	if pos < len(b) {
		padding := termMsgJust - msgLen
		if padding < 1 {
			padding = 1
		}
		if padding > len(spaces) {
			padding = len(spaces)
		}
		buf = append(buf, spaces[:padding]...)
	}

	if pos < len(b) {
		fieldCount := int(b[pos])
		pos++

		var levelColor []byte
		if w.useColor && level < 5 {
			levelColor = levelColors[level]
		}

		for i := 0; i < fieldCount && pos < len(b); i++ {
			if i > 0 {
				buf = append(buf, ' ')
			}

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

			fieldType := FieldType(b[pos])
			pos++

			if levelColor != nil {
				buf = append(buf, levelColor...)
				buf = append(buf, b[keyStart:keyEnd]...)
				buf = append(buf, colorResetBytes...)
				buf = append(buf, '=')
			} else {
				buf = append(buf, b[keyStart:keyEnd]...)
				buf = append(buf, '=')
			}

			buf, pos = w.decodeFieldValueBuf(buf, b, pos, fieldType)
		}
	}

	buf = append(buf, '\n')

	w.buf = buf
	_, err := w.out.Write(buf)
	w.mu.Unlock()
	return len(b), err
}

// writeStructured is the direct-text fast path used by StructuredLogger
// when its writer is a *TerminalWriter. It bypasses the binary encode +
// re-decode round-trip — typical real workloads (humans reading logs) hit
// this exclusively. Same output as Write(binaryEncoded).
func (w *TerminalWriter) writeStructured(level Level, msg string, fields []Field) {
	w.mu.Lock()

	buf := w.buf[:0]

	if w.useColor && level < 5 {
		buf = append(buf, levelStringsColored[level]...)
	} else if level < 5 {
		buf = append(buf, levelStrings[level]...)
	} else {
		buf = append(buf, levelStrings[5]...)
	}
	buf = append(buf, ' ')

	sec := int64(unixNanos() / 1_000_000_000)
	if sec != w.cachedSec {
		w.cachedSec = sec
		formatTimePrefix(w.cachedTime[:], time.Unix(sec, 0))
	}
	buf = append(buf, w.cachedTime[:]...)

	msgLen := len(msg)
	if msgLen > 0 {
		buf = append(buf, msg...)
	}

	if len(fields) > 0 {
		padding := termMsgJust - msgLen
		if padding < 1 {
			padding = 1
		}
		if padding > len(spaces) {
			padding = len(spaces)
		}
		buf = append(buf, spaces[:padding]...)

		var levelColor []byte
		if w.useColor && level < 5 {
			levelColor = levelColors[level]
		}

		for i := range fields {
			if i > 0 {
				buf = append(buf, ' ')
			}
			f := &fields[i]

			if levelColor != nil {
				buf = append(buf, levelColor...)
				buf = append(buf, f.Key...)
				buf = append(buf, colorResetBytes...)
				buf = append(buf, '=')
			} else {
				buf = append(buf, f.Key...)
				buf = append(buf, '=')
			}

			buf = appendFieldValue(buf, f)
		}
	}

	buf = append(buf, '\n')

	w.buf = buf
	w.out.Write(buf)
	w.mu.Unlock()
}

// formatTimePrefix renders "[MM-DD|HH:MM:SS] " into dst. dst must be
// exactly termTSWidth bytes. Uses two-digit ASCII via shift+mask without
// a lookup table, fitting in a few NEON/SSE-friendly stores.
//
//go:inline
func formatTimePrefix(dst []byte, t time.Time) {
	_, m, d := t.Date()
	h, mi, s := t.Clock()
	dst[0] = '['
	dst[1] = byte('0' + m/10)
	dst[2] = byte('0' + m%10)
	dst[3] = '-'
	dst[4] = byte('0' + d/10)
	dst[5] = byte('0' + d%10)
	dst[6] = '|'
	dst[7] = byte('0' + h/10)
	dst[8] = byte('0' + h%10)
	dst[9] = ':'
	dst[10] = byte('0' + mi/10)
	dst[11] = byte('0' + mi%10)
	dst[12] = ':'
	dst[13] = byte('0' + s/10)
	dst[14] = byte('0' + s%10)
	dst[15] = ']'
	dst[16] = ' '
}

// appendFieldValue serializes a Field directly to text. Mirrors
// decodeFieldValueBuf but skips the binary round-trip.
func appendFieldValue(buf []byte, f *Field) []byte {
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
		return escapeStringOptimized(buf, unsafe.Slice(unsafe.StringData(f.str), len(f.str)))
	case FieldTypeBytes:
		if f.ptr == nil || f.num == 0 {
			return buf
		}
		return appendHex(buf, unsafe.Slice((*byte)(f.ptr), int(f.num)))
	default:
		return append(buf, '?')
	}
}

// decodeFieldValueBuf decodes a binary field value into buf. Native byte
// order matches the encoder; numeric values are read with a single load.
func (w *TerminalWriter) decodeFieldValueBuf(buf, b []byte, pos int, fieldType FieldType) ([]byte, int) {
	switch fieldType {
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
		return escapeStringOptimized(buf, b[pos+2:pos+2+slen]), pos + 2 + slen

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

// escapeStringOptimized writes s into buf, applying logfmt-style quoting
// when needed. The classifier table makes the scan branchless.
func escapeStringOptimized(buf []byte, s []byte) []byte {
	var flags byte
	for i := 0; i < len(s); i++ {
		flags |= classify[s[i]]
	}
	needsEscape := flags&1 != 0
	hasSpace := flags&2 != 0

	if !needsEscape && !hasSpace {
		return append(buf, s...)
	}
	if !needsEscape {
		buf = append(buf, '"')
		buf = append(buf, s...)
		return append(buf, '"')
	}

	buf = append(buf, '"')
	for _, c := range s {
		switch c {
		case '\\':
			buf = append(buf, '\\', '\\')
		case '"':
			buf = append(buf, '\\', '"')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			buf = append(buf, c)
		}
	}
	return append(buf, '"')
}

// escapeString is the string-input wrapper kept for backwards compatibility.
func escapeString(s string) string {
	var buf []byte
	return BytesToString(escapeStringOptimized(buf, StringToBytes(s)))
}

// appendInt / appendUint / appendHex are the shared formatters used by
// both terminal and logfmt writers.

func appendInt(buf []byte, v int64) []byte {
	if v < 0 {
		buf = append(buf, '-')
		return appendUint(buf, uint64(-v))
	}
	return appendUint(buf, uint64(v))
}

func appendUint(buf []byte, v uint64) []byte {
	if v < 10 {
		return append(buf, byte('0'+v))
	}
	var tmp [20]byte
	i := len(tmp)
	for v >= 10 {
		q := v / 10
		i--
		tmp[i] = byte('0' + v - q*10)
		v = q
	}
	i--
	tmp[i] = byte('0' + v)
	return append(buf, tmp[i:]...)
}

func appendHex(buf []byte, data []byte) []byte {
	const hex = "0123456789abcdef"
	for _, b := range data {
		buf = append(buf, hex[b>>4], hex[b&0xf])
	}
	return buf
}

// getLevelColor / getLevelString / fieldValueSize / decodeFieldValue are
// kept as legacy helpers used by tests.

func (w *TerminalWriter) getLevelColor(level Level) string {
	if level < 5 {
		return string(levelColors[level])
	}
	return ""
}

func (w *TerminalWriter) getLevelString(level Level) string {
	if level < 5 {
		return string(levelStrings[level])
	}
	return string(levelStrings[5])
}

func (w *TerminalWriter) fieldValueSize(b []byte, fieldType FieldType) int {
	switch fieldType {
	case FieldTypeInt, FieldTypeUint, FieldTypeBool, FieldTypeFloat64:
		return 8
	case FieldTypeFloat32:
		return 4
	case FieldTypeString, FieldTypeBytes:
		if len(b) >= 2 {
			return 2 + int(*(*uint16)(unsafe.Pointer(&b[0])))
		}
	}
	return 0
}

func (w *TerminalWriter) decodeFieldValue(b []byte, fieldType FieldType) string {
	var buf []byte
	result, _ := w.decodeFieldValueBuf(buf, b, 0, fieldType)
	return string(result)
}

// StdoutTerminal creates a terminal writer for stdout
func StdoutTerminal() io.Writer {
	return NewTerminalWriter(os.Stdout)
}

// StderrTerminal creates a terminal writer for stderr
func StderrTerminal() io.Writer {
	return NewTerminalWriter(os.Stderr)
}

// SetColorEnabled allows manually enabling/disabling colors
func (w *TerminalWriter) SetColorEnabled(enabled bool) {
	w.mu.Lock()
	w.useColor = enabled
	w.mu.Unlock()
}

// IsColorEnabled returns whether colors are enabled
func (w *TerminalWriter) IsColorEnabled() bool {
	w.mu.Lock()
	v := w.useColor
	w.mu.Unlock()
	return v
}

// IsTerminal checks if the given writer is a terminal
func IsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isTerminal(f.Fd())
	}
	return false
}
