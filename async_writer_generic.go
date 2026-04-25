package zlog

import (
	"io"
	"runtime"
	"sync/atomic"
)

// rbSlot is a single ring-buffer cell carrying a value plus a
// sequence counter that doubles as a generation marker.
//
// Layout: seq is the readiness gate; val is the payload.
//   - For producers: a slot is ready to be filled when seq == head
//     (the index the producer is about to claim).
//   - For consumers: a slot is ready to be read when seq == tail+1
//     (one past the index the consumer is about to claim).
//
// After a Put at head=H, the producer sets seq=H+1 — that's both
// "this slot is filled for generation H" and "wait for the consumer
// to advance past H before overwriting."
//
// After a Get at tail=T, the consumer sets seq=T+size — that's
// "this slot has been consumed; it's ready for the producer's next
// generation (which writes at head = T+size)."
//
// Initial seq for slot i is i, so the very first Put at head=0 sees
// slot[0].seq=0 and may claim it.
type rbSlot[T any] struct {
	seq atomic.Uint64
	val atomic.Pointer[T]
}

// RingBuffer[T] is a lock-free MPMC ring buffer based on the LMAX
// Disruptor pattern (per-slot sequence numbers).
//
// head and tail are monotonic uint64 counters; only their `& mask`
// values index the buffer. The seq field on each slot encodes the
// generation, which lets producers and consumers coordinate without
// a mutex AND without the silent overwrite race that a "naive" CAS
// design has (where a producer's full-check passing immediately
// after a consumer claims the same slot lets the producer clobber
// the slot the consumer is about to read).
//
// All operations are non-blocking: Put returns false on full, Get
// returns (nil, false) on empty. There are no spin-waits.
type RingBuffer[T any] struct {
	_      [CacheLineSize]byte
	mask   uint64
	_      [56]byte
	head   atomic.Uint64
	_      [56]byte
	tail   atomic.Uint64
	_      [56]byte
	buffer []rbSlot[T]
	pool   *Pool[*T]
}

// NewRingBuffer creates a new generic ring buffer.
func NewRingBuffer[T any](size int, pool *Pool[*T]) *RingBuffer[T] {
	size = nextPowerOf2(size)
	rb := &RingBuffer[T]{
		buffer: make([]rbSlot[T], size),
		mask:   uint64(size - 1),
		pool:   pool,
	}
	// Seed each slot's seq with its index so the first Put sees
	// seq==head==0 on slot 0 and may claim it.
	for i := range rb.buffer {
		rb.buffer[i].seq.Store(uint64(i))
	}
	return rb
}

// Put adds an item to the ring buffer. Returns false if full.
//
//go:inline
func (rb *RingBuffer[T]) Put(item *T) bool {
	for {
		head := rb.head.Load()
		s := &rb.buffer[head&rb.mask]
		seq := s.seq.Load()

		switch d := int64(seq) - int64(head); {
		case d == 0:
			// Slot is ready for this generation. Try to claim.
			if rb.head.CompareAndSwap(head, head+1) {
				s.val.Store(item)
				s.seq.Store(head + 1) // publish to consumers
				return true
			}
			// Lost the race; another producer claimed this slot. Retry.
		case d < 0:
			// Slot still holds an unconsumed item from an earlier
			// generation. The buffer is full from this producer's
			// perspective; back off.
			return false
		default:
			// d > 0: another producer has already advanced past us.
			// Reload head and retry.
		}
	}
}

// Get retrieves an item from the ring buffer.
//
//go:inline
func (rb *RingBuffer[T]) Get() (*T, bool) {
	for {
		tail := rb.tail.Load()
		s := &rb.buffer[tail&rb.mask]
		seq := s.seq.Load()

		switch d := int64(seq) - int64(tail+1); {
		case d == 0:
			// Slot is published for this consumer's generation.
			if rb.tail.CompareAndSwap(tail, tail+1) {
				item := s.val.Load()
				s.val.Store(nil)
				s.seq.Store(tail + uint64(len(rb.buffer))) // ready for next producer gen
				return item, true
			}
			// Lost the race to another consumer. Retry.
		case d < 0:
			// Slot has not yet been published — either the buffer is
			// empty or a producer claimed but hasn't stored. Either
			// way, nothing for us right now.
			return nil, false
		default:
			// d > 0: another consumer raced ahead. Reload tail and retry.
		}
	}
}

// LogEntry represents a log entry with zero-copy data
type LogEntry struct {
	data []byte // Reference to original data
}

// AsyncWriterV2 is a modern async writer using a lock-free MPMC ring
// buffer. Multiple goroutines can call Write concurrently; multiple
// workers consume in parallel. No mutex on the hot path.
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

// Write adds data to the async writer. Safe for concurrent callers.
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
