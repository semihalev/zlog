package zlog

import (
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestFatal(t *testing.T) {
	if os.Getenv("BE_FATAL") == "1" {
		logger := New()
		logger.SetWriter(io.Discard)
		logger.Fatal("fatal error")
		return
	}

	// Run the test in a subprocess
	cmd := exec.Command(os.Args[0], "-test.run=TestFatal")
	cmd.Env = append(os.Environ(), "BE_FATAL=1")
	err := cmd.Run()

	// Check that it exited with status 1
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return // Expected behavior
	}

	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestStructuredFatal(t *testing.T) {
	if os.Getenv("BE_FATAL_STRUCTURED") == "1" {
		logger := NewStructured()
		logger.SetWriter(io.Discard)
		logger.Fatal("fatal error", String("key", "value"))
		return
	}

	// Run the test in a subprocess
	cmd := exec.Command(os.Args[0], "-test.run=TestStructuredFatal")
	cmd.Env = append(os.Environ(), "BE_FATAL_STRUCTURED=1")
	err := cmd.Run()

	// Check that it exited with status 1
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return // Expected behavior
	}

	t.Fatalf("process ran with err %v, want exit status 1", err)
}

func TestStructuredFatalExitsWhenFiltered(t *testing.T) {
	if os.Getenv("BE_FATAL_STRUCTURED_FILTERED") == "1" {
		logger := NewStructured()
		logger.SetWriter(io.Discard)
		logger.SetLevel(Level(255))
		logger.Fatal("filtered fatal", String("key", "value"))
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestStructuredFatalExitsWhenFiltered")
	cmd.Env = append(os.Environ(), "BE_FATAL_STRUCTURED_FILTERED=1")
	err := cmd.Run()

	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}

	t.Fatalf("process ran with err %v, want exit status 1", err)
}
