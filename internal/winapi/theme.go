//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

var (
	uxtheme = syscall.NewLazyDLL("uxtheme.dll")

	procSetWindowTheme = uxtheme.NewProc("SetWindowTheme")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
)

// dpiAwarenessPerMonitorV2 is the modern awareness mode: the process is told
// the real pixel dimensions and is notified when the user drags the window to a
// monitor with different scaling.
const dpiAwarenessPerMonitorV2 = ^uintptr(3) // -4 as an unsigned value

// SetDPIAware opts the process into real pixels.
//
// Without this, Windows renders the app at 96 DPI and then scales the result
// up, which makes every glyph soft on a high-DPI display. It must be called
// before any window is created.
//
// The modern call needs Windows 10 1703 or later; the fallback is the ancient
// system-DPI-aware one, which is still much better than nothing.
func SetDPIAware() {
	if err := procSetProcessDpiAwarenessContext.Find(); err == nil {
		if r, _, _ := procSetProcessDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2); r != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}

// SetWindowTheme applies a visual style class to a control.
//
// Passing "DarkMode_Explorer" is what turns a control's scrollbars dark; a list
// or edit box otherwise keeps bright white scrollbars however its background is
// coloured, because the scrollbar is drawn by the theme engine rather than by
// the control.
func SetWindowTheme(hwnd uintptr, class string) error {
	name, err := syscall.UTF16PtrFromString(class)
	if err != nil {
		return err
	}
	r, _, _ := procSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(name)), 0)
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}
