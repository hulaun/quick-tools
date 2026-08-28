//go:build windows

package winapi

import (
	"os"
	"syscall"
)

var (
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
	procGetStdHandle  = kernel32.NewProc("GetStdHandle")
)

const (
	attachParentProcess = ^uintptr(0)  // (DWORD)-1
	stdOutputHandle     = ^uintptr(10) // (DWORD)-11
	stdErrorHandle      = ^uintptr(11) // (DWORD)-12
)

// AttachConsole reconnects stdout and stderr to the terminal that launched us.
//
// A release build is linked with -H windowsgui, which marks the executable as a
// GUI application. Windows then gives it no console at all, so anything printed
// by a command-line flag such as -test-scripts goes nowhere -- the command
// appears to do nothing and exit.
//
// Attaching to the parent's console fixes that when run from a terminal. When
// there is no parent console -- launched from Explorer, say -- allocate one, so
// output is still visible rather than silently discarded.
//
// Returns false if no console could be obtained, in which case printing is
// pointless and the caller may prefer a message box.
func AttachConsole() bool {
	attached := false
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r != 0 {
		attached = true
	} else if r, _, _ := procAllocConsole.Call(); r != 0 {
		attached = true
	}
	if !attached {
		return false
	}

	// Go's os.Stdout was bound at startup to handles that do not exist in a GUI
	// process. They have to be rebound to the console we just acquired.
	if h, _, _ := procGetStdHandle.Call(stdOutputHandle); h != 0 && h != uintptr(syscall.InvalidHandle) {
		os.Stdout = os.NewFile(uintptr(syscall.Handle(h)), "/dev/stdout")
	}
	if h, _, _ := procGetStdHandle.Call(stdErrorHandle); h != 0 && h != uintptr(syscall.InvalidHandle) {
		os.Stderr = os.NewFile(uintptr(syscall.Handle(h)), "/dev/stderr")
	}
	return true
}
