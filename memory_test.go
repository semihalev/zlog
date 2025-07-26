package zlog

import (
	"runtime"
	"testing"
)

func TestNoLargeMemoryAllocation(t *testing.T) {
	// Get initial memory stats
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Create multiple UltimateLogger instances
	for i := 0; i < 10; i++ {
		logger := NewUltimateLogger()
		logger.SetWriter(DiscardWriter())
		logger.Info("test")
	}

	// Force GC to clean up any temporary allocations
	runtime.GC()

	// Get final memory stats
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Calculate memory growth (handle potential underflow)
	var growth int64
	if m2.Alloc >= m1.Alloc {
		growth = int64(m2.Alloc - m1.Alloc)
	} else {
		// Memory was freed, growth is negative
		growth = -int64(m1.Alloc - m2.Alloc)
	}

	// Should be much less than 64MB (allow some KB for normal allocations)
	if growth > 1024*1024 { // 1MB threshold
		t.Errorf("Memory grew by %d bytes, expected much less than 64MB", growth)
	}

	t.Logf("Memory growth: %d bytes (%.2f KB)", growth, float64(growth)/1024)
}
