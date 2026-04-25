//go:build !windows
// +build !windows

package zlog

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
)

// MMapWriter provides zero-copy, zero-syscall logging via memory-mapped files
type MMapWriter struct {
	file     *os.File
	data     []byte
	size     int64
	offset   atomic.Int64
	pageSize int64
}

// NewMMapWriter creates a new memory-mapped file writer
func NewMMapWriter(path string, size int64) (*MMapWriter, error) {
	// Create or open file
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}

	// Resize file
	if err := file.Truncate(size); err != nil {
		file.Close()
		return nil, err
	}

	// Memory map the file
	data, err := syscall.Mmap(int(file.Fd()), 0, int(size),
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, err
	}

	pageSize := int64(os.Getpagesize())

	return &MMapWriter{
		file:     file,
		data:     data,
		size:     size,
		pageSize: pageSize,
	}, nil
}

// Write writes data to the memory-mapped file. Reserves a contiguous
// region with a CAS loop on offset so concurrent writers can't race on
// wrap-around or overlap each other's slots.
func (w *MMapWriter) Write(b []byte) (int, error) {
	n := int64(len(b))
	if n == 0 {
		return 0, nil
	}
	if n > w.size {
		return 0, fmt.Errorf("zlog: mmap write of %d bytes exceeds region size %d", n, w.size)
	}

	var start, end int64
	for {
		cur := w.offset.Load()
		if cur+n > w.size {
			if w.offset.CompareAndSwap(cur, n) {
				start, end = 0, n
				break
			}
		} else {
			if w.offset.CompareAndSwap(cur, cur+n) {
				start, end = cur, cur+n
				break
			}
		}
	}

	copy(w.data[start:end], b)

	// MS_ASYNC schedules write-back without blocking, so the previous
	// goroutine-per-flush pattern was both racy and wasteful.
	startPage := start / w.pageSize
	endPage := end / w.pageSize
	if startPage != endPage {
		w.syncRange(startPage*w.pageSize, w.pageSize)
	}

	return len(b), nil
}

// syncRange schedules an async write-back of a range of memory.
func (w *MMapWriter) syncRange(offset, length int64) {
	if offset+length > w.size {
		length = w.size - offset
	}
	msync(w.data[offset:offset+length], MS_ASYNC)
}

// Close unmaps and closes the file
func (w *MMapWriter) Close() error {
	if err := syscall.Munmap(w.data); err != nil {
		return err
	}
	return w.file.Close()
}
