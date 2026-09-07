//go:build windows

package winapi

import (
	"time"
	"unicode/utf16"
)

// Replaying a macro.
//
// SendPaste next door is the one-chord version of this and was written first;
// everything below is the same idea generalised, with two things it did not
// need. Literal text goes in as Unicode code units rather than as keys, and the
// extended-key flag is set for the keys that have one.

const (
	keyeventfExtended = 0x0001
	keyeventfUnicode  = 0x0004
)

// KeyStep is one thing to send: some literal text, or one chord.
//
// The field layout matches macro.Step deliberately, and the modifier bits are
// the same MOD_* values ModAlt and friends already use, so the UI can hand one
// straight over without a conversion table in the middle.
type KeyStep struct {
	Text string
	Mods uint32
	VK   uint16
}

// SendKeys replays a sequence into whatever window currently has focus.
//
// gap is the pause between steps. Zero is allowed and is usually a mistake:
// some applications process a burst of synthesised keys inside one turn of
// their message pump and end up applying the navigation and the typing out of
// order.
//
// It blocks for len(steps)*gap, so it belongs on a goroutine rather than on the
// UI thread -- which is also fine for SendInput, which targets the foreground
// window rather than one of ours.
func SendKeys(steps []KeyStep, gap time.Duration) {
	if len(steps) == 0 {
		return
	}

	// The user is very likely still holding the modifiers from the hotkey or the
	// keypress that started this. Sending Home on top of a held Ctrl+Alt is not
	// Home. Same reasoning, same fix, as SendPaste.
	releaseHeldModifiers()
	time.Sleep(15 * time.Millisecond)

	for i, s := range steps {
		if i > 0 && gap > 0 {
			time.Sleep(gap)
		}
		if s.Text != "" {
			sendText(s.Text)
			continue
		}
		sendChord(s.Mods, s.VK)
	}
}

// SendUndo presses Ctrl+Z n times, which is what putting a macro's edit back
// takes when the macro made more than one change.
func SendUndo(n int, gap time.Duration) {
	if n <= 0 {
		return
	}
	steps := make([]KeyStep, n)
	for i := range steps {
		steps[i] = KeyStep{Mods: ModControl, VK: VKZ}
	}
	SendKeys(steps, gap)
}

// releaseHeldModifiers lifts every modifier the user is physically holding.
func releaseHeldModifiers() {
	var release []keyInput
	for _, vk := range []uint16{VKControl, VKMenu, VKShift, VKLWin, VKRWin} {
		if keyIsDown(vk) {
			release = append(release, keyEvent(vk, true))
		}
	}
	sendInputs(release)
}

// modifierKeys is the order modifiers are pressed in. They come up in reverse,
// which is what a person's hands do and what applications watching for a
// modifier release expect.
var modifierKeys = []struct {
	bit uint32
	vk  uint16
}{
	{ModControl, VKControl},
	{ModShift, VKShift},
	{ModAlt, VKMenu},
	{ModWin, VKLWin},
}

func sendChord(mods uint32, vk uint16) {
	var in []keyInput
	for _, m := range modifierKeys {
		if mods&m.bit != 0 {
			in = append(in, keyEvent(m.vk, false))
		}
	}
	in = append(in, extendedKeyEvent(vk, false), extendedKeyEvent(vk, true))
	for i := len(modifierKeys) - 1; i >= 0; i-- {
		if mods&modifierKeys[i].bit != 0 {
			in = append(in, keyEvent(modifierKeys[i].vk, true))
		}
	}
	sendInputs(in)
}

// sendText types characters rather than pressing keys.
//
// KEYEVENTF_UNICODE takes the character in the scan code field and ignores the
// virtual key entirely, which is what makes it layout-independent: a macro that
// recorded a quotation mark types a quotation mark whatever the keyboard is set
// to, without having to know which key produces one.
func sendText(text string) {
	units := utf16.Encode([]rune(text))
	in := make([]keyInput, 0, len(units)*2)
	for _, u := range units {
		in = append(in,
			keyInput{typ: inputKeyboard, wScan: u, dwFlags: keyeventfUnicode},
			keyInput{typ: inputKeyboard, wScan: u, dwFlags: keyeventfUnicode | keyeventfKeyUp},
		)
	}
	sendInputs(in)
}

// extendedKeyEvent is keyEvent with the extended-key flag set where it belongs.
//
// The navigation block -- the arrows, Home, End, the page keys, Insert and
// Delete -- shares virtual key codes with the numeric keypad, and the extended
// flag is the only thing that tells them apart. A well-behaved Win32
// application reading the virtual key does not care, but anything reading scan
// codes does, and that includes terminals, remote desktop clients and games.
// Setting it costs nothing and removes a class of "the macro works everywhere
// except in X".
func extendedKeyEvent(vk uint16, up bool) keyInput {
	in := keyEvent(vk, up)
	if isExtendedVK(vk) {
		in.dwFlags |= keyeventfExtended
	}
	return in
}

func isExtendedVK(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, // PageUp, PageDown, End, Home
		0x25, 0x26, 0x27, 0x28, // Left, Up, Right, Down
		0x2d, 0x2e, // Insert, Delete
		0x5b, 0x5c, 0x5d, // LWin, RWin, Apps
		0x90, 0xa3, 0xa5: // NumLock, RControl, RAlt
		return true
	}
	return false
}
