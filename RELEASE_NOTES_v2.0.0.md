# Release v2.0.0

## 🚀 Major Performance Overhaul

This release represents a complete performance rewrite leveraging Go 1.23+ features to achieve unprecedented speed and true zero allocations.

## ⚡ Performance Highlights

- **2x faster than v1.x** - Reduced from ~40 ns/op to 21.88 ns/op
- **True zero allocations** - Direct writer operations now have 0 B/op
- **45.7 million logs/second** - Industry-leading throughput
- **Removed 64MB upfront allocation** - More memory efficient

## 🔄 Breaking Changes

- Removed redundant logger implementations:
  - `NanoLogger` - Use `UltimateLogger` instead
  - `SimpleLogger` - Use `Logger` instead  
  - `ZeroAllocLogger` - Use `UltimateLogger` instead
- Changed internal buffer pool implementation
- Minimum Go version is now 1.23

## ✨ New Features

### Generic Buffer Pool
- Type-safe buffer management with zero interface{} boxing
- Tiered allocation strategy (64B to 1MB+)
- Automatic size class detection

### Zero-Copy String Operations
- `StringToBytes()` - Convert strings without allocation
- `BytesToString()` - Convert bytes without allocation
- Uses `unsafe.StringData` from Go 1.20+

### Lock-Free Async Writer
- Generic ring buffer with `atomic.Pointer[T]`
- Multiple worker goroutines for parallel writes
- Reduced allocations from 542 B/op to 262 B/op

### Windows Color Support
- Automatic ANSI color detection for Windows 10+
- Virtual terminal processing enablement
- `NO_COLOR` environment variable support
- Manual color control via `SetColorEnabled()`

## 🐛 Bug Fixes

- Fixed terminal writer magic header mismatch
- Fixed buffer allocation issues for large messages
- Fixed string escaping performance with loop unrolling
- Fixed Windows terminal color escape sequences

## 📊 Benchmark Improvements

```
BenchmarkUltimateLogger-10       56M    21.88 ns/op    0 B/op    0 allocs/op
BenchmarkAsyncWriterDirect-10     8M   158.0 ns/op    0 B/op    0 allocs/op
BenchmarkZeroCopyString-10     2000M    0.497 ns/op    0 B/op    0 allocs/op
```

## 🔧 Migration Guide

### For NanoLogger users:
```go
// Old
logger := zlog.NewNanoLogger()

// New
logger := zlog.NewUltimateLogger()
```

### For Windows users seeing escape codes:
```go
// Option 1: Disable colors
tw := zlog.NewTerminalWriter(os.Stdout).(*zlog.TerminalWriter)
tw.SetColorEnabled(false)
logger.SetWriter(tw)

// Option 2: Use plain writer
logger.SetWriter(zlog.StdoutWriter())

// Option 3: Set environment variable
// SET NO_COLOR=1
```

## 🙏 Acknowledgments

Special thanks to @semihalev for identifying performance bottlenecks and requesting modern Go optimizations.

---

Full changelog: https://github.com/semihalev/zlog/compare/v1.2.5...v2.0.0