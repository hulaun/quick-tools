//go:build windows

package ui

import (
	"strings"
	"testing"

	"github.com/hulaun/quick-tools/internal/place"
)

// A palette with every opener installed. actionsFor reads nothing else, so the
// action policy can be tested without a window.
func testPalette() *Palette {
	return &Palette{openers: place.Openers{
		place.OpenCode:      `C:\VSCode\Code.exe`,
		place.OpenNotepadPP: `C:\Program Files\Notepad++\notepad++.exe`,
		place.OpenTerminal:  `C:\Windows\System32\wt.exe`,
	}}
}

func labels(acts []action) []string {
	out := make([]string, len(acts))
	for i, a := range acts {
		out[i] = a.label
	}
	return out
}

// The first action is what Enter runs, so what comes first per kind of path is
// the whole behaviour of the tab.
func TestActionsForDefaultOrder(t *testing.T) {
	p := testPalette()

	cases := []struct {
		name  string
		kind  place.Kind
		first string
	}{
		{"a folder opens in Explorer", place.KindDir, "Open in Explorer"},
		{"an executable runs", place.KindExe, "Run"},
		{"any other file goes to its registered app", place.KindFile, "Open"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acts := p.actionsFor(placeRow{resolved: `C:\x`}, c.kind)
			if len(acts) == 0 {
				t.Fatal("no actions")
			}
			if acts[0].label != c.first {
				t.Errorf("first action = %q, want %q (got %v)", acts[0].label, c.first, labels(acts))
			}
		})
	}
}

// Copying the path is always available, including for a path that is not there
// -- it is the one thing that still works when nothing can be opened.
func TestActionsForAlwaysOfferCopy(t *testing.T) {
	p := testPalette()
	for _, kind := range []place.Kind{place.KindDir, place.KindFile, place.KindExe, place.KindMissing} {
		acts := p.actionsFor(placeRow{resolved: `C:\x`}, kind)
		found := false
		for _, a := range acts {
			if a.id == place.OpenCopy {
				found = true
			}
		}
		if !found {
			t.Errorf("kind %v has no copy action: %v", kind, labels(acts))
		}
	}
}

// A missing path offers nothing that would fail. Listing "Open in VS Code" for
// a path that is not there only produces an error a moment later.
func TestActionsForMissingOffersOnlyCopy(t *testing.T) {
	acts := testPalette().actionsFor(placeRow{resolved: `Z:\nope`}, place.KindMissing)
	if len(acts) != 1 || acts[0].id != place.OpenCopy {
		t.Errorf("missing path actions = %v, want just a copy", labels(acts))
	}
}

// The place's own choice becomes the first action, which is what makes
// "open": "code" mean "Enter opens this in VS Code".
func TestActionsForConfiguredOpenerGoesFirst(t *testing.T) {
	p := testPalette()
	acts := p.actionsFor(placeRow{
		Place:    place.Place{Open: place.OpenCode},
		resolved: `C:\project`,
	}, place.KindDir)

	if acts[0].id != place.OpenCode {
		t.Fatalf("first action = %q, want the configured opener (got %v)", acts[0].id, labels(acts))
	}
	// Moving it must not lose or duplicate anything else.
	seen := map[string]int{}
	for _, a := range acts {
		seen[a.id]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("action %q appears %d times: %v", id, n, labels(acts))
		}
	}
	if len(acts) != len(p.actionsFor(placeRow{resolved: `C:\project`}, place.KindDir)) {
		t.Errorf("reordering changed the number of actions: %v", labels(acts))
	}
}

// An opener that is not installed is left out rather than offered and failing.
func TestActionsForOmitsMissingOpeners(t *testing.T) {
	p := &Palette{openers: place.Openers{}} // nothing detected
	acts := p.actionsFor(placeRow{resolved: `C:\x\file.txt`}, place.KindFile)

	for _, a := range acts {
		switch a.id {
		case place.OpenCode, place.OpenNotepadPP, place.OpenTerminal:
			t.Errorf("offered %q with no executable for it: %v", a.id, labels(acts))
		}
	}
	// The ones that need no third-party app must survive.
	if len(acts) == 0 || acts[0].label != "Open" {
		t.Errorf("actions with nothing installed = %v, want the shell defaults", labels(acts))
	}
}

// Elevation is carried down to the launch and said out loud, because it is the
// difference between an editor that can save to hosts and one that cannot.
func TestActionsForElevation(t *testing.T) {
	p := testPalette()
	acts := p.actionsFor(placeRow{
		Place:    place.Place{Open: place.OpenNotepadPP, Elevate: true},
		resolved: `C:\Windows\System32\drivers\etc\hosts`,
	}, place.KindFile)

	if acts[0].id != place.OpenNotepadPP {
		t.Fatalf("first action = %q, want notepad++", acts[0].id)
	}
	if !acts[0].elevate {
		t.Error("first action does not carry elevate")
	}
	if !strings.Contains(acts[0].label, "as admin") {
		t.Errorf("label = %q, want it to say the launch is elevated", acts[0].label)
	}

	// Copying a path never needs administrator rights, and saying so would be
	// noise.
	for _, a := range acts {
		if a.id == place.OpenCopy {
			if a.elevate {
				t.Error("the copy action carries elevate")
			}
			if strings.Contains(a.label, "as admin") {
				t.Errorf("copy label = %q, want no elevation note", a.label)
			}
		}
	}
}

// The terminal option names the terminal that was actually found. "Open in
// Terminal" landing in a bare cmd.exe is a small surprise worth removing.
func TestTerminalLabel(t *testing.T) {
	if got := terminalLabel(`C:\Windows\System32\wt.exe`); got != "Open in Terminal" {
		t.Errorf("wt.exe labelled %q", got)
	}
	if got := terminalLabel(`C:\Windows\System32\cmd.exe`); got != "Open in Command Prompt" {
		t.Errorf("cmd.exe labelled %q", got)
	}
}

// A file's terminal starts in the folder holding it, not in the file.
func TestActionsForTerminalDirectory(t *testing.T) {
	p := testPalette()

	dirActs := p.actionsFor(placeRow{resolved: `C:\project`}, place.KindDir)
	fileActs := p.actionsFor(placeRow{resolved: `C:\project\notes.txt`}, place.KindFile)

	find := func(acts []action) action {
		for _, a := range acts {
			if a.id == place.OpenTerminal {
				return a
			}
		}
		t.Fatal("no terminal action")
		return action{}
	}

	if got, want := find(dirActs).dir, `C:\project`; got != want {
		t.Errorf("terminal dir for a folder = %q, want %q", got, want)
	}
	if got, want := find(fileActs).dir, `C:\project`; got != want {
		t.Errorf("terminal dir for a file = %q, want its parent %q", got, want)
	}
}
