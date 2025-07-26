//go:build windows

package zlog

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	// Windows console mode flags
	ENABLE_VIRTUAL_TERMINAL_PROCESSING = 0x0004
	ENABLE_PROCESSED_OUTPUT            = 0x0001
)

var (
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode            = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode            = kernel32.NewProc("SetConsoleMode")
	procGetStdHandle              = kernel32.NewProc("GetStdHandle")
	virtualTerminalSupported      bool
	virtualTerminalSupportChecked bool
)

// isTerminal returns true if the file descriptor is a terminal and enables colors if supported
func isTerminal(fd uintptr) bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(fd, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false
	}

	// Try to enable virtual terminal processing for ANSI color support
	if !virtualTerminalSupportChecked {
		virtualTerminalSupportChecked = true
		// Try to enable virtual terminal processing
		newMode := mode | ENABLE_VIRTUAL_TERMINAL_PROCESSING | ENABLE_PROCESSED_OUTPUT
		r, _, _ = procSetConsoleMode.Call(fd, uintptr(newMode))
		if r != 0 {
			virtualTerminalSupported = true
		}
	}

	// On older Windows versions without virtual terminal support,
	// we should disable colors even if it's a terminal
	if !virtualTerminalSupported {
		// Check Windows version - Windows 10 build 14393 and later support VT
		if !checkWindowsVersion() {
			return false // Disable colors on older Windows
		}
	}

	return true
}

// checkWindowsVersion checks if Windows supports ANSI colors (Windows 10 1607+)
func checkWindowsVersion() bool {
	// Get Windows version
	dll := syscall.NewLazyDLL("ntdll.dll")
	proc := dll.NewProc("RtlGetVersion")

	type osversioninfoexw struct {
		dwOSVersionInfoSize uint32
		dwMajorVersion      uint32
		dwMinorVersion      uint32
		dwBuildNumber       uint32
		dwPlatformId        uint32
		szCSDVersion        [128]uint16
	}

	var info osversioninfoexw
	info.dwOSVersionInfoSize = uint32(unsafe.Sizeof(info))
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&info)))

	if ret == 0 {
		// Windows 10 is version 10.0, build 14393+ supports ANSI
		if info.dwMajorVersion > 10 || (info.dwMajorVersion == 10 && info.dwBuildNumber >= 14393) {
			return true
		}
	}

	// Fallback: check if TERM environment variable is set (Git Bash, WSL, etc.)
	if os.Getenv("TERM") != "" {
		return true
	}

	return false
}

// Alternative simple check for standard outputs
func isTerminalSimple(fd uintptr) bool {
	return fd == uintptr(os.Stdout.Fd()) || fd == uintptr(os.Stderr.Fd())
}
