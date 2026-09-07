package macro

import (
	"encoding/json"
	"testing"
)

func TestParseChordRoundTrip(t *testing.T) {
	cases := []string{
		"Home", "End", "Left", "Right", "Up", "Down",
		"Ctrl+Right", "Shift+Alt+Right", "Ctrl+Shift+Alt+Win+K",
		"Tab", "Enter", "Escape", "Backspace", "Delete", "Insert",
		"PageUp", "PageDown", "Space", "F2", "F12", "A", "Z", "0", "9",
		"Ctrl+V", ";", "'", ",", ".", "/", "\\", "`", "-", "=", "[", "]",
	}
	for _, want := range cases {
		mods, vk, err := ParseChord(want)
		if err != nil {
			t.Fatalf("ParseChord(%q): %v", want, err)
		}
		if got := FormatChord(mods, vk); got != want {
			t.Errorf("round trip of %q gave %q", want, got)
		}
	}
}

func TestParseChordAliasesAndOrder(t *testing.T) {
	// Whatever order and spelling goes in, one canonical form comes out. That is
	// what stops two macros pressing the same keys from reading differently.
	for _, in := range []string{"alt+shift+right", "SHIFT+ALT+Right", "Shift + Alt + Right"} {
		mods, vk, err := ParseChord(in)
		if err != nil {
			t.Fatalf("ParseChord(%q): %v", in, err)
		}
		if got := FormatChord(mods, vk); got != "Shift+Alt+Right" {
			t.Errorf("ParseChord(%q) formatted as %q, want Shift+Alt+Right", in, got)
		}
	}

	mods, vk, err := ParseChord("Control+Esc")
	if err != nil {
		t.Fatal(err)
	}
	if mods != ModCtrl || vk != vkEscape {
		t.Errorf("Control+Esc gave mods=%x vk=%x", mods, vk)
	}
}

func TestParseChordRejectsNonsense(t *testing.T) {
	for _, in := range []string{"", "  ", "Ctrl+", "Splat+A", "Ctrl+Nope", "F25"} {
		if _, _, err := ParseChord(in); err == nil {
			t.Errorf("ParseChord(%q) should have failed", in)
		}
	}
}

// The on-disk form has to be readable and correctable by hand, which is the
// whole reason a step is written as "Ctrl+Right" rather than as two numbers.
func TestStepJSON(t *testing.T) {
	steps := []Step{
		{Mods: 0, VK: vkHome},
		{Mods: ModCtrl, VK: vkRight},
		{Mods: ModShift | ModAlt, VK: vkRight},
		{Text: `"`},
	}

	data, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	const want = `[{"key":"Home"},{"key":"Ctrl+Right"},{"key":"Shift+Alt+Right"},{"type":"\""}]`
	if string(data) != want {
		t.Fatalf("marshalled as\n  %s\nwant\n  %s", data, want)
	}

	var back []Step
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != len(steps) {
		t.Fatalf("got %d steps back, want %d", len(back), len(steps))
	}
	for i := range steps {
		if back[i] != steps[i] {
			t.Errorf("step %d came back as %+v, want %+v", i, back[i], steps[i])
		}
	}
}

func TestStepJSONRejectsEmpty(t *testing.T) {
	var s Step
	if err := json.Unmarshal([]byte(`{}`), &s); err == nil {
		t.Error("a step with neither key nor type should not unmarshal")
	}
	if err := json.Unmarshal([]byte(`{"key":"Ctrl+Splat"}`), &s); err == nil {
		t.Error("a step with an unparseable key should not unmarshal")
	}
}

func TestUndoDepth(t *testing.T) {
	chord := func(s string) Step {
		mods, vk, err := ParseChord(s)
		if err != nil {
			t.Helper()
			t.Fatal(err)
		}
		return Step{Mods: mods, VK: vk}
	}

	cases := []struct {
		name  string
		steps []Step
		want  int
	}{
		{
			// The macro this feature was asked for. Only the quote changes
			// anything, so one Ctrl+Z is the whole undo and the palette does not
			// have to intervene at all.
			name: "navigate then type once",
			steps: []Step{
				chord("Home"), chord("Ctrl+Right"), chord("Ctrl+Right"),
				chord("Shift+Alt+Right"), {Text: `"`},
			},
			want: 1,
		},
		{
			// Typing either side of a movement is two undo groups, because the
			// movement is what breaks the group.
			name: "type, move, type",
			steps: []Step{
				{Text: `"`}, chord("End"), {Text: `"`},
			},
			want: 2,
		},
		{
			name:  "pure navigation changes nothing",
			steps: []Step{chord("Home"), chord("Ctrl+Right"), chord("Shift+End")},
			want:  0,
		},
		{
			name:  "paste and delete both count",
			steps: []Step{chord("Ctrl+V"), chord("Home"), chord("Delete")},
			want:  2,
		},
		{
			name:  "an unrecognised chord is assumed harmless",
			steps: []Step{chord("Ctrl+Alt+F9"), {Text: "x"}},
			want:  1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := (Macro{Steps: c.steps}).UndoDepth(); got != c.want {
				t.Errorf("UndoDepth() = %d, want %d", got, c.want)
			}
		})
	}

	// An explicit override always wins: what an application groups into one undo
	// is the application's decision, and the estimate is only a starting point.
	m := Macro{Undo: 4, Steps: []Step{chord("Home")}}
	if got := m.UndoDepth(); got != 4 {
		t.Errorf("an explicit undo of 4 gave %d", got)
	}
}

func TestFormat(t *testing.T) {
	steps := []Step{
		{Mods: 0, VK: vkHome},
		{Mods: ModShift | ModAlt, VK: vkRight},
		{Text: "a\tb"},
	}
	const want = "Home\nShift+Alt+Right\ntype \"a\\tb\""
	if got := Format(steps); got != want {
		t.Errorf("Format() =\n%s\nwant\n%s", got, want)
	}
}
