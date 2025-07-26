package zlog

// Entry for compatibility with old tests
type Entry = LogEntry

// OldRingBuffer wraps the new generic RingBuffer for compatibility
type OldRingBuffer struct {
	rb   *RingBuffer[Entry]
	pool *Pool[*Entry]
}

// NewCompatRingBuffer creates a ring buffer (compatibility wrapper)
func NewCompatRingBuffer(size int) *OldRingBuffer {
	// Keep old behavior - panic if not power of 2
	if size&(size-1) != 0 {
		panic("ring buffer size must be power of 2")
	}
	
	pool := NewPool(func() *Entry {
		return &Entry{}
	})
	return &OldRingBuffer{
		rb:   NewRingBuffer[Entry](size, pool),
		pool: pool,
	}
}

// Put wrapper for old interface
func (orb *OldRingBuffer) Put(data []byte) bool {
	entry := orb.pool.Get()
	if cap(entry.data) < len(data) {
		entry.data = make([]byte, len(data))
	} else {
		entry.data = entry.data[:len(data)]
	}
	copy(entry.data, data)
	return orb.rb.Put(entry)
}

// Get wrapper for old interface  
func (orb *OldRingBuffer) Get() ([]byte, bool) {
	entry, ok := orb.rb.Get()
	if !ok {
		return nil, false
	}
	data := entry.data
	entry.data = entry.data[:0]
	orb.pool.Put(entry)
	return data, true
}