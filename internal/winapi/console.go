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
	procGetFileType   = kernel32.NewProc("GetFileType")
)

const (
	attachParentProcess = ^uintptr(0)  // (DWORD)-1
	stdOutputHandle     = ^uintptr(10) // (DWORD)-11
	stdErrorHandle      = ^uintptr(11) // (DWORD)-12

	fileTypeUnknown = 0
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
	// A terminal that is not a console -- mintty, which is what Git Bash uses --
	// hands the child an inherited *pipe* for stdout and has no console for
	// AttachConsole to find. Allocating one then throws that working pipe away
	// and prints into a window that vanishes when the process exits, which looks
	// exactly like the command having done nothing. So if the handles Go bound at
	// startup are already usable, leave them alone.
	if stdHandlesUsable() {
		return true
	}

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

// stdHandlesUsable reports whether stdout already goes somewhere -- a pipe, a
// file or a console. GetFileType is the test: a GUI process with no console
// gets a null or invalid handle, which comes back FILE_TYPE_UNKNOWN.
func stdHandlesUsable() bool {
	h, _, _ := procGetStdHandle.Call(stdOutputHandle)
	if h == 0 || h == uintptr(syscall.InvalidHandle) {
		return false
	}
	t, _, _ := procGetFileType.Call(h)
	return t != fileTypeUnknown
}
