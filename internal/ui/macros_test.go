//go:build windows

package ui

import (
	"strings"
	"testing"

	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/macro"
)

// fakeMacros is a macroStore in memory, so the parts of the tab that are policy
// rather than Win32 can be exercised without a window.
type fakeMacros struct {
	entries []macro.Macro
	err     error
}

func (f *fakeMacros) Load() ([]macro.Macro, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]macro.Macro, len(f.entries))
	for i, m := range f.entries {
		m.Index = i
		out[i] = m
	}
	return out, nil
}

func (f *fakeMacros) Changed() bool { return false }
func (f *fakeMacros) Path() string  { return `C:\storage\macros.json` }

func (f *fakeMacros) Add(m macro.Macro) (int, error) {
	f.entries = append(f.entries, m)
	return len(f.entries) - 1, nil
}

func (f *fakeMacros) SetName(i int, name string) error {
	f.entries[i].Name = name
	return nil
}

func (f *fakeMacros) SetSteps(i int, steps []macro.Step) error {
	f.entries[i].Steps = steps
	return nil
}

func (f *fakeMacros) Delete(i int) error {
	f.entries = append(f.entries[:i], f.entries[i+1:]...)
	return nil
}

func chord(t *testing.T, s string) macro.Step {
	t.Helper()
	mods, vk, err := macro.ParseChord(s)
	if err != nil {
		t.Fatal(err)
	}
	return macro.Step{Mods: mods, VK: vk}
}

func macroPalette(entries ...macro.Macro) *Palette {
	return &Palette{
		cfg:    config.Config{Hotkey: "Ctrl+Alt+Q", MacroUndo: true},
		macros: &fakeMacros{entries: entries},
	}
}

// The index carried on a row is the position in macros.json, which is what
// rename and delete address. The list is sorted for reading, so it is not the
// row number -- getting this wrong renames the entry above or below the one on
// screen.
func TestReloadMacrosSortsButKeepsFileIndexes(t *testing.T) {
	p := macroPalette(
		macro.Macro{Name: "zulu", Group: "b"},
		macro.Macro{Name: "alpha", Group: "b"},
		macro.Macro{Name: "mike", Group: "a"},
	)
	p.reloadMacros()

	items := p.indexes[modeMacros].Search("")
	if len(items) != 3 {
		t.Fatalf("indexed %d macros, want 3", len(items))
	}

	wantOrder := []struct {
		name  string
		index int
	}{
		{"mike", 2},  // group "a" first
		{"alpha", 1}, // then "b", by name
		{"zulu", 0},
	}
	for i, want := range wantOrder {
		r, ok := items[i].Data.(macroRow)
		if !ok {
			t.Fatalf("row %d is not a macroRow", i)
		}
		if r.Name != want.name {
			t.Errorf("row %d is %q, want %q", i, r.Name, want.name)
		}
		if r.Index != want.index {
			t.Errorf("%q has file index %d, want %d", r.Name, r.Index, want.index)
		}
	}
}

// A malformed file is reported in the pane. An empty tab with no reason given
// looks like the feature is broken rather than like a typo in a JSON file.
func TestMacroEmptyMessageReportsAReadError(t *testing.T) {
	p := macroPalette()
	p.macros.(*fakeMacros).err = errFake{}
	p.reloadMacros()

	msg := p.macroEmptyMessage()
	if !strings.Contains(msg, "macros.json") || !strings.Contains(msg, "broken") {
		t.Errorf("empty message = %q, want it to name the file and the error", msg)
	}
}

type errFake struct{}

func (errFake) Error() string { return "broken" }

// With nothing wrong, the message says how to record rather than just saying
// there is nothing -- the stop key is the one part of the flow that cannot be
// guessed, so it is named.
func TestMacroEmptyMessageExplainsRecording(t *testing.T) {
	p := macroPalette()
	p.reloadMacros()

	msg := p.macroEmptyMessage()
	for _, want := range []string{"Alt+D", "Ctrl+Alt+Q"} {
		if !strings.Contains(msg, want) {
			t.Errorf("empty message does not mention %s:\n%s", want, msg)
		}
	}
}

// What the pane says about Ctrl+Z is the difference between trusting the macro
// and not, so it is worth pinning per case.
func TestDescribeUndo(t *testing.T) {
	cases := []struct {
		name  string
		m     macro.Macro
		arm   bool
		want  string
		avoid string
	}{
		{
			name: "pure navigation",
			m:    macro.Macro{Steps: []macro.Step{chord(t, "Home"), chord(t, "End")}},
			arm:  true,
			want: "nothing to undo",
		},
		{
			name: "one change",
			m: macro.Macro{Steps: []macro.Step{
				chord(t, "Home"), chord(t, "Ctrl+Right"), {Text: `"`},
			}},
			arm:   true,
			want:  "one Ctrl+Z",
			avoid: "undoes all",
		},
		{
			name: "two changes, watch armed",
			m: macro.Macro{Steps: []macro.Step{
				{Text: `"`}, chord(t, "End"), {Text: `"`},
			}},
			arm:  true,
			want: "the first Ctrl+Z after it runs undoes all 2",
		},
		{
			// With macroUndo off the pane must not claim the palette will help.
			name: "two changes, watch off",
			m: macro.Macro{Steps: []macro.Step{
				{Text: `"`}, chord(t, "End"), {Text: `"`},
			}},
			want:  "2 Ctrl+Z presses undo it",
			avoid: "undoes all",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := describeUndo(c.m, c.arm)
			if !strings.Contains(got, c.want) {
				t.Errorf("describeUndo() = %q, want it to contain %q", got, c.want)
			}
			// The pane must not claim the palette does anything the code will not
			// do: the watch is never armed below a depth of two, nor at all with
			// macroUndo off.
			if c.avoid != "" && strings.Contains(got, c.avoid) {
				t.Errorf("describeUndo() = %q, should not mention %q", got, c.avoid)
			}
		})
	}
}

// The label is what the row reads as, and a macro with no group must not come
// out with a stray colon in front of it.
func TestMacroLabel(t *testing.T) {
	p := macroPalette(
		macro.Macro{Name: "grouped", Group: "SQL"},
		macro.Macro{Name: "bare"},
	)
	p.reloadMacros()

	items := p.indexes[modeMacros].Search("")
	got := []string{macroLabel(items[0]), macroLabel(items[1])}
	want := []string{"bare", "SQL: grouped"} // "" sorts before "sql"
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("labels = %v, want %v", got, want)
		}
	}
}

// The steps are searchable as context but must never outrank a name. A macro
// actually called "home" has to win over every macro that happens to press it.
func TestMacroSearchRanksNamesOverSteps(t *testing.T) {
	p := macroPalette(
		macro.Macro{Name: "indent", Steps: []macro.Step{chord(t, "Home"), chord(t, "Tab")}},
		macro.Macro{Name: "home", Steps: []macro.Step{chord(t, "End")}},
	)
	p.reloadMacros()

	got := p.indexes[modeMacros].Search("home")
	if len(got) == 0 {
		t.Fatal("searching for home found nothing")
	}
	if got[0].Name != "home" {
		t.Errorf("best match for %q is %q, want the macro named home", "home", got[0].Name)
	}
	if len(got) != 2 {
		t.Errorf("found %d macros, want both -- the other presses Home", len(got))
	}
}
