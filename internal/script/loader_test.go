package script

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func byID(entries []Entry, id string) (Entry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func TestLoadsScripts(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "shout.js", `
		var name = "Shout";
		function transform(i) { return i.toUpperCase() + "!"; }
	`)
	// Fixture files sit beside scripts and must not be loaded as scripts.
	write(t, dir, "shout.test.json", `[]`)

	l := NewLoader(dir)
	entries := l.Load()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.ID != "script.shout" || e.Name != "Shout" {
		t.Errorf("entry = %+v", e)
	}
	if got, err := e.Run("hi"); err != nil || got != "HI!" {
		t.Errorf("Run = %q, %v", got, err)
	}
}

func TestMissingFolderIsNotAnError(t *testing.T) {
	l := NewLoader(filepath.Join(t.TempDir(), "does-not-exist"))
	if entries := l.Load(); len(entries) != 0 {
		t.Errorf("got %d entries, want none", len(entries))
	}
	if l.Changed() {
		t.Error("a folder that never existed should not report a change")
	}
}

// A broken script must stay visible with its error, not silently disappear
// while the user is mid-edit.
func TestBrokenScriptSurfacesItsError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "broken.js", `function transform( {`)

	entries := NewLoader(dir).Load()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if _, err := entries[0].Run("x"); err == nil {
		t.Error("expected the entry to report its compile error")
	}
}

func TestChangeDetection(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "a.js", `function transform(i){ return "v1"; }`)

	l := NewLoader(dir)
	l.Load()
	if l.Changed() {
		t.Fatal("no change expected straight after a load")
	}

	// Size differs, so this is detected even where mtime resolution is coarse.
	if err := os.WriteFile(p, []byte(`function transform(i){ return "version two"; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !l.Changed() {
		t.Fatal("an edited script was not detected")
	}

	entries := l.Load()
	e, ok := byID(entries, "script.a")
	if !ok {
		t.Fatal("script.a missing after reload")
	}
	if got, _ := e.Run("x"); got != "version two" {
		t.Errorf("reload ran the old body: %q", got)
	}
}

func TestNewAndDeletedFilesAreDetected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.js", `function transform(i){ return i; }`)

	l := NewLoader(dir)
	l.Load()

	write(t, dir, "b.js", `function transform(i){ return i; }`)
	if !l.Changed() {
		t.Fatal("a new script was not detected")
	}
	if entries := l.Load(); len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	if err := os.Remove(filepath.Join(dir, "b.js")); err != nil {
		t.Fatal(err)
	}
	if !l.Changed() {
		t.Fatal("a deleted script was not detected")
	}
	if entries := l.Load(); len(entries) != 1 {
		t.Fatalf("got %d entries after delete, want 1", len(entries))
	}
}

// Recompiling on every poll would discard the runtime constantly; unchanged
// files must be reused.
func TestUnchangedScriptsAreNotRecompiled(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.js", `
		var counter = 0;
		function transform(i) { counter++; return String(counter); }
	`)

	l := NewLoader(dir)
	e1, _ := byID(l.Load(), "script.a")
	if got, _ := e1.Run("x"); got != "1" {
		t.Fatalf("first run = %q", got)
	}

	// A second Load with no file change should keep the same runtime, so the
	// counter carries on rather than resetting.
	e2, _ := byID(l.Load(), "script.a")
	if got, _ := e2.Run("x"); got != "2" {
		t.Errorf("script was recompiled on an unchanged load: got %q, want \"2\"", got)
	}
}

func TestLoadIsStableOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "zebra.js", `var name = "Zebra"; function transform(i){return i;}`)
	write(t, dir, "apple.js", `var name = "Apple"; function transform(i){return i;}`)

	l := NewLoader(dir)
	for i := 0; i < 5; i++ {
		entries := l.Load()
		if entries[0].Name != "Apple" || entries[1].Name != "Zebra" {
			t.Fatalf("unstable order: %s, %s", entries[0].Name, entries[1].Name)
		}
	}
	_ = time.Now
}
