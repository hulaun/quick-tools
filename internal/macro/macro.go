// Package macro holds the palette's fourth mode: recorded keystroke sequences,
// replayed into whatever window had focus.
//
// A macro is the answer to an edit that has to be made on twenty scattered
// lines and cannot be made with a selection: put the caret on the line, press
// one key, and the same run of Home / Ctrl+Right / Shift+Alt+Right / a
// character happens again exactly as it did the first time.
//
// The package is deliberately free of Win32. Everything here is types, text
// form and the rules for turning a raw stream of key events into a clean list
// of steps -- all of which is worth testing, and none of which needs a window.
// The hook that produces the events and the SendInput that replays them live in
// internal/winapi.
package macro

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Modifier bits.
//
// The values are RegisterHotKey's MOD_* constants, which are also what
// winapi.ModAlt and friends use. That is not a coincidence worth relying on
// silently: keeping them equal is what lets a Step cross into winapi without a
// conversion table in between, and a conversion table is exactly the kind of
// thing that gets one bit wrong and produces a macro that types Alt+Right when
// it meant Shift+Right.
const (
	ModAlt   = 0x1
	ModCtrl  = 0x2
	ModShift = 0x4
	ModWin   = 0x8
)

// Step is one thing a macro does: type some literal text, or press one chord.
//
// The two are kept apart rather than reduced to "press these keys" because
// they replay differently and for a good reason. Literal text goes in as
// Unicode code units, which is independent of the keyboard layout -- a macro
// that types " must still type " on a layout where that character is not
// Shift+2. A chord is a navigation or editing command and has to go in as the
// virtual key it is, because that is what the target application is listening
// for.
type Step struct {
	// Text is literal characters. Non-empty means this is a text step and Mods
	// and VK are unused.
	Text string

	Mods uint32
	VK   uint16
}

// IsText reports whether the step types characters rather than pressing a key.
func (s Step) IsText() bool { return s.Text != "" }

// String is the step in its readable form: `Ctrl+Right`, or `"foo"` for text.
func (s Step) String() string {
	if s.IsText() {
		return "type " + quote(s.Text)
	}
	return FormatChord(s.Mods, s.VK)
}

// quote renders text for display without pulling in the whole package for one
// call. Go's own quoting is what is wanted here: a recorded tab or newline has
// to be visible in the pane rather than laid out as one.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// stepJSON is the on-disk form. A step is written as one of two shapes:
//
//	{"key": "Ctrl+Right"}
//	{"type": "\""}
//
// rather than as a modifier mask and a number, because macros.json is a file
// someone will open. "Shift+Alt+Right" says what it does; {"mods":5,"vk":39}
// does not, and cannot be corrected by hand.
type stepJSON struct {
	Key  string `json:"key,omitempty"`
	Type string `json:"type,omitempty"`
}

func (s Step) MarshalJSON() ([]byte, error) {
	if s.IsText() {
		return json.Marshal(stepJSON{Type: s.Text})
	}
	return json.Marshal(stepJSON{Key: FormatChord(s.Mods, s.VK)})
}

func (s *Step) UnmarshalJSON(data []byte) error {
	var raw stepJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Type != "" {
		*s = Step{Text: raw.Type}
		return nil
	}
	if raw.Key == "" {
		return fmt.Errorf("a step needs either \"key\" or \"type\"")
	}
	mods, vk, err := ParseChord(raw.Key)
	if err != nil {
		return err
	}
	*s = Step{Mods: mods, VK: vk}
	return nil
}

// Macro is one entry in macros.json.
type Macro struct {
	Name string `json:"name"`

	// Group is an optional heading, shown before the name and searchable as
	// context -- the same as a place's group and a note's folder.
	Group string `json:"group,omitempty"`

	Steps []Step `json:"steps"`

	// Undo overrides how many Ctrl+Z presses put the target back the way it was.
	// Zero means work it out from the steps; see UndoDepth for why that is a
	// guess worth being able to correct.
	Undo int `json:"undo,omitempty"`

	// Delay is the gap between steps in milliseconds, or zero for the default.
	// Some applications drop synthesised keys that arrive faster than a person
	// could type them, and which ones is not something that can be known here.
	Delay int `json:"delay,omitempty"`

	// Index is the entry's position in macros.json, filled in by Load and never
	// written back. Renaming and deleting address it, because the list on screen
	// is sorted for reading and its order is not the file's -- the same rule, and
	// the same trap, as place.Place.Index.
	Index int `json:"-"`
}

// DefaultDelay is the gap between steps when a macro does not name one.
//
// Zero would be faster and is wrong: a synthesised burst with no gap at all
// arrives inside a single message-pump turn in some editors, which then process
// the navigation and the typing out of order. Eight milliseconds is far below
// noticing and above every threshold seen so far.
const DefaultDelay = 8

// Gap is the delay this macro replays at, in milliseconds.
func (m Macro) Gap() int {
	if m.Delay > 0 {
		return m.Delay
	}
	return DefaultDelay
}

// UndoDepth is how many Ctrl+Z presses undo one replay of this macro.
//
// It counts *runs* of consecutive text-changing steps rather than the steps
// themselves, because that is how editors group an undo: typing three
// characters in a row is one undo, but moving the caret between them breaks the
// group. So the macro in the README -- Home, Ctrl+Right, Ctrl+Right,
// Shift+Alt+Right, then a quote -- has a depth of one, and a plain Ctrl+Z
// already does the right thing with no help from us.
//
// It is a guess, and it says so: what an application groups into one undo is
// the application's decision and there is no way to ask. Setting "undo" on the
// macro replaces the guess with a number.
func (m Macro) UndoDepth() int {
	if m.Undo > 0 {
		return m.Undo
	}
	runs, prev := 0, false
	for _, s := range m.Steps {
		cur := s.Mutates()
		if cur && !prev {
			runs++
		}
		prev = cur
	}
	return runs
}

// Mutates reports whether a step changes the document rather than only moving
// around in it.
//
// Navigation is free: Home, the arrows, Ctrl+Right and every selection made
// with Shift leave the text exactly as it was, so they contribute nothing to an
// undo. Only the keys below do, and the list is deliberately short and
// conservative -- an unrecognised chord is treated as harmless, which makes the
// estimate too low rather than too high. Too low means a leftover Ctrl+Z the
// user presses themselves; too high means undoing an edit they made before the
// macro ran, which is the one outcome worth ruling out.
func (s Step) Mutates() bool {
	if s.IsText() {
		return true
	}
	const (
		vkBack   = 0x08
		vkTab    = 0x09
		vkReturn = 0x0d
		vkDelete = 0x2e
	)
	if s.Mods&ModCtrl == 0 && s.Mods&ModAlt == 0 && s.Mods&ModWin == 0 {
		switch s.VK {
		case vkBack, vkTab, vkReturn, vkDelete:
			return true
		}
		// A bare printable key, which only a hand-written macro produces: the
		// recorder turns those into text steps.
		return isPrintableVK(s.VK)
	}
	if s.Mods == ModCtrl {
		switch s.VK {
		case 'V', 'X', 'D', 'K', 'J', 'Y', vkBack, vkDelete, vkReturn:
			return true
		}
	}
	return false
}

// Format renders a macro's steps one per line, which is what the pane shows and
// what makes the recording readable enough to trust before running it.
func Format(steps []Step) string {
	var b strings.Builder
	for i, s := range steps {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s.String())
	}
	return b.String()
}
