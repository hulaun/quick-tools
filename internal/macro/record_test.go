package macro

import "testing"

// down and up build the raw events a hook would deliver. text is what the key
// would put in a document, which the hook works out from the keyboard layout.
func down(vk uint16, text string) Event { return Event{VK: vk, Down: true, Text: text} }
func up(vk uint16) Event                { return Event{VK: vk, Down: false} }

const (
	vkShiftKey = 0x10
	vkCtrlKey  = 0x11
	vkAltKey   = 0x12
)

func record(events ...Event) []Step {
	var r Recorder
	for _, e := range events {
		r.Add(e)
	}
	return r.Steps()
}

func chords(t *testing.T, steps []Step) []string {
	t.Helper()
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.String()
	}
	return out
}

func wantSteps(t *testing.T, got []Step, want ...string) {
	t.Helper()
	g := chords(t, got)
	if len(g) != len(want) {
		t.Fatalf("recorded %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Fatalf("recorded %v, want %v", g, want)
		}
	}
}

// The macro this feature was asked for, recorded exactly as the hook would
// deliver it: Home, Ctrl+Right twice, Shift+Alt+Right, then a quote.
func TestRecordTheAskedForMacro(t *testing.T) {
	steps := record(
		down(vkHome, ""), up(vkHome),

		down(vkCtrlKey, ""),
		down(vkRight, ""), up(vkRight),
		down(vkRight, ""), up(vkRight),
		up(vkCtrlKey),

		down(vkShiftKey, ""), down(vkAltKey, ""),
		down(vkRight, ""), up(vkRight),
		// The modifiers come up in whichever order the fingers left them, which
		// must not matter.
		up(vkAltKey), up(vkShiftKey),

		down(vkShiftKey, ""),
		down(0x32, `"`), up(0x32), // Shift+2 on a US layout
		up(vkShiftKey),
	)

	wantSteps(t, steps,
		"Home", "Ctrl+Right", "Ctrl+Right", "Shift+Alt+Right", `type "\""`)
}

// A modifier press is never a step of its own, and a key release is never a
// step at all -- the chord was decided when the key went down.
func TestRecordDropsModifiersAndReleases(t *testing.T) {
	steps := record(
		down(vkCtrlKey, ""), up(vkCtrlKey),
		down(vkShiftKey, ""), up(vkShiftKey),
	)
	if len(steps) != 0 {
		t.Fatalf("modifiers alone recorded %v, want nothing", chords(t, steps))
	}
}

// Consecutive typing is one step, because that is one undo in an editor and
// because a macro reading "type \"foo\"" is a macro someone can check.
func TestRecordMergesTypedRuns(t *testing.T) {
	steps := record(
		down(0x46, "f"), up(0x46),
		down(0x4f, "o"), up(0x4f),
		down(0x4f, "o"), up(0x4f),
	)
	wantSteps(t, steps, `type "foo"`)
}

// Anything in between ends the run. Moving the caret is exactly what breaks an
// editor's undo group, so the split here is not cosmetic.
func TestRecordSplitsTypingAroundAMove(t *testing.T) {
	steps := record(
		down(0x46, "f"), up(0x46),
		down(vkEnd, ""), up(vkEnd),
		down(0x4f, "o"), up(0x4f),
	)
	wantSteps(t, steps, `type "f"`, "End", `type "o"`)

	if got := (Macro{Steps: steps}).UndoDepth(); got != 2 {
		t.Errorf("UndoDepth() = %d, want 2", got)
	}
}

// A character produced with Ctrl or Alt held is a command, not typing. Ctrl+V
// famously produces a character -- 0x16 -- and recording that as text would
// replay a control code into the document instead of pasting.
func TestRecordTreatsCommandModifiersAsChords(t *testing.T) {
	steps := record(
		down(vkCtrlKey, ""),
		down(0x56, "\x16"), up(0x56),
		up(vkCtrlKey),
	)
	wantSteps(t, steps, "Ctrl+V")
}

// Keys whose "character" is a control code are keys, whatever the layout says.
// Enter comes back from ToUnicode as "\r" and must replay as Enter.
func TestRecordKeepsControlKeysAsKeys(t *testing.T) {
	steps := record(
		down(vkEnter, "\r"), up(vkEnter),
		down(vkTabKey, "\t"), up(vkTabKey),
		down(vkEscape, "\x1b"), up(vkEscape),
		down(vkBackspace, "\b"), up(vkBackspace),
	)
	wantSteps(t, steps, "Enter", "Tab", "Escape", "Backspace")
}

// Shift is not a command modifier: it is how a capital is made, and the
// character the hook reports already accounts for it.
func TestRecordShiftedTypingIsStillTyping(t *testing.T) {
	steps := record(
		down(vkShiftKey, ""),
		down(0x41, "A"), up(0x41),
		up(vkShiftKey),
	)
	wantSteps(t, steps, `type "A"`)
}

// A held key repeats, and each repeat is a real press worth replaying -- five
// Rights is what holding Right did.
func TestRecordKeepsRepeats(t *testing.T) {
	steps := record(
		down(vkRight, ""), down(vkRight, ""), down(vkRight, ""), up(vkRight),
	)
	wantSteps(t, steps, "Right", "Right", "Right")
}

// The cap exists so that a recording nobody stopped does not grow without
// bound. It stops collecting and says so, rather than silently truncating.
func TestRecordCapsAtMaxSteps(t *testing.T) {
	var r Recorder
	for i := 0; i < MaxSteps+50; i++ {
		r.Add(down(vkRight, ""))
	}
	if len(r.Steps()) != MaxSteps {
		t.Errorf("recorded %d steps, want the cap of %d", len(r.Steps()), MaxSteps)
	}
	if !r.Full() {
		t.Error("Full() should report that the cap was reached")
	}
}

// Held is what the caller matches its own stop combination against.
func TestRecorderTracksHeldModifiers(t *testing.T) {
	var r Recorder
	r.Add(down(vkCtrlKey, ""))
	r.Add(down(vkAltKey, ""))
	if got := r.Held().Mask(); got != ModCtrl|ModAlt {
		t.Errorf("Held() = %x, want %x", got, ModCtrl|ModAlt)
	}
	r.Add(up(vkAltKey))
	if got := r.Held().Mask(); got != ModCtrl {
		t.Errorf("Held() after releasing Alt = %x, want %x", got, ModCtrl)
	}
}
