package zlog

import (
	"sync"
	"unsafe"
)

// Pool is a generic, type-safe object pool optimized for Go 1.23+
type Pool[T any] struct {
	pool sync.Pool
}

// NewPool creates a new generic pool
func NewPool[T any](new func() T) *Pool[T] {
	return &Pool[T]{
		pool: sync.Pool{
			New: func() any { return new() },
		},
	}
}

// Get retrieves an object from the pool
//
//go:inline
func (p *Pool[T]) Get() T {
	return p.pool.Get().(T)
}

// Put returns an object to the pool
//
//go:inline
func (p *Pool[T]) Put(v T) {
	p.pool.Put(v)
}

// BufferPool is a specialized pool for byte slices with zero-copy optimizations
type BufferPool struct {
	pools [20]*Pool[*[]byte] // Pools for different size classes
}

// NewBufferPool creates a new buffer pool with automatic size classes
func NewBufferPool() *BufferPool {
	bp := &BufferPool{}
	for i := range bp.pools {
		size := 1 << (i + 6) // 64, 128, 256, 512, 1KB, 2KB, 4KB, 8KB, 16KB, etc.
		bp.pools[i] = NewPool(func() *[]byte {
			b := make([]byte, 0, size)
			return &b
		})
	}
	return bp
}

// Get retrieves a buffer that can hold at least size bytes
//
//go:inline
func (bp *BufferPool) Get(size int) *[]byte {
	// Find the right size class using bit manipulation
	if size <= 0 {
		size = 64
	}

	// Use CLZ (count leading zeros) for fast size class calculation
	bits := 64 - leadingZeros64(uint64(size-1))
	idx := bits - 6
	if idx < 0 {
		idx = 0
	}
	if idx >= len(bp.pools) {
		// For very large buffers, allocate directly
		b := make([]byte, 0, size)
		return &b
	}

	buf := bp.pools[idx].Get()
	// Ensure the buffer has enough capacity
	if cap(*buf) < size {
		*buf = make([]byte, 0, size)
	}
	return buf
}

// Put returns a buffer to the pool
//
//go:inline
func (bp *BufferPool) Put(buf *[]byte) {
	// Reset buffer
	*buf = (*buf)[:0]

	cap := cap(*buf)
	if cap == 0 {
		return
	}

	// Find size class
	bits := 64 - leadingZeros64(uint64(cap-1))
	idx := bits - 6
	if idx >= 0 && idx < len(bp.pools) && cap == 1<<(idx+6) {
		bp.pools[idx].Put(buf)
	}
}

// leadingZeros64 counts leading zeros using compiler intrinsics
//
//go:inline
func leadingZeros64(x uint64) int {
	return len64tab[x>>58] +
		len64tab[(x>>52)&0x3f] +
		len64tab[(x>>46)&0x3f] +
		len64tab[(x>>40)&0x3f] +
		len64tab[(x>>34)&0x3f] +
		len64tab[(x>>28)&0x3f] +
		len64tab[(x>>22)&0x3f] +
		len64tab[(x>>16)&0x3f]
}

var len64tab = [64]int{
	64, 63, 62, 62, 61, 61, 61, 61, 60, 60, 60, 60, 60, 60, 60, 60,
	59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 59,
	58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58,
	58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58,
}

// Global buffer pool instance
var globalBufferPool = NewBufferPool()

// GetBuffer gets a buffer from the global pool
//
//go:inline
func GetBuffer(size int) *[]byte {
	return globalBufferPool.Get(size)
}

// PutBuffer returns a buffer to the global pool
//
//go:inline
func PutBuffer(buf *[]byte) {
	globalBufferPool.Put(buf)
}

// StringToBytes converts string to bytes without allocation using Go 1.20+ unsafe features
//
//go:inline
func StringToBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// BytesToString converts bytes to string without allocation
//
//go:inline
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(&b[0], len(b))
}
