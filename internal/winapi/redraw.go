//go:build windows

package winapi

var procRedrawWindow = user32.NewProc("RedrawWindow")

const (
	rdwInvalidate  = 0x0001
	rdwErase       = 0x0004
	rdwFrame       = 0x0400
	rdwAllChildren = 0x0080
	rdwUpdateNow   = 0x0100
)

// RedrawAll repaints a window and every child from scratch, background included.
//
// This exists for one reason: a window region applied while the window is
// hidden does not get the area it newly excludes repainted. `SetWindowRgn`'s
// redraw flag has nothing to redraw yet, and the paint that comes with
// `ShowWindow` does not cover it either -- so the corners a rounded region cuts
// off keep whatever was in the buffer, which is the control's own square
// background. The region is correct the whole time; `PtInRegion` agrees, and
// the control clips correctly everywhere it is asked to repaint afterwards.
// Only those first pixels are stale, and they are the only ones anybody looks
// at, so the corners appear not to have been rounded at all.
//
// One call after the first show fixes it for the life of the window.
func RedrawAll(hwnd uintptr) {
	// UPDATENOW rather than leaving the repaint queued: what this is fixing is
	// a paint that has already been missed once, and a second invalidation
	// sitting behind the one ShowWindow queued gets folded into it and misses
	// the same pixels for the same reason.
	procRedrawWindow.Call(hwnd, 0, 0,
		rdwInvalidate|rdwErase|rdwFrame|rdwAllChildren|rdwUpdateNow)
}
