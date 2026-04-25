# zlog

[![Go Reference](https://pkg.go.dev/badge/github.com/semihalev/zlog/v2.svg)](https://pkg.go.dev/github.com/semihalev/zlog/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/semihalev/zlog)](https://goreportcard.com/report/github.com/semihalev/zlog)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A small, very fast, **truly zero-allocation** structured logger for Go.

Apple M5, Go 1.23+:

```
BenchmarkUltimateLogger              15.9 ns/op    0 B/op   0 allocs/op
BenchmarkUltimateLoggerParallel       5.2 ns/op    0 B/op   0 allocs/op
BenchmarkStructuredLogger            35.6 ns/op    0 B/op   0 allocs/op
BenchmarkStructuredLoggerParallel    10.5 ns/op    0 B/op   0 allocs/op
BenchmarkStructured + 5 fields       45.2 ns/op    0 B/op   0 allocs/op
BenchmarkStructured + 10 fields      76.7 ns/op    0 B/op   0 allocs/op
BenchmarkRealWorld → TerminalWriter  26.5 ns/op    0 B/op   0 allocs/op
BenchmarkDisabledDebug                0.23 ns/op   0 B/op   0 allocs/op
```

The interesting number is the last column on every row: every logging path is genuinely 0 allocs/op once the buffer pool is warm, including the colored terminal output. Parallel benchmarks are *faster* than serial because each `GOMAXPROCS` slot keeps its own pool localcache and there's no contention on a sequence counter.

## Install

```bash
go get github.com/semihalev/zlog/v2
```

Requires Go 1.23+.

## Quick start

```go
package main

import "github.com/semihalev/zlog/v2"

func main() {
    log := zlog.NewStructured()
    log.SetWriter(zlog.StdoutTerminal())

    log.Info("server started",
        zlog.String("addr", ":8080"),
        zlog.Int("workers", 4),
    )
    log.Warn("slow query",
        zlog.String("table", "users"),
        zlog.Float64("duration_ms", 327.4),
    )
    log.Error("db connection lost",
        zlog.String("err", "i/o timeout"),
        zlog.Int("retry", 3),
    )
}
```

```
INFO  [04-25|15:51:30] server started                          addr=:8080 workers=4
WARN  [04-25|15:51:30] slow query                              table=users duration_ms=327.4
ERROR [04-25|15:51:30] db connection lost                      err="i/o timeout" retry=3
```

## Logger types

| Type | Purpose | Cost |
|---|---|---|
| `zlog.NewUltimateLogger()` | Bare-message hot path. No fields. | ~16 ns/op serial, ~5 ns/op parallel |
| `zlog.NewStructured()` | Typed fields, the recommended API. | ~36 ns/op + ~10 ns per extra field |
| `zlog.New()` | Plain `Logger`. Same shape as Ultimate. | ~21 ns/op |

For most apps, `NewStructured()` is the right choice: zero alloc, typed fields, ~26ns end-to-end when the writer is a `TerminalWriter` (that path is direct-text, no binary intermediate).

## Fields

```go
log.Info("event",
    zlog.String("name", "Alice"),
    zlog.Int("age", 30),
    zlog.Int64("id", 123456789),
    zlog.Uint("count", 42),
    zlog.Uint64("total", 9999999),
    zlog.Float32("score", 98.5),
    zlog.Float64("pi", 3.14159),
    zlog.Bool("active", true),
    zlog.Bytes("data", []byte{0x01, 0x02, 0x03}),
)
```

All field constructors are inlinable and allocation-free.

The global helpers (`zlog.Info`, `zlog.Warn`, ...) use the same typed `Field`
API and stay allocation-free:

```go
zlog.Info("user logged in", zlog.String("username", "alice"), zlog.Int("user_id", 12345))
```

There's also an explicit key/value compatibility API that's friendlier when you
have `any` values:

```go
zlog.InfoKV("user logged in", "username", "alice", "user_id", 12345)
```

## Writers

- `zlog.StdoutTerminal()` / `zlog.StderrTerminal()` — colored, padded, human-readable terminal output. The structured logger detects this writer and formats text directly into a pooled buffer (no binary intermediate).
- `zlog.StdoutWriter()` / `zlog.StderrWriter()` — raw binary output to stdout/stderr.
- `zlog.NewLogfmtWriter(io.Writer)` — `key=value` text format on top of the binary log.
- `zlog.NewMMapWriter(path, size)` — memory-mapped file with no per-write syscall (Linux/macOS/Windows).
- `zlog.NewAsyncWriter(io.Writer, bufSize)` — lock-free ring buffer with worker drains; for fan-out at high throughput.
- Any `io.Writer` works. The package writes a compact binary record; the terminal/logfmt writers are the supported decoders.

```go
mmap, _ := zlog.NewMMapWriter("/var/log/app.log", 100*1024*1024) // 100 MB
defer mmap.Close()

log := zlog.NewStructured()
log.SetWriter(mmap)
```

## Levels

```go
log := zlog.NewStructured()
log.SetLevel(zlog.LevelWarn) // Only Warn / Error / Fatal are emitted.

if log.GetLevel() <= zlog.LevelDebug {
    // expensive debug computation only when needed
}
```

A disabled level call is ~0.23 ns/op (a single atomic load + compare).

## Terminal output

```
DEBUG [01-02|15:04:05] starting up
INFO  [01-02|15:04:05] server initialized
WARN  [01-02|15:04:05] config not found, using defaults
ERROR [01-02|15:04:05] db connection failed                    error=timeout retry=3
```

Colors:

- DEBUG → cyan
- INFO → green
- WARN → yellow
- ERROR → red
- FATAL → magenta

Color is auto-detected from the underlying `*os.File` (TTY check). It also respects `NO_COLOR` and `TERM=dumb`. To force-disable in code:

```go
tw := zlog.NewTerminalWriter(os.Stdout).(*zlog.TerminalWriter)
tw.SetColorEnabled(false)
log.SetWriter(tw)
```

The terminal writer caches the formatted timestamp by Unix second, so `time.Time` decomposition runs once per second, not per log line.

### Windows

ANSI colors work on Windows 10 (build 14393+) automatically. On older Windows or when output isn't a TTY, plain text is used. Same env vars (`NO_COLOR`, `TERM=dumb`) apply.

## How it stays zero-alloc

A few of the things that matter:

- Every record is built into a `sync.Pool`-backed buffer indexed by a power-of-two size class. Allocation only happens when the pool is cold.
- The "small message → stack buffer" fast path that traditional loggers use was deliberately removed: passing a stack array to an `io.Writer.Write` interface call forces it onto the heap. The pool path is the actual zero-alloc path.
- Field encoding writes int/float values as a single `*(*uint64)(unsafe.Pointer(&buf[pos])) = ...` store in native byte order — one instruction per numeric field, not eight byte stores.
- The structured logger detects when its writer is a `*TerminalWriter` and formats text directly into the pooled buffer, skipping the binary encode → re-decode round-trip. Type assertion is ~1ns; the saving is ~30-40ns.
- Wall-clock timestamps are computed as `baseWallNs + (nanotime() - baseMonoNs)`, where `baseWallNs` and `baseMonoNs` are sampled once at `init()`. One VDSO call per log, ~5ns. Drifts from the kernel's wall clock if NTP adjusts the system time after init — fine for log timestamps.
- Escape-detection scans use a 256-byte classifier table (Go compiles byte-LUT loads to NEON `tbl` / SSSE3 `pshufb` on supported architectures).
- No `runtime.exit`, no panic-recovery in hot paths, no `defer` in the terminal writer's `Write`.

If you change a logger or writer and start seeing 1 alloc/op, run `go build -gcflags='-m=2'` and look for `escapes to heap` on the buffer — that's almost always an interface boundary somewhere.

## Binary log format

The binary record on disk / on the wire is:

```
0..3   magic "ZLOG"   (uint32, little-endian)
4      version        (1)
5      level          (1)
6..13  unix nanoseconds (uint64, native order)
14..15 msgLen         (uint16, native order)
16+    message bytes
       optional: 1-byte fieldCount, then fields
```

Each field is `keyLen(1) + key + type(1) + value`. Numeric values are 4 or 8 bytes native order. String / bytes values are `len(uint16, native) + payload`.

Native byte order is intentional: only this package's writers consume the binary form, so a `bswap` per field would buy nothing. The binary form is **not** stable across machines with different endianness — if you ship binary logs over the wire, decode on the producer's architecture.

## Testing

```bash
go test -race ./...
go test -bench=. -benchmem ./...
```

CI runs build + race + zero-alloc verification on Linux / macOS / Windows.

## License

MIT — see [LICENSE](LICENSE).
