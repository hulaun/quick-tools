//go:build windows

package winapi

import "fmt"

// RegisterHotkeyFor binds a system-wide hotkey to a window rather than to the
// calling thread's message queue.
//
// The real app uses this instead of HotkeyLoop: the GUI framework already owns
// a message loop, so routing WM_HOTKEY to the palette window lets that single
// loop dispatch it. HotkeyLoop stays for the cmd/m0 spike, which has no window.
//
// The window must be created before calling this, and the call must happen on
// the thread that owns the window.
func RegisterHotkeyFor(hwnd uintptr, id int, modifiers, vk uint32) error {
	r, _, err := procRegisterHotKey.Call(hwnd, uintptr(id), uintptr(modifiers|ModNoRepeat), uintptr(vk))
	if r == 0 {
		return fmt.Errorf("winapi: RegisterHotKey failed -- another application probably owns this combination: %w", err)
	}
	return nil
}

// UnregisterHotkeyFor releases a hotkey registered with RegisterHotkeyFor.
func UnregisterHotkeyFor(hwnd uintptr, id int) {
	procUnregisterHotKey.Call(hwnd, uintptr(id))
}

// WMHotkey is the message id delivered when a registered hotkey fires, exported
// so the UI layer can hook it without importing raw Win32 constants.
const WMHotkey = wmHotkey
