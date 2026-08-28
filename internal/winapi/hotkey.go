//go:build windows

package winapi

import (
	"fmt"
	"runtime"
	"unsafe"
)

// HotkeyLoop registers a system-wide hotkey and calls fn each time it fires.
//
// It blocks. The caller must be on a locked OS thread: RegisterHotKey delivers
// WM_HOTKEY to the *thread* that registered it, and an unpinned goroutine can
// be migrated to another thread by the Go runtime -- which is the usual cause of
// a hotkey that "works sometimes".
func HotkeyLoop(id int, modifiers, vk uint32, fn func()) error {
	if !isThreadLocked() {
		return fmt.Errorf("winapi: HotkeyLoop requires runtime.LockOSThread")
	}

	r, _, err := procRegisterHotKey.Call(0, uintptr(id), uintptr(modifiers|ModNoRepeat), uintptr(vk))
	if r == 0 {
		return fmt.Errorf("winapi: RegisterHotKey failed (already taken by another app?): %w", err)
	}
	defer procUnregisterHotKey.Call(0, uintptr(id))

	var m msg
	for {
		r, _, err := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1:
			return fmt.Errorf("winapi: GetMessage failed: %w", err)
		case 0:
			return nil // WM_QUIT
		}
		if m.message == wmHotkey && int(m.wParam) == id {
			fn()
		}
	}
}

// Quit posts WM_QUIT, ending HotkeyLoop.
func Quit() { procPostQuitMessage.Call(0) }

var threadLocked bool

// LockThread pins the calling goroutine and records that we did so.
func LockThread() {
	runtime.LockOSThread()
	threadLocked = true
}

func isThreadLocked() bool { return threadLocked }
