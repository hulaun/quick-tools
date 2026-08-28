//go:build windows

package winapi

import "testing"

func TestParseHotkey(t *testing.T) {
	cases := []struct {
		in       string
		wantMods uint32
		wantVK   uint32
	}{
		{"Ctrl+Alt+Space", ModControl | ModAlt, 0x20},
		{"ctrl+alt+space", ModControl | ModAlt, 0x20},
		{"Ctrl+Shift+P", ModControl | ModShift, 0x50},
		{"Win+V", ModWin, 0x56},
		{"Ctrl+F12", ModControl, 0x7b},
		{"Ctrl+Alt+1", ModControl | ModAlt, 0x31},
		{"Ctrl + Alt + Space", ModControl | ModAlt, 0x20}, // spaces tolerated
		{"Control+Escape", ModControl, 0x1b},
		{"Ctrl+Alt+`", ModControl | ModAlt, 0xc0},
	}
	for _, c := range cases {
		mods, vk, err := ParseHotkey(c.in)
		if err != nil {
			t.Errorf("ParseHotkey(%q): unexpected error %v", c.in, err)
			continue
		}
		if mods != c.wantMods || vk != c.wantVK {
			t.Errorf("ParseHotkey(%q) = mods %#x vk %#x, want mods %#x vk %#x",
				c.in, mods, vk, c.wantMods, c.wantVK)
		}
	}
}

func TestParseHotkeyRejects(t *testing.T) {
	// A bare key would swallow that key system-wide in every application, and a
	// typo in a modifier should be reported rather than silently dropped.
	bad := []string{
		"Space",         // no modifier
		"",              // empty
		"Ctrl+",         // empty key
		"Ctrl+Nonsense", // unknown key
		"Ctrlx+Space",   // misspelt modifier
		"Ctrl+Alt+F99",  // out of range function key
	}
	for _, in := range bad {
		if mods, vk, err := ParseHotkey(in); err == nil {
			t.Errorf("ParseHotkey(%q) = %#x/%#x, want an error", in, mods, vk)
		}
	}
}

// Every hotkey the parser accepts must round-trip through the same modifier
// constants RegisterHotKey uses, so a config typo cannot silently register a
// different combination than the one written.
func TestParseHotkeyModifiersAreDistinct(t *testing.T) {
	seen := map[uint32]string{}
	for name, m := range modifierNames {
		if prev, ok := seen[m]; ok && prev != "" {
			continue // aliases such as ctrl/control are expected
		}
		seen[m] = name
	}
	if len(seen) != 4 {
		t.Errorf("expected 4 distinct modifiers, got %d: %v", len(seen), seen)
	}
}
