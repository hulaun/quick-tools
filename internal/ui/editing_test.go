//go:build windows

package ui

import "testing"

// editChars is the guard against a key bound as a command also arriving as
// text. TranslateMessage queues the character before the keydown handler runs,
// so returning 0 from WM_KEYDOWN does not stop it: whatever the command key
// would type has to be swallowed here as well.
func TestEditCharsSwallowsCommandKeys(t *testing.T) {
	swallowed := map[uint16]string{
		0x09: "Tab -- chains a transform, enters a note, enters the action list",
		0x7f: "Ctrl+Backspace -- would insert DEL and show a box glyph",
		0x13: "Ctrl+S",
		0x01: "Ctrl+A",
	}
	for ch, why := range swallowed {
		if !editChars(ch) {
			t.Errorf("editChars(%#02x) = false, want true: %s", ch, why)
		}
	}
}

// Everything a query is actually made of has to reach the field. Space is on
// this list deliberately: it used to be the chain key, which cost the search
// box the ability to type a space without Shift held, and moving chaining to
// Tab is what gave it back.
func TestEditCharsLetsTextThrough(t *testing.T) {
	for _, ch := range []uint16{' ', 'a', 'Z', '0', '-', '_', '.', '/', '\\', ':', 0xE9} {
		if editChars(ch) {
			t.Errorf("editChars(%q) = true, want false -- it is a character, not a command", rune(ch))
		}
	}
}
