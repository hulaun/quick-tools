//go:build windows

package winapi

// ForegroundWindow returns the window that currently has focus, so we can put
// focus back there after the palette closes.
func ForegroundWindow() uintptr {
	h, _, _ := procGetForegroundWin.Call()
	return h
}

// RestoreForeground gives focus back to hwnd.
//
// A bare SetForegroundWindow silently fails: Windows blocks focus stealing
// unless the calling thread already owns the foreground. Attaching our input
// queue to the target's thread for the duration lifts that restriction.
func RestoreForeground(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	targetThread, _, _ := procGetWindowThreadPI.Call(hwnd, 0)
	ourThread := uintptr(currentThreadID())

	if targetThread != 0 && targetThread != ourThread {
		procAttachThreadInput.Call(ourThread, targetThread, 1)
		defer procAttachThreadInput.Call(ourThread, targetThread, 0)
	}
	r, _, _ := procSetForegroundWin.Call(hwnd)
	return r != 0
}
