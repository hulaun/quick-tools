//go:build windows

package winapi

var (
	procSetCapture     = user32.NewProc("SetCapture")
	procReleaseCapture = user32.NewProc("ReleaseCapture")
)

// SetCapture routes every mouse message to one window until ReleaseCapture,
// even when the pointer leaves it.
//
// Dragging the overlay scrollbar needs it for the same reason the real one
// does: the pointer routinely wanders off the thumb, and off the control
// entirely, while the button is still down. Without capture the drag stops the
// moment it leaves the six pixels it started on.
func SetCapture(hwnd uintptr) uintptr {
	prev, _, _ := procSetCapture.Call(hwnd)
	return prev
}

// ReleaseCapture hands mouse messages back to whichever window is under the
// pointer.
func ReleaseCapture() bool {
	r, _, _ := procReleaseCapture.Call()
	return r != 0
}
