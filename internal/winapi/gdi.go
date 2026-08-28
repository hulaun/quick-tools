//go:build windows

package winapi

import "syscall"

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
)

// CreateRoundRectRgn makes a rectangular region with rounded corners, returning
// a raw HRGN handle.
//
// windigo does not wrap this one. It is how a child control gets rounded
// corners at all: the DWM corner preference that rounds the main window applies
// only to top-level windows, so panels inside it have to be clipped to shape.
//
// The caller passes the handle to SetWindowRgn, which takes ownership -- the
// region must not be deleted afterwards.
func CreateRoundRectRgn(left, top, right, bottom, cornerW, cornerH int) uintptr {
	r, _, _ := procCreateRoundRectRgn.Call(
		uintptr(left), uintptr(top), uintptr(right), uintptr(bottom),
		uintptr(cornerW), uintptr(cornerH))
	return r
}

// UI state flags for WM_UPDATEUISTATE.
const (
	wmUpdateUIState = 0x0128
	uisSet          = 1
	uisfHideFocus   = 0x0001
)

// HideFocusRectangles stops a window and its children drawing the dotted focus
// rectangle around the focused item.
//
// Windows normally shows those only after the user navigates by keyboard, but a
// list view that is clicked draws one immediately, and on a dark themed row it
// reads as a stray dotted outline rather than as focus.
func HideFocusRectangles(hwnd uintptr) {
	procSendMessageW := user32.NewProc("SendMessageW")
	procSendMessageW.Call(hwnd, wmUpdateUIState, uintptr(uisSet|(uisfHideFocus<<16)), 0)
}
