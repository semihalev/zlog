package zlog

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"
)

const (
	termTimeFormat = "01-02|15:04:05"
	termMsgJust    = 40
)

// Color codes for terminal output
const (
	colorReset   = "\x1b[0m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorBlue    = "\x1b[34m"
	colorMagenta = "\x1b[35m"
	colorCyan    = "\x1b[36m"
	colorGray    = "\x1b[37m"
	colorBold    = "\x1b[1m"
)

// Pre-allocated spaces for padding
var spaces = []byte("                                        ") // 40 spaces

// Pre-allocated level strings and colors
var (
	levelStrings = [6][]byte{
		[]byte("DEBUG"),
		[]byte("INFO "),
		[]byte("WARN "),
		[]byte("ERROR"),
		[]byte("FATAL"),
		[]byte("UNKN "),
	}

	levelColors = [6][]byte{
		[]byte(colorCyan),
		[]byte(colorGreen),
		[]byte(colorYellow),
		[]byte(colorRed),
		[]byte(colorMagenta),
		[]byte(""),
	}

	colorResetBytes = []byte(colorReset)
)

// TerminalWriter decodes binary log format and outputs beautiful terminal format
type TerminalWriter struct {
	out        io.Writer
	useColor   bool
	timeFormat string

	// Pre-allocated buffer - reused for each write
	buf []byte
	mu  sync.Mutex
}

// NewTerminalWriter creates a new terminal writer
func NewTerminalWriter(out io.Writer) *TerminalWriter {
	// Check if we can detect terminal
	useColor := false
	if f, ok := out.(*os.File); ok {
		useColor = isTerminal(f.Fd())
	}

	// Allow disabling colors via environment variable
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		useColor = false
	}

	return &TerminalWriter{
		out:        out,
		useColor:   useColor,
		timeFormat: termTimeFormat,
		buf:        make([]byte, 0, 2048), // Pre-allocate 2KB buffer
	}
}

// Write decodes binary log and outputs formatted text
func (w *TerminalWriter) Write(b []byte) (int, error) {
	if len(b) < 16 { // Minimum header size
		return 0, fmt.Errorf("invalid log entry: too short")
	}

	// Decode binary header
	// Check for MagicHeader (ZLOG)
	magic := *(*uint32)(unsafe.Pointer(&b[0]))
	if magic != MagicHeader {
		return 0, fmt.Errorf("invalid magic header")
	}

	// version := b[4] // Currently unused
	level := Level(b[5])

	var timestamp uint64
	var msgLen int
	var msgStart int

	// Try to detect format by looking at the data
	// Basic logger: 16-byte header with 2-byte msgLen at offset 14
	// Structured logger: 22-byte header with 1-byte msgLen at offset 22

	// Try basic format first (most common)
	if len(b) >= 16 {
		possibleMsgLen := int(*(*uint16)(unsafe.Pointer(&b[14])))
		if possibleMsgLen > 0 && possibleMsgLen <= 65535 && len(b) >= 16+possibleMsgLen {
			// Looks like basic format
			timestamp = *(*uint64)(unsafe.Pointer(&b[6]))
			msgLen = possibleMsgLen
			msgStart = 16
		} else if len(b) >= 23 {
			// Try structured format
			possibleMsgLen = int(b[22])
			if len(b) >= 23+possibleMsgLen {
				// Looks like structured format
				timestamp = *(*uint64)(unsafe.Pointer(&b[14]))
				msgLen = possibleMsgLen
				msgStart = 23
			} else {
				return 0, fmt.Errorf("invalid log entry: cannot determine format")
			}
		} else {
			return 0, fmt.Errorf("invalid log entry: message truncated")
		}
	} else {
		return 0, fmt.Errorf("invalid log format: too short")
	}

	// Get message
	var msgEnd int
	if msgLen > 0 {
		msgEnd = msgStart + msgLen
	}

	// Lock to use our pre-allocated buffer
	w.mu.Lock()
	defer w.mu.Unlock()

	// Reset buffer
	buf := w.buf[:0]

	// Format level with color
	if w.useColor && level < 5 {
		buf = append(buf, levelColors[level]...)
		buf = append(buf, levelStrings[level]...)
		buf = append(buf, colorResetBytes...)
	} else if level < 5 {
		buf = append(buf, levelStrings[level]...)
	} else {
		buf = append(buf, levelStrings[5]...)
	}

	// Format timestamp
	buf = append(buf, '[')
	t := time.Unix(0, int64(timestamp))
	buf = t.AppendFormat(buf, w.timeFormat)
	buf = append(buf, "] "...)

	// Add message
	if msgEnd > msgStart {
		buf = append(buf, b[msgStart:msgEnd]...)
	}

	// Check if we have fields (for structured logger)
	pos := msgStart + msgLen

	// Add padding if we have fields
	if pos < len(b) && msgEnd-msgStart < termMsgJust {
		padding := termMsgJust - (msgEnd - msgStart)
		if padding > 0 && padding <= len(spaces) {
			buf = append(buf, spaces[:padding]...)
		}
	}

	// Decode fields if present
	if pos < len(b) {
		fieldCount := int(b[pos])
		pos++

		for i := 0; i < fieldCount && pos < len(b); i++ {
			if i > 0 {
				buf = append(buf, ' ')
			}

			// Get key
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

			// Format key with color
			if w.useColor && level < 5 {
				buf = append(buf, levelColors[level]...)
				buf = append(buf, b[keyStart:keyEnd]...)
				buf = append(buf, colorResetBytes...)
				buf = append(buf, '=')
			} else {
				buf = append(buf, b[keyStart:keyEnd]...)
				buf = append(buf, '=')
			}

			// Decode value
			buf, pos = w.decodeFieldValueBuf(buf, b, pos, fieldType)
		}
	}

	buf = append(buf, '\n')

	// Save expanded buffer for reuse
	w.buf = buf

	// Write to output
	_, err := w.out.Write(buf)
	return len(b), err
}

// decodeFieldValueBuf decodes a field value from binary into buffer
func (w *TerminalWriter) decodeFieldValueBuf(buf, b []byte, pos int, fieldType FieldType) ([]byte, int) {
	switch fieldType {
	case FieldTypeInt:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		// Big endian decoding
		v := uint64(b[pos])<<56 | uint64(b[pos+1])<<48 | uint64(b[pos+2])<<40 | uint64(b[pos+3])<<32 |
			uint64(b[pos+4])<<24 | uint64(b[pos+5])<<16 | uint64(b[pos+6])<<8 | uint64(b[pos+7])
		buf = appendInt(buf, int64(v))
		return buf, pos + 8

	case FieldTypeUint, FieldTypeBool:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		v := uint64(b[pos])<<56 | uint64(b[pos+1])<<48 | uint64(b[pos+2])<<40 | uint64(b[pos+3])<<32 |
			uint64(b[pos+4])<<24 | uint64(b[pos+5])<<16 | uint64(b[pos+6])<<8 | uint64(b[pos+7])
		if fieldType == FieldTypeBool {
			if v == 0 {
				return append(buf, "false"...), pos + 8
			}
			return append(buf, "true"...), pos + 8
		}
		buf = appendUint(buf, v)
		return buf, pos + 8

	case FieldTypeFloat32:
		if len(b)-pos < 4 {
			return append(buf, '?'), pos + 4
		}
		v := uint32(b[pos])<<24 | uint32(b[pos+1])<<16 | uint32(b[pos+2])<<8 | uint32(b[pos+3])
		f := *(*float32)(unsafe.Pointer(&v))
		buf = appendFloat32(buf, f)
		return buf, pos + 4

	case FieldTypeFloat64:
		if len(b)-pos < 8 {
			return append(buf, '?'), pos + 8
		}
		v := uint64(b[pos])<<56 | uint64(b[pos+1])<<48 | uint64(b[pos+2])<<40 | uint64(b[pos+3])<<32 |
			uint64(b[pos+4])<<24 | uint64(b[pos+5])<<16 | uint64(b[pos+6])<<8 | uint64(b[pos+7])
		f := *(*float64)(unsafe.Pointer(&v))
		buf = appendFloat64(buf, f)
		return buf, pos + 8

	case FieldTypeString:
		if len(b)-pos < 2 {
			return append(buf, '?'), pos + 2
		}
		slen := int(uint16(b[pos])<<8 | uint16(b[pos+1]))
		if len(b)-pos < 2+slen {
			return append(buf, '?'), pos + 2 + slen
		}
		// Escape string
		buf = escapeStringOptimized(buf, b[pos+2:pos+2+slen])
		return buf, pos + 2 + slen

	case FieldTypeBytes:
		if len(b)-pos < 2 {
			return append(buf, '?'), pos + 2
		}
		blen := int(uint16(b[pos])<<8 | uint16(b[pos+1]))
		if len(b)-pos < 2+blen {
			return append(buf, '?'), pos + 2 + blen
		}
		// Format as hex
		buf = appendHex(buf, b[pos+2:pos+2+blen])
		return buf, pos + 2 + blen

	default:
		return append(buf, '?'), pos
	}
}

// escapeStringOptimized escapes string without allocation
func escapeStringOptimized(buf []byte, s []byte) []byte {
	// Fast path - scan for special characters using optimized loop
	needsEscape := false
	hasSpace := false

	// Unroll loop for better performance
	i := 0
	for ; i+4 <= len(s); i += 4 {
		if s[i] == '"' || s[i] == '\\' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t' {
			needsEscape = true
			break
		}
		if s[i+1] == '"' || s[i+1] == '\\' || s[i+1] == '\n' || s[i+1] == '\r' || s[i+1] == '\t' {
			needsEscape = true
			break
		}
		if s[i+2] == '"' || s[i+2] == '\\' || s[i+2] == '\n' || s[i+2] == '\r' || s[i+2] == '\t' {
			needsEscape = true
			break
		}
		if s[i+3] == '"' || s[i+3] == '\\' || s[i+3] == '\n' || s[i+3] == '\r' || s[i+3] == '\t' {
			needsEscape = true
			break
		}
		if s[i] == ' ' || s[i+1] == ' ' || s[i+2] == ' ' || s[i+3] == ' ' {
			hasSpace = true
		}
	}

	// Check remaining bytes
	if !needsEscape {
		for ; i < len(s); i++ {
			if s[i] == '"' || s[i] == '\\' || s[i] == '\n' || s[i] == '\r' || s[i] == '\t' {
				needsEscape = true
				break
			}
			if s[i] == ' ' {
				hasSpace = true
			}
		}
	}

	// If only spaces, just quote it
	if !needsEscape && hasSpace {
		buf = append(buf, '"')
		buf = append(buf, s...)
		return append(buf, '"')
	}

	if !needsEscape {
		return append(buf, s...)
	}

	// Escape with quotes
	buf = append(buf, '"')

	// Reserve space to avoid multiple allocations
	if cap(buf)-len(buf) < len(s)*2 {
		newBuf := make([]byte, len(buf), len(buf)+len(s)*2)
		copy(newBuf, buf)
		buf = newBuf
	}

	for _, b := range s {
		switch b {
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
			buf = append(buf, b)
		}
	}
	return append(buf, '"')
}

// escapeString escapes a string for terminal output (kept for compatibility)
func escapeString(s string) string {
	var buf []byte
	// Use zero-copy string to bytes conversion
	return BytesToString(escapeStringOptimized(buf, StringToBytes(s)))
}

// Fast integer to string conversion without allocation
func appendInt(buf []byte, v int64) []byte {
	if v < 0 {
		buf = append(buf, '-')
		v = -v
	}
	return appendUint(buf, uint64(v))
}

func appendUint(buf []byte, v uint64) []byte {
	if v == 0 {
		return append(buf, '0')
	}

	// Use a small buffer on stack
	var tmp [20]byte
	i := len(tmp)

	for v > 0 {
		i--
		tmp[i] = byte(v%10) + '0'
		v /= 10
	}

	return append(buf, tmp[i:]...)
}

// Simple float formatting
func appendFloat32(buf []byte, f float32) []byte {
	return appendFloat64(buf, float64(f))
}

func appendFloat64(buf []byte, f float64) []byte {
	// Simple implementation - just 3 decimal places
	if f < 0 {
		buf = append(buf, '-')
		f = -f
	}

	// Integer part
	intPart := uint64(f)
	buf = appendUint(buf, intPart)

	// Decimal part
	buf = append(buf, '.')
	f -= float64(intPart)
	f *= 1000
	fracPart := uint64(f + 0.5) // Round

	// Ensure 3 digits
	if fracPart < 100 {
		buf = append(buf, '0')
		if fracPart < 10 {
			buf = append(buf, '0')
		}
	}
	return appendUint(buf, fracPart)
}

// Fast hex encoding
func appendHex(buf []byte, data []byte) []byte {
	const hex = "0123456789abcdef"
	for _, b := range data {
		buf = append(buf, hex[b>>4], hex[b&0xf])
	}
	return buf
}

// getLevelColor returns the color for a log level (kept for compatibility)
func (w *TerminalWriter) getLevelColor(level Level) string {
	if level < 5 {
		return string(levelColors[level])
	}
	return ""
}

// getLevelString returns the string representation of a level (kept for compatibility)
func (w *TerminalWriter) getLevelString(level Level) string {
	if level < 5 {
		return string(levelStrings[level])
	}
	return string(levelStrings[5])
}

// fieldValueSize returns the size of a field value in bytes (kept for compatibility)
func (w *TerminalWriter) fieldValueSize(b []byte, fieldType FieldType) int {
	switch fieldType {
	case FieldTypeInt, FieldTypeUint, FieldTypeBool, FieldTypeFloat64:
		return 8
	case FieldTypeFloat32:
		return 4
	case FieldTypeString, FieldTypeBytes:
		if len(b) >= 2 {
			return 2 + int(uint16(b[0])<<8|uint16(b[1]))
		}
	}
	return 0
}

// decodeFieldValue with string return (kept for compatibility with tests)
func (w *TerminalWriter) decodeFieldValue(b []byte, fieldType FieldType) string {
	var buf []byte
	result, _ := w.decodeFieldValueBuf(buf, b, 0, fieldType)
	return string(result)
}

// Convenience functions for creating terminal writers

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
	defer w.mu.Unlock()
	w.useColor = enabled
}

// IsColorEnabled returns whether colors are enabled
func (w *TerminalWriter) IsColorEnabled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.useColor
}

// IsTerminal checks if the given writer is a terminal
func IsTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return isTerminal(f.Fd())
	}
	return false
}
