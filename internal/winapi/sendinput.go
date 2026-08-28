//go:build windows

package winapi

import (
	"time"
	"unsafe"
)

// keyInput mirrors Win32 INPUT with its KEYBDINPUT union member selected.
//
// The trailing pad matters: INPUT is sized by its largest union member
// (MOUSEINPUT), so the struct must be 40 bytes on amd64. SendInput rejects the
// call outright if cbSize does not match, and the failure is silent-looking.
type keyInput struct {
	typ         uint32
	_           uint32
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
	_           [8]byte
}

func keyEvent(vk uint16, up bool) keyInput {
	in := keyInput{typ: inputKeyboard, wVk: vk}
	if up {
		in.dwFlags = keyeventfKeyUp
	}
	return in
}

func sendInputs(in []keyInput) {
	if len(in) == 0 {
		return
	}
	procSendInput.Call(
		uintptr(len(in)),
		uintptr(unsafe.Pointer(&in[0])),
		unsafe.Sizeof(in[0]),
	)
}

// keyIsDown reports whether a key is physically held right now.
func keyIsDown(vk uint16) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return r&0x8000 != 0
}

// SendPaste synthesises Ctrl+V into whatever window currently has focus.
//
// The subtle part is releasing modifiers first. The user is very likely still
// holding Ctrl+Alt from the hotkey that got us here; if we simply add Ctrl+V on
// top, the target application receives Ctrl+Alt+V and does something else
// entirely -- or nothing. So we lift every modifier we find held, then press a
// clean Ctrl+V.
func SendPaste() {
	var release []keyInput
	for _, vk := range []uint16{VKControl, VKMenu, VKShift, VKLWin, VKRWin} {
		if keyIsDown(vk) {
			release = append(release, keyEvent(vk, true))
		}
	}
	sendInputs(release)

	// Give the target a moment to process the key-ups before the paste, or fast
	// applications can still see the modifiers as held.
	time.Sleep(15 * time.Millisecond)

	sendInputs([]keyInput{
		keyEvent(VKControl, false),
		keyEvent(VKV, false),
		keyEvent(VKV, true),
		keyEvent(VKControl, true),
	})
}
