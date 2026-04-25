package zlog

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unsafe"
)

// TestBytesEmptyDoesNotPanic verifies that constructing a Bytes field
// from a nil or empty slice is safe. The previous implementation took
// &val[0] unconditionally, which panicked on len(val)==0.
func TestBytesEmptyDoesNotPanic(t *testing.T) {
	cases := []struct {
		name string
		val  []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Bytes panicked on %s slice: %v", tc.name, r)
				}
			}()

			var buf bytes.Buffer
			logger := NewStructured()
			logger.SetWriter(&buf)
			logger.Info("empty bytes", Bytes("data", tc.val))

			if buf.Len() == 0 {
				t.Fatal("expected output")
			}
		})
	}
}

func TestPlainLoggerLongMessageHeaderMatchesPayload(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetWriter(&buf)

	logger.Info(strings.Repeat("x", 70_000))

	b := buf.Bytes()
	if got, want := len(b), 16+65535; got != want {
		t.Fatalf("encoded length = %d, want %d", got, want)
	}
	msgLen := int(*(*uint16)(unsafe.Pointer(&b[14])))
	if msgLen != len(b)-16 {
		t.Fatalf("header msgLen = %d, payload length = %d", msgLen, len(b)-16)
	}
}

// TestLogfmtBinaryDecodeZeroAllocLong verifies that LogfmtWriter.Write
// (the binary-decode path) stays zero-alloc on long records. This path
// is hit when LogfmtWriter is wrapped behind another writer or fed
// pre-encoded binary records directly.
func TestLogfmtBinaryDecodeZeroAllocLong(t *testing.T) {
	// Encode a 50 KB record once via the structured logger writing into
	// a capture writer, then replay the captured bytes through
	// LogfmtWriter.Write to drive the binary-decode path.
	bigStr := strings.Repeat("x", 50_000)

	cap := &captureWriter{}
	source := NewStructured()
	source.SetWriter(cap)
	source.Info("payload", String("payload", bigStr))
	binData := append([]byte(nil), cap.last...)

	lf := NewLogfmtWriter(io.Discard)

	allocs := testing.AllocsPerRun(20, func() {
		if _, err := lf.Write(binData); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("LogfmtWriter.Write allocs = %v, want 0", allocs)
	}
}

// captureWriter records the most recent Write payload so tests can
// replay binary records through other writers.
type captureWriter struct {
	last []byte
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if cap(c.last) >= len(b) {
		c.last = c.last[:len(b)]
	} else {
		c.last = make([]byte, len(b))
	}
	copy(c.last, b)
	return len(b), nil
}

// TestKVDirectPathTerminal verifies that StructuredLogger.logKV detects
// a *TerminalWriter and produces correctly formatted text via the
// direct path. Output should contain "level [time] msg key=value\n".
func TestKVDirectPathTerminal(t *testing.T) {
	var underlying bytes.Buffer
	tw := NewTerminalWriter(&underlying)
	tw.SetColorEnabled(false)

	logger := NewStructured()
	logger.SetWriter(tw)
	logger.InfoKV("user logged in", "user", "alice", "age", 30)

	out := underlying.String()
	for _, want := range []string{"INFO ", "user logged in", "user=alice", "age=30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}

// TestKVDirectPathLogfmt verifies the same for *LogfmtWriter.
func TestKVDirectPathLogfmt(t *testing.T) {
	var underlying bytes.Buffer
	lf := NewLogfmtWriter(&underlying)

	logger := NewStructured()
	logger.SetWriter(lf)
	logger.InfoKV("login", "user", "alice", "age", 30)

	out := underlying.String()
	for _, want := range []string{"level=info", `msg=login`, "user=alice", "age=30"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
}
