package zlog

import (
	"io"
	"strings"
	"testing"
)

func TestLongStructuredRecordsZeroAllocWarm(t *testing.T) {
	longMsg := strings.Repeat("m", 70_000)
	longValue := strings.Repeat("v", 70_000)
	escapedValue := strings.Repeat("\"\\\n\r\t", 14_000)
	longBytes := make([]byte, 70_000)

	tests := []struct {
		name   string
		writer Writer
	}{
		{name: "binary", writer: io.Discard},
		{name: "logfmt", writer: NewLogfmtWriter(io.Discard)},
		{name: "terminal", writer: NewTerminalWriter(io.Discard)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewStructured()
			logger.SetWriter(tt.writer)

			allocs := testing.AllocsPerRun(10, func() {
				logger.Info(longMsg,
					String("long", longValue),
					String("escaped", escapedValue),
					Bytes("bytes", longBytes))
			})
			if allocs != 0 {
				t.Fatalf("allocs = %v, want 0", allocs)
			}
		})
	}
}
