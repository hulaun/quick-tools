//go:build windows

// Package winapi wraps the handful of Win32 calls quick-tools needs.
//
// Everything here is pure syscall against the system DLLs -- no cgo, so the
// project builds with nothing but the Go toolchain installed.
package winapi

import "syscall"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterHotKey    = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey  = user32.NewProc("UnregisterHotKey")
	procGetForegroundWin  = user32.NewProc("GetForegroundWindow")
	procSetForegroundWin  = user32.NewProc("SetForegroundWindow")
	procAttachThreadInput = user32.NewProc("AttachThreadInput")
	procGetWindowThreadPI = user32.NewProc("GetWindowThreadProcessId")
	procOpenClipboard     = user32.NewProc("OpenClipboard")
	procCloseClipboard    = user32.NewProc("CloseClipboard")
	procEmptyClipboard    = user32.NewProc("EmptyClipboard")
	procGetClipboardData  = user32.NewProc("GetClipboardData")
	procSetClipboardData  = user32.NewProc("SetClipboardData")
	procIsFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procSendInput         = user32.NewProc("SendInput")
	procGetAsyncKeyState  = user32.NewProc("GetAsyncKeyState")

	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")
	procGlobalAlloc        = kernel32.NewProc("GlobalAlloc")
	procGlobalFree         = kernel32.NewProc("GlobalFree")
	procGlobalLock         = kernel32.NewProc("GlobalLock")
	procGlobalUnlock       = kernel32.NewProc("GlobalUnlock")
)

// Modifier flags for RegisterHotKey.
const (
	ModAlt      = 0x0001
	ModControl  = 0x0002
	ModShift    = 0x0004
	ModWin      = 0x0008
	ModNoRepeat = 0x4000
)

// Virtual key codes we care about.
const (
	VKShift   = 0x10
	VKControl = 0x11
	VKMenu    = 0x12 // Alt
	VKSpace   = 0x20
	VKLWin    = 0x5B
	VKRWin    = 0x5C
	VKV       = 0x56
	VKZ       = 0x5A
)

const (
	wmHotkey       = 0x0312
	cfUnicodeText  = 13
	gmemMoveable   = 0x0002
	inputKeyboard  = 1
	keyeventfKeyUp = 0x0002
)

// currentThreadID returns the calling thread's Win32 thread id.
func currentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadId.Call()
	return uint32(id)
}
