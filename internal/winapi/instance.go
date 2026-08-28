//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

var (
	procCreateMutexW           = kernel32.NewProc("CreateMutexW")
	procReleaseMutex           = kernel32.NewProc("ReleaseMutex")
	procCloseHandle            = kernel32.NewProc("CloseHandle")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
)

const (
	errAlreadyExists = 183

	// hwndBroadcast delivers a message to every top-level window.
	hwndBroadcast = 0xffff

	// mutexName is per-session ("Local\") rather than machine-wide. Two users
	// logged in at once should each get their own palette, and a machine-wide
	// name would let the first one block the second.
	mutexName = "Local\\quick-tools-single-instance"
)

// ShowPaletteMessage is a process-independent message id, registered by name.
//
// Windows guarantees the same string yields the same id for every process in
// the session, and that no other application's id collides with it. That makes
// it safe to broadcast: only another quick-tools recognises it.
var ShowPaletteMessage = registerWindowMessage("quick-tools.show-palette")

func registerWindowMessage(name string) uint32 {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(id)
}

// SingleInstance is a held claim on being the only running copy.
type SingleInstance struct{ handle uintptr }

// AcquireSingleInstance tries to become the one running instance.
//
// It returns alreadyRunning=true if another copy holds the claim. The mutex is
// deliberately not released in that case -- the other process owns it.
func AcquireSingleInstance() (si *SingleInstance, alreadyRunning bool, err error) {
	name, err := syscall.UTF16PtrFromString(mutexName)
	if err != nil {
		return nil, false, err
	}
	h, _, lastErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, false, lastErr
	}
	// The handle is returned even when the mutex already existed, so the error
	// code is the only way to tell whether we are the first.
	if en, ok := lastErr.(syscall.Errno); ok && en == errAlreadyExists {
		procCloseHandle.Call(h)
		return nil, true, nil
	}
	return &SingleInstance{handle: h}, false, nil
}

// Release gives up the claim.
func (si *SingleInstance) Release() {
	if si == nil || si.handle == 0 {
		return
	}
	procReleaseMutex.Call(si.handle)
	procCloseHandle.Call(si.handle)
	si.handle = 0
}

// SignalExistingInstance asks the already-running copy to show its palette.
//
// Launching the app a second time is almost always someone trying to open it,
// not trying to run two of them -- so the second process hands the request over
// and exits, rather than dying silently with a hotkey conflict.
func SignalExistingInstance() {
	if ShowPaletteMessage == 0 {
		return
	}
	procPostMessageW.Call(hwndBroadcast, uintptr(ShowPaletteMessage), 0, 0)
}
