//go:build !race

// This file enforces a strict 0-allocs/op contract on the structured
// hot path with very large records (70 KB messages + 70 KB string and
// bytes fields). The check is disabled under -race because the race
// detector triggers GC frequently enough to clear sync.Pool's
// localcache for the 1 MiB-class buffers these records require, which
// leaks ~1 alloc/op into the measurement window. That's a property of
// the test environment, not the production hot path: a non-race build
// keeps the pool warm and runs at 0 allocs/op as advertised.

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
