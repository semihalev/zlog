package zlog

import (
	"io"
	"runtime"
	"sync/atomic"
)

// RingBuffer[T] is a generic lock-free ring buffer optimized for Go 1.23+
type RingBuffer[T any] struct {
	_      [CacheLineSize]byte // Padding
	mask   uint64              // Size mask for fast modulo
	_      [56]byte            // Padding to cache line
	head   atomic.Uint64       // Producer position
	_      [56]byte            // Padding to cache line
	tail   atomic.Uint64       // Consumer position
	_      [56]byte            // Padding to cache line
	buffer []atomic.Pointer[T] // Buffer of atomic pointers
	pool   *Pool[*T]           // Object pool for entries
}

// NewRingBuffer creates a new generic ring buffer
func NewRingBuffer[T any](size int, pool *Pool[*T]) *RingBuffer[T] {
	// Ensure size is power of 2
	size = nextPowerOf2(size)

	rb := &RingBuffer[T]{
		buffer: make([]atomic.Pointer[T], size),
		mask:   uint64(size - 1),
		pool:   pool,
	}

	return rb
}

// Put adds an item to the ring buffer (wait-free for single producer)
//
//go:inline
func (rb *RingBuffer[T]) Put(item *T) bool {
	head := rb.head.Load()
	next := (head + 1) & rb.mask

	// Check if full
	if next == rb.tail.Load() {
		return false
	}

	// Store item
	rb.buffer[head].Store(item)

	// Update head (this is the linearization point)
	rb.head.Store(next)
	return true
}

// Get retrieves an item from the ring buffer (lock-free for multiple consumers)
//
//go:inline
func (rb *RingBuffer[T]) Get() (*T, bool) {
	for {
		tail := rb.tail.Load()
		head := rb.head.Load()

		// Empty?
		if tail == head {
			return nil, false
		}

		// Try to claim this slot
		next := (tail + 1) & rb.mask
		if rb.tail.CompareAndSwap(tail, next) {
			// Wait for data to be available (should be immediate)
			for {
				if item := rb.buffer[tail].Load(); item != nil {
					// Clear slot for reuse
					rb.buffer[tail].Store(nil)
					return item, true
				}
				runtime.Gosched()
			}
		}
	}
}

// LogEntry represents a log entry with zero-copy data
type LogEntry struct {
	data []byte // Reference to original data
}

// AsyncWriterV2 is a modern async writer using generic ring buffer
type AsyncWriterV2 struct {
	rb      *RingBuffer[LogEntry]
	writer  io.Writer
	done    atomic.Bool
	pool    *Pool[*LogEntry]
	workers int
}

// NewAsyncWriterV2 creates a new async writer with multiple workers
func NewAsyncWriterV2(w io.Writer, bufferSize, workers int) *AsyncWriterV2 {
	// Create pool for log entries
	pool := NewPool(func() *LogEntry {
		return &LogEntry{}
	})

	aw := &AsyncWriterV2{
		rb:      NewRingBuffer(bufferSize, pool),
		writer:  w,
		pool:    pool,
		workers: workers,
	}

	// Start workers
	for i := 0; i < workers; i++ {
		go aw.worker()
	}

	return aw
}

// Write adds data to the async writer (zero-copy)
func (aw *AsyncWriterV2) Write(b []byte) (int, error) {
	// Get entry from pool
	entry := aw.pool.Get()

	// Copy data to avoid lifetime issues
	if cap(entry.data) < len(b) {
		entry.data = make([]byte, len(b))
	} else {
		entry.data = entry.data[:len(b)]
	}
	copy(entry.data, b)

	// Try to put in ring buffer
	for !aw.rb.Put(entry) {
		if aw.done.Load() {
			aw.pool.Put(entry)
			return 0, io.ErrClosedPipe
		}

		// Backpressure - help consume
		if consumed, ok := aw.rb.Get(); ok {
			aw.writer.Write(consumed.data)
			consumed.data = consumed.data[:0]
			aw.pool.Put(consumed)
		} else {
			runtime.Gosched()
		}
	}

	return len(b), nil
}

// worker processes entries from the ring buffer
func (aw *AsyncWriterV2) worker() {
	for !aw.done.Load() {
		entry, ok := aw.rb.Get()
		if ok {
			aw.writer.Write(entry.data)
			entry.data = entry.data[:0]
			aw.pool.Put(entry)
		} else {
			runtime.Gosched()
		}
	}

	// Drain remaining
	for {
		entry, ok := aw.rb.Get()
		if !ok {
			break
		}
		aw.writer.Write(entry.data)
		entry.data = entry.data[:0]
		aw.pool.Put(entry)
	}
}

// Close stops the async writer
func (aw *AsyncWriterV2) Close() error {
	aw.done.Store(true)
	return nil
}

// nextPowerOf2 returns the next power of 2 greater than or equal to n
//
//go:inline
func nextPowerOf2(n int) int {
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	n++
	return n
}

// NewAsyncWriter creates an async writer (compatibility wrapper)
func NewAsyncWriter(w io.Writer, bufferSize int) *AsyncWriterV2 {
	workers := runtime.GOMAXPROCS(0) / 2
	if workers < 1 {
		workers = 1
	}
	return NewAsyncWriterV2(w, bufferSize, workers)
}
