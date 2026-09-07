package place

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T, body string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "places.json")
	if body != "" {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(path)
}

func mustLoad(t *testing.T, s *Store) []Place {
	t.Helper()
	places, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	return places
}

func TestAddCreatesTheFile(t *testing.T) {
	s := newTestStore(t, "")

	idx, err := s.Add(Place{Name: "Work", Path: `D:\work`})
	if err != nil {
		t.Fatal(err)
	}
	if idx != 0 {
		t.Errorf("index = %d, want 0 for the first entry", idx)
	}

	places := mustLoad(t, s)
	if len(places) != 1 || places[0].Name != "Work" || places[0].Path != `D:\work` {
		t.Fatalf("got %+v", places)
	}
}

func TestAddAppendsAndIndexesInFileOrder(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"},{"name":"B","path":"C:/b"}]`)

	idx, err := s.Add(Place{Name: "C", Path: `C:\c`})
	if err != nil {
		t.Fatal(err)
	}
	if idx != 2 {
		t.Errorf("index = %d, want 2", idx)
	}

	places := mustLoad(t, s)
	for i, want := range []string{"A", "B", "C"} {
		if places[i].Name != want {
			t.Errorf("entry %d = %q, want %q", i, places[i].Name, want)
		}
		if places[i].Index != i {
			t.Errorf("entry %d has Index %d", i, places[i].Index)
		}
	}
}

func TestSetName(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"},{"name":"B","path":"C:/b"}]`)

	if err := s.SetName(1, "Renamed"); err != nil {
		t.Fatal(err)
	}
	places := mustLoad(t, s)
	if places[1].Name != "Renamed" {
		t.Errorf("name = %q, want Renamed", places[1].Name)
	}
	if places[0].Name != "A" {
		t.Errorf("renaming one entry changed another: %q", places[0].Name)
	}
	if places[1].Path != `C:/b` {
		t.Errorf("renaming lost the path: %q", places[1].Path)
	}
}

func TestSetNameRejectsEmpty(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"}]`)
	if err := s.SetName(0, ""); err == nil {
		t.Error("an empty name was accepted")
	}
	if mustLoad(t, s)[0].Name != "A" {
		t.Error("the rejected rename still wrote to the file")
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"},{"name":"B","path":"C:/b"},{"name":"C","path":"C:/c"}]`)

	if err := s.Delete(1); err != nil {
		t.Fatal(err)
	}
	places := mustLoad(t, s)
	if len(places) != 2 || places[0].Name != "A" || places[1].Name != "C" {
		t.Fatalf("got %+v", places)
	}
	// The indexes have to be the new file positions, or the next delete removes
	// the wrong row.
	if places[1].Index != 1 {
		t.Errorf("index after a delete = %d, want 1", places[1].Index)
	}
}

func TestDeleteOutOfRangeChangesNothing(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"}]`)
	for _, i := range []int{-1, 1, 99} {
		if err := s.Delete(i); err == nil {
			t.Errorf("Delete(%d) was accepted", i)
		}
	}
	if len(mustLoad(t, s)) != 1 {
		t.Error("a rejected delete still wrote to the file")
	}
}

func TestDeleteLastLeavesAnEmptyArray(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"}]`)
	if err := s.Delete(0); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "[]" {
		t.Errorf("file = %q, want an empty array -- null is not a places file", got)
	}
	if len(mustLoad(t, s)) != 0 {
		t.Error("the emptied file did not load as no places")
	}
}

// Editing one entry must not quietly rewrite the others. A hand-written file
// carries comments and fields this version does not know about, and losing
// them on an unrelated rename would be the kind of damage nobody notices until
// much later.
func TestEditPreservesUnknownFieldsOnOtherEntries(t *testing.T) {
	s := newTestStore(t, `[
  {"_comment": "keep me", "name": "A", "path": "C:/a", "futureField": {"nested": [1, 2]}},
  {"name": "B", "path": "C:/b"}
]`)

	if err := s.SetName(1, "Renamed"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"_comment", "keep me", "futureField", "nested"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("%q was lost:\n%s", want, data)
		}
	}
}

// The entry being edited keeps its own unknown fields too.
func TestSetNamePreservesUnknownFieldsOnTheEditedEntry(t *testing.T) {
	s := newTestStore(t, `[{"_comment":"mine","name":"A","path":"C:/a","elevate":true}]`)

	if err := s.SetName(0, "B"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "_comment") {
		t.Errorf("the edited entry lost its comment:\n%s", data)
	}
	if !mustLoad(t, s)[0].Elevate {
		t.Error("the edited entry lost elevate")
	}
}

// A big integer in a field this version does not model must survive a rename.
// Decoding into a plain `any` would turn it into a float64 and round it, which
// is the same trap the JSON transforms have a test pinning.
func TestSetNameDoesNotCorruptLargeNumbers(t *testing.T) {
	const id = "9007199254740993"
	s := newTestStore(t, `[{"name":"A","path":"C:/a","someId":`+id+`}]`)

	if err := s.SetName(0, "B"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), id) {
		t.Errorf("the id was rewritten:\n%s", data)
	}
}

// The file stays something a person can open and edit by hand.
func TestRewriteStaysIndented(t *testing.T) {
	s := newTestStore(t, `[{"name":"A","path":"C:/a"}]`)
	if _, err := s.Add(Place{Name: "B", Path: `C:\b`}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\n  {") {
		t.Errorf("output is not indented:\n%s", data)
	}
	if !json.Valid(data) {
		t.Errorf("output is not valid JSON:\n%s", data)
	}
}

// A blank-path entry is skipped for display but still occupies its position in
// the file, or every index after it would address the wrong row.
func TestLoadIndexesSurviveSkippedEntries(t *testing.T) {
	s := newTestStore(t, `[{"name":"blank","path":"  "},{"name":"real","path":"C:/a"}]`)

	places := mustLoad(t, s)
	if len(places) != 1 {
		t.Fatalf("got %d places, want 1", len(places))
	}
	if places[0].Index != 1 {
		t.Fatalf("index = %d, want 1 -- the file position, not the display position", places[0].Index)
	}

	// Deleting by that index must remove the real entry, not the blank one.
	if err := s.Delete(places[0].Index); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.Path())
	if strings.Contains(string(data), "real") {
		t.Errorf("deleted the wrong entry:\n%s", data)
	}
	if !strings.Contains(string(data), "blank") {
		t.Errorf("deleted the wrong entry:\n%s", data)
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\.m2`: ".m2",
		`C:\Users\me\`:    "me",
		`C:\`:             "C:",
		`D:\work\a.txt`:   "a.txt",
		"":                "place",
	}
	for in, want := range cases {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsRooted(t *testing.T) {
	rooted := []string{`C:\x`, `C:`, `\\server\share`, `\x`}
	loose := []string{"vpn", "", "src\\main", "..\\up"}

	for _, s := range rooted {
		if !IsRooted(s) {
			t.Errorf("IsRooted(%q) = false, want true", s)
		}
	}
	for _, s := range loose {
		if IsRooted(s) {
			t.Errorf("IsRooted(%q) = true, want false", s)
		}
	}
}
