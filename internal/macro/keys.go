package macro

import (
	"fmt"
	"strings"
)

// Chord text, in both directions.
//
// winapi.ParseHotkey already parses one direction of this, and this is
// deliberately not that function. Two things differ, and both matter here:
//
//   - A hotkey must have a modifier, because registering a bare key system-wide
//     would swallow it in every application. A macro step is the opposite case:
//     "Home" and "Right" are the commonest steps there are.
//   - A macro has to render a chord back out again, for the pane and for
//     macros.json. A hotkey is only ever read.
//
// And this package stays free of Win32 so that it can be tested without a
// window, which rules out importing the one in winapi anyway.

// Virtual key codes this package names. Letters, digits and function keys are
// handled arithmetically rather than listed.
const (
	vkBackspace = 0x08
	vkTabKey    = 0x09
	vkEnter     = 0x0d
	vkEscape    = 0x1b
	vkSpace     = 0x20
	vkPageUp    = 0x21
	vkPageDown  = 0x22
	vkEnd       = 0x23
	vkHome      = 0x24
	vkLeft      = 0x25
	vkUp        = 0x26
	vkRight     = 0x27
	vkDown      = 0x28
	vkInsert    = 0x2d
	vkDel       = 0x2e
)

// keyNames is the canonical spelling of each named key -- what FormatChord
// writes. Parsing accepts these plus the aliases below.
var keyNames = map[uint16]string{
	vkBackspace: "Backspace", vkTabKey: "Tab", vkEnter: "Enter",
	vkEscape: "Escape", vkSpace: "Space",
	vkPageUp: "PageUp", vkPageDown: "PageDown", vkEnd: "End", vkHome: "Home",
	vkLeft: "Left", vkUp: "Up", vkRight: "Right", vkDown: "Down",
	vkInsert: "Insert", vkDel: "Delete",

	0xc0: "`", 0xbd: "-", 0xbb: "=", 0xdb: "[", 0xdd: "]",
	0xba: ";", 0xde: "'", 0xbc: ",", 0xbe: ".", 0xbf: "/", 0xdc: "\\",
}

// keyAliases are the extra spellings accepted on the way in, for a file
// someone types into by hand.
var keyAliases = map[string]uint16{
	"return": vkEnter, "esc": vkEscape, "del": vkDel, "ins": vkInsert,
	"pgup": vkPageUp, "pgdn": vkPageDown, "pagedn": vkPageDown,
	"backtick": 0xc0, "tilde": 0xc0, "minus": 0xbd, "equals": 0xbb,
	"comma": 0xbc, "period": 0xbe, "slash": 0xbf, "backslash": 0xdc,
	"semicolon": 0xba, "quote": 0xde,
}

var modAliases = map[string]uint32{
	"ctrl": ModCtrl, "control": ModCtrl,
	"alt": ModAlt, "shift": ModShift,
	"win": ModWin, "super": ModWin, "meta": ModWin, "cmd": ModWin,
}

// lowerNames is keyNames inverted, built once.
var lowerNames = func() map[string]uint16 {
	m := make(map[string]uint16, len(keyNames)+len(keyAliases))
	for vk, name := range keyNames {
		m[strings.ToLower(name)] = vk
	}
	for name, vk := range keyAliases {
		m[name] = vk
	}
	return m
}()

// ParseChord turns "Shift+Alt+Right" into its modifier mask and virtual key.
//
// Unlike a hotkey, a bare key is allowed and is the common case.
func ParseChord(s string) (mods uint32, vk uint16, err error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, 0, fmt.Errorf("empty key")
	}

	// "+" separates the parts, so it cannot also be spelled as a key here.
	// Nothing is lost: a plus sign is a character, and the recorder writes
	// characters as text steps, which do not go through this at all.
	parts := strings.Split(trimmed, "+")
	for i, raw := range parts {
		p := strings.ToLower(strings.TrimSpace(raw))
		last := i == len(parts)-1

		if p == "" {
			return 0, 0, fmt.Errorf("key %q has an empty part", s)
		}

		if !last {
			m, ok := modAliases[p]
			if !ok {
				return 0, 0, fmt.Errorf("key %q: %q is not a modifier (use Ctrl, Alt, Shift or Win)", s, raw)
			}
			mods |= m
			continue
		}

		key, kerr := parseKeyName(p)
		if kerr != nil {
			return 0, 0, fmt.Errorf("key %q: %w", s, kerr)
		}
		vk = key
	}
	return mods, vk, nil
}

func parseKeyName(p string) (uint16, error) {
	if vk, ok := lowerNames[p]; ok {
		return vk, nil
	}
	if len(p) == 1 {
		c := p[0]
		if c >= 'a' && c <= 'z' {
			return uint16(c-'a') + 0x41, nil // VK_A..VK_Z
		}
		if c >= '0' && c <= '9' {
			return uint16(c-'0') + 0x30, nil // VK_0..VK_9
		}
	}
	if len(p) >= 2 && p[0] == 'f' {
		n := 0
		for _, d := range p[1:] {
			if d < '0' || d > '9' {
				n = 0
				break
			}
			n = n*10 + int(d-'0')
		}
		if n >= 1 && n <= 24 {
			return uint16(0x70 + n - 1), nil // VK_F1..VK_F24
		}
	}
	return 0, fmt.Errorf("%q is not a key this build recognises", p)
}

// FormatChord is ParseChord's inverse: the canonical spelling of a chord.
//
// The modifier order is fixed -- Ctrl, Shift, Alt, Win -- so that two macros
// pressing the same combination read identically however they were recorded or
// typed, and so a diff of macros.json says something.
func FormatChord(mods uint32, vk uint16) string {
	var b strings.Builder
	for _, m := range []struct {
		bit  uint32
		name string
	}{
		{ModCtrl, "Ctrl"}, {ModShift, "Shift"}, {ModAlt, "Alt"}, {ModWin, "Win"},
	} {
		if mods&m.bit != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(keyName(vk))
	return b.String()
}

func keyName(vk uint16) string {
	if name, ok := keyNames[vk]; ok {
		return name
	}
	switch {
	case vk >= 0x41 && vk <= 0x5a: // A-Z
		return string(rune(vk))
	case vk >= 0x30 && vk <= 0x39: // 0-9
		return string(rune(vk))
	case vk >= 0x70 && vk <= 0x87: // F1-F24
		return fmt.Sprintf("F%d", vk-0x70+1)
	}
	// Nothing is gained by inventing a name for a key nobody named. The numeric
	// form still round-trips through ParseChord's failure into a visible error
	// rather than a silently wrong key.
	return fmt.Sprintf("VK%02X", vk)
}

// IsModifier reports whether a virtual key is one of the modifiers, in either
// its generic or its left/right form. The recorder folds these into the mask
// rather than emitting a step for each.
func IsModifier(vk uint16) bool {
	switch vk {
	case 0x10, 0x11, 0x12, // VK_SHIFT, VK_CONTROL, VK_MENU
		0xa0, 0xa1, // VK_LSHIFT, VK_RSHIFT
		0xa2, 0xa3, // VK_LCONTROL, VK_RCONTROL
		0xa4, 0xa5, // VK_LMENU, VK_RMENU
		0x5b, 0x5c: // VK_LWIN, VK_RWIN
		return true
	}
	return false
}

// Modifiers is a modifier mask being tracked from a stream of key events.
//
// Tracking rather than asking Windows is the point. GetAsyncKeyState answers
// about this instant, and a key event is handled some time after it happened --
// which is nearly always the same answer and occasionally, under load or across
// a fast chord, is not. Both the recorder and the undo watcher need the state
// as it was when the key went down, so both count it themselves.
type Modifiers uint32

// Track folds one modifier transition in. A key that is not a modifier leaves
// the mask alone, so the caller can pass every event through without checking.
func (m Modifiers) Track(vk uint16, down bool) Modifiers {
	bit := Modifiers(modBitFor(vk))
	if bit == 0 {
		return m
	}
	if down {
		return m | bit
	}
	return m &^ bit
}

// Mask is the raw bits, for comparing against a parsed hotkey.
func (m Modifiers) Mask() uint32 { return uint32(m) }

// Any reports whether any of bits is held.
func (m Modifiers) Any(bits uint32) bool { return uint32(m)&bits != 0 }

func modBitFor(vk uint16) uint32 {
	switch vk {
	case 0x10, 0xa0, 0xa1:
		return ModShift
	case 0x11, 0xa2, 0xa3:
		return ModCtrl
	case 0x12, 0xa4, 0xa5:
		return ModAlt
	case 0x5b, 0x5c:
		return ModWin
	}
	return 0
}

// isPrintableVK reports whether a bare press of this key would ordinarily put a
// character in a document. Used only by Mutates, for hand-written macros --
// recording turns these into text steps instead.
func isPrintableVK(vk uint16) bool {
	switch {
	case vk >= 0x41 && vk <= 0x5a, // A-Z
		vk >= 0x30 && vk <= 0x39, // 0-9
		vk >= 0x60 && vk <= 0x6f, // numpad, including its operators
		vk == vkSpace:
		return true
	}
	_, punctuation := keyNames[vk]
	return punctuation && vk >= 0xba // the OEM punctuation block
}
