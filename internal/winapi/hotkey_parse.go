//go:build windows

package winapi

import (
	"fmt"
	"strings"
)

// namedKeys maps the spellings a user might reasonably write in config.json to
// virtual key codes. Letters and digits are handled arithmetically below.
var namedKeys = map[string]uint32{
	"space": 0x20, "enter": 0x0d, "return": 0x0d, "tab": 0x09,
	"escape": 0x1b, "esc": 0x1b, "backspace": 0x08, "delete": 0x2e, "del": 0x2e,
	"insert": 0x2d, "ins": 0x2d, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pagedown": 0x22,
	"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
	"`": 0xc0, "backtick": 0xc0, "tilde": 0xc0,
	"-": 0xbd, "minus": 0xbd, "=": 0xbb, "equals": 0xbb,
	"[": 0xdb, "]": 0xdd, ";": 0xba, "'": 0xde,
	",": 0xbc, ".": 0xbe, "/": 0xbf, "\\": 0xdc,
}

var modifierNames = map[string]uint32{
	"ctrl": ModControl, "control": ModControl,
	"alt": ModAlt, "shift": ModShift,
	"win": ModWin, "super": ModWin, "meta": ModWin, "cmd": ModWin,
}

// ParseHotkey turns a string like "Ctrl+Alt+Space" into the modifier mask and
// virtual key code RegisterHotKey wants.
//
// It is deliberately strict about requiring at least one modifier: registering
// a bare key system-wide would swallow that key in every application, which is
// never what someone means.
func ParseHotkey(s string) (modifiers, vk uint32, err error) {
	parts := strings.Split(s, "+")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("hotkey %q needs at least one modifier and a key, e.g. Ctrl+Alt+Space", s)
	}

	for i, raw := range parts {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			return 0, 0, fmt.Errorf("hotkey %q has an empty part", s)
		}

		// Everything but the final part must be a modifier.
		if i < len(parts)-1 {
			m, ok := modifierNames[p]
			if !ok {
				return 0, 0, fmt.Errorf("hotkey %q: %q is not a modifier (use Ctrl, Alt, Shift or Win)", s, raw)
			}
			modifiers |= m
			continue
		}

		key, kerr := parseKey(p)
		if kerr != nil {
			return 0, 0, fmt.Errorf("hotkey %q: %w", s, kerr)
		}
		vk = key
	}

	if modifiers == 0 {
		return 0, 0, fmt.Errorf("hotkey %q needs at least one modifier", s)
	}
	return modifiers, vk, nil
}

func parseKey(p string) (uint32, error) {
	if vk, ok := namedKeys[p]; ok {
		return vk, nil
	}
	if len(p) == 1 {
		c := p[0]
		if c >= 'a' && c <= 'z' {
			return uint32(c-'a') + 0x41, nil // VK_A..VK_Z
		}
		if c >= '0' && c <= '9' {
			return uint32(c-'0') + 0x30, nil // VK_0..VK_9
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
			return uint32(0x70 + n - 1), nil // VK_F1..VK_F24
		}
	}
	return 0, fmt.Errorf("%q is not a key this build recognises", p)
}
