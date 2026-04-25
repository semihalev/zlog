package zlog

import (
	"io"
	"os"
	"testing"
)

// All three tests use the exact wiring root.init() sets up:
//   logger := NewStructured()
//   logger.SetWriter(StdoutTerminal())
// then call zlog.Info(...) for typed fields or zlog.InfoKV(...) for
// the any-typed key/value compatibility path.

func setDefaultDevNull(b *testing.B) func() {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Skip("no /dev/null")
	}
	original := Default()
	logger := NewStructured()
	logger.SetWriter(NewTerminalWriter(devnull))
	SetDefault(logger)
	return func() {
		SetDefault(original)
		devnull.Close()
	}
}

func setDefaultDiscard(_ *testing.B) func() {
	original := Default()
	logger := NewStructured()
	logger.SetWriter(NewTerminalWriter(io.Discard))
	SetDefault(logger)
	return func() { SetDefault(original) }
}

// Typed Field path (the recommended API): Info("msg", String("k","v"), Int("n",1))
func BenchmarkDefaultTyped_DevNull(b *testing.B) {
	defer setDefaultDevNull(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Info("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}

func BenchmarkDefaultTyped_Discard(b *testing.B) {
	defer setDefaultDiscard(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Info("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}

// Untyped KV path (the compatibility API): InfoKV("msg", "k", v, "n", n)
func BenchmarkDefaultKV_DevNull(b *testing.B) {
	defer setDefaultDevNull(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InfoKV("Server started", "addr", ":8080", "workers", 4, "ready", true)
	}
}

func BenchmarkDefaultKV_Discard(b *testing.B) {
	defer setDefaultDiscard(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InfoKV("Server started", "addr", ":8080", "workers", 4, "ready", true)
	}
}

// Bare message (no fields)
func BenchmarkDefaultBare_DevNull(b *testing.B) {
	defer setDefaultDevNull(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Info("Server started")
	}
}

func BenchmarkDefaultBare_Discard(b *testing.B) {
	defer setDefaultDiscard(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Info("Server started")
	}
}

// Calling Default() directly with typed Fields skips only the global atomic
// lookup; both paths are typed and allocation-free.
func BenchmarkDefaultViaInstance_DevNull(b *testing.B) {
	defer setDefaultDevNull(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Default().Info("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}

func BenchmarkDefaultViaInstance_Discard(b *testing.B) {
	defer setDefaultDiscard(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Default().Info("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}

// InfoF is the zero-allocation typed-Field path through the default
// logger added in v2.0.8. Should match the via-instance numbers because
// it forwards directly via ...Field with no boxing.
func BenchmarkDefaultInfoF_DevNull(b *testing.B) {
	defer setDefaultDevNull(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InfoF("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}

func BenchmarkDefaultInfoF_Discard(b *testing.B) {
	defer setDefaultDiscard(b)()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		InfoF("Server started", String("addr", ":8080"), Int("workers", 4), Bool("ready", true))
	}
}
