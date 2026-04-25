//go:build windows
// +build windows

package zlog

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// MMapWriter provides zero-copy, zero-syscall logging via memory-mapped files
type MMapWriter struct {
	file       *os.File
	data       []byte
	size       int64
	offset     atomic.Int64
	pageSize   int64
	mapHandle  syscall.Handle
	fileHandle syscall.Handle
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

	// Get file handle
	fileHandle := syscall.Handle(file.Fd())

	// Create file mapping
	mapHandle, err := syscall.CreateFileMapping(
		fileHandle,
		nil,
		syscall.PAGE_READWRITE,
		uint32(size>>32),
		uint32(size),
		nil,
	)
	if err != nil {
		file.Close()
		return nil, err
	}

	// Map view of file
	addr, err := syscall.MapViewOfFile(
		mapHandle,
		syscall.FILE_MAP_WRITE,
		0,
		0,
		uintptr(size),
	)
	if err != nil {
		syscall.CloseHandle(mapHandle)
		file.Close()
		return nil, err
	}

	// Create byte slice from mapped memory
	var data []byte
	header := (*[1 << 30]byte)(unsafe.Pointer(addr))
	data = header[:size:size]

	pageSize := int64(os.Getpagesize())

	return &MMapWriter{
		file:       file,
		data:       data,
		size:       size,
		pageSize:   pageSize,
		mapHandle:  mapHandle,
		fileHandle: fileHandle,
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

	// FlushViewOfFile is non-blocking; the prior goroutine-per-flush
	// pattern was both racy and wasteful.
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
	syscall.FlushViewOfFile(uintptr(unsafe.Pointer(&w.data[offset])), uintptr(length))
}

// Close unmaps and closes the file
func (w *MMapWriter) Close() error {
	// Unmap view
	if err := syscall.UnmapViewOfFile(uintptr(unsafe.Pointer(&w.data[0]))); err != nil {
		return err
	}
	// Close mapping handle
	if err := syscall.CloseHandle(w.mapHandle); err != nil {
		return err
	}
	return w.file.Close()
}
