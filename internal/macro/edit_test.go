package macro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "macros.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing file should not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d macros from a missing file", len(got))
	}
}

func TestLoadReportsMalformed(t *testing.T) {
	if _, err := Load(write(t, "{not json")); err == nil {
		t.Error("a malformed file should be reported, not swallowed")
	}
}

// The index is the position in the file, not the position in the list. An
// entry skipped for having no name must not shift the ones after it, or a
// rename lands on the wrong macro -- the same trap place.Load has a test for.
func TestLoadIndexSurvivesASkippedEntry(t *testing.T) {
	path := write(t, `[
	  {"name": "first",  "steps": [{"key": "Home"}]},
	  {"steps": [{"key": "End"}]},
	  {"name": "third",  "steps": [{"key": "Ctrl+Right"}]}
	]`)

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d macros, want 2", len(got))
	}
	if got[0].Index != 0 {
		t.Errorf("first macro has index %d, want 0", got[0].Index)
	}
	if got[1].Index != 2 {
		t.Errorf("third macro has index %d, want 2 -- the position in the file", got[1].Index)
	}
}

// A field this version does not know about, on an entry that is not being
// touched, must come back out unchanged.
func TestEditPreservesUnknownFields(t *testing.T) {
	path := write(t, `[
	  {"_comment": "hand written", "name": "keep", "steps": [{"key": "Home"}], "future": {"big": 9007199254740993}},
	  {"name": "rename me", "steps": [{"key": "End"}]}
	]`)

	s := NewStore(path)
	if err := s.SetName(1, "renamed"); err != nil {
		t.Fatal(err)
	}

	out := read(t, path)
	for _, want := range []string{`"_comment"`, `"hand written"`, `9007199254740993`, `"renamed"`} {
		if !strings.Contains(out, want) {
			t.Errorf("after a rename the file lost %s:\n%s", want, out)
		}
	}
}

// The same rule for the entry actually being changed: a large number in a
// field we do not model must not be rounded on the way through.
func TestSetStepsPreservesTheRestOfTheEntry(t *testing.T) {
	path := write(t, `[
	  {"name": "tuned", "group": "sql", "delay": 25, "undo": 3, "big": 9007199254740993,
	   "steps": [{"key": "Home"}]}
	]`)

	s := NewStore(path)
	newSteps := []Step{{Mods: ModCtrl, VK: vkRight}, {Text: "x"}}
	if err := s.SetSteps(0, newSteps); err != nil {
		t.Fatal(err)
	}

	out := read(t, path)
	for _, want := range []string{`"delay": 25`, `"undo": 3`, `9007199254740993`, `"sql"`, `"Ctrl+Right"`} {
		if !strings.Contains(out, want) {
			t.Errorf("re-recording lost %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"Home"`) {
		t.Errorf("re-recording kept the old steps:\n%s", out)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Steps) != 2 {
		t.Fatalf("reloaded %+v", got)
	}
	if got[0].Delay != 25 || got[0].Undo != 3 {
		t.Errorf("tuning was lost: %+v", got[0])
	}
}

func TestAddCreatesTheFileAndFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "storage", "macros.json")
	s := NewStore(path)

	idx, err := s.Add(Macro{Name: "first"})
	if err != nil {
		t.Fatalf("Add into a folder that does not exist yet: %v", err)
	}
	if idx != 0 {
		t.Errorf("first Add returned index %d, want 0", idx)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "first" {
		t.Fatalf("loaded %+v", got)
	}
	// An empty recording is an empty list, not a missing field: the tab has to
	// be able to say "nothing recorded yet" rather than showing a broken entry.
	if got[0].Steps == nil {
		t.Error("a new macro should have an empty steps list, not null")
	}
}

func TestDeleteAddressesTheFilePosition(t *testing.T) {
	path := write(t, `[
	  {"name": "a", "steps": []},
	  {"name": "b", "steps": []},
	  {"name": "c", "steps": []}
	]`)

	s := NewStore(path)
	if err := s.Delete(1); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Fatalf("after deleting index 1: %+v", got)
	}

	if err := s.Delete(7); err == nil {
		t.Error("deleting an index that is not there should fail rather than corrupt the file")
	}
}

func TestEmptyListStaysAnArray(t *testing.T) {
	path := write(t, `[{"name": "only", "steps": []}]`)
	s := NewStore(path)
	if err := s.Delete(0); err != nil {
		t.Fatal(err)
	}

	var raws []json.RawMessage
	if err := json.Unmarshal([]byte(read(t, path)), &raws); err != nil {
		t.Fatalf("an emptied file should still parse as a list: %v", err)
	}
}
