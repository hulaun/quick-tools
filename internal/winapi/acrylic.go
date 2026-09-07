//go:build windows

package winapi

import (
	"unsafe"
)

var procSetWindowCompositionAttribute = user32.NewProc("SetWindowCompositionAttribute")

// accentPolicy is ACCENT_POLICY.
type accentPolicy struct {
	State         uint32
	Flags         uint32
	GradientColor uint32 // AABBGGRR, not the COLORREF order
	AnimationID   uint32
}

// compositionAttribData is WINDOWCOMPOSITIONATTRIBDATA.
type compositionAttribData struct {
	Attrib uint32
	PvData unsafe.Pointer
	CbData uintptr
}

const (
	wcaAccentPolicy = 19
	// accentAcrylic is ACCENT_ENABLE_ACRYLICBLURBEHIND.
	accentAcrylic = 4
	accentDisable = 0
)

// SetAcrylic puts a real acrylic blur behind a window: the desktop underneath,
// blurred and tinted, showing wherever the window's own painting leaves the
// alpha byte at zero.
//
// This is `SetWindowCompositionAttribute`, which is undocumented, and it is here
// because the documented routes do not work for a window drawn with GDI:
//
//   - DWMWA_SYSTEMBACKDROP_TYPE is accepted and does nothing. The material is
//     drawn in the window frame, and a WS_POPUP with no caption has no frame.
//   - DwmExtendFrameIntoClientArea with margins of -1 gives it one, but what
//     that extends is the *legacy* Aero frame, and Windows stopped blurring it
//     in Windows 8. What comes back is a flat fill -- measured here as #545454,
//     identical whatever is behind the window, which is what gave the game away.
//   - Every Mica and acrylic window that ships with Windows 11 -- Explorer,
//     Notepad, Task Manager -- is XAML over DirectComposition, not GDI, and gets
//     the material through a route a GDI window has no access to.
//
// So this is the one that works, and it has been the one that works since
// Windows 10 1803. It is used widely enough to be stable in practice, but it is
// not contractual: if a future build breaks it, the fallback is a flat dark
// window, not a broken one -- the call fails, nothing is drawn behind, and the
// palette is opaque. Do not build anything on top of it that cannot degrade
// that way.
//
// tint is AABBGGRR, *not* a COLORREF: the alpha is the strength of the tint laid
// over the blur, and a zero alpha gets an unreadable, undertinted window rather
// than a clearer one.
func SetAcrylic(hwnd uintptr, tint uint32) bool {
	return setAccent(hwnd, accentAcrylic, tint)
}

// ClearAcrylic turns the effect off again, leaving an ordinary opaque window.
func ClearAcrylic(hwnd uintptr) bool {
	return setAccent(hwnd, accentDisable, 0)
}

func setAccent(hwnd uintptr, state, tint uint32) bool {
	if err := procSetWindowCompositionAttribute.Find(); err != nil {
		return false
	}
	policy := accentPolicy{State: state, GradientColor: tint}
	data := compositionAttribData{
		Attrib: wcaAccentPolicy,
		PvData: unsafe.Pointer(&policy),
		CbData: unsafe.Sizeof(policy),
	}
	r, _, _ := procSetWindowCompositionAttribute.Call(hwnd, uintptr(unsafe.Pointer(&data)))
	return r != 0
}
