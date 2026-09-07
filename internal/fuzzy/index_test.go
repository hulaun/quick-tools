package fuzzy

import "testing"

func testIndex() *Index {
	return New([]Item{
		{ID: "case.camel", Name: "camelCase", Group: "Case", Tags: []string{"cc", "lowerCamel"}},
		{ID: "case.snake", Name: "snake_case", Group: "Case", Tags: []string{"sc", "underscore"}},
		{ID: "path.forward", Name: "Backslashes -> forward slashes", Group: "Path", Tags: []string{"unix", "posix"}},
		{ID: "json.pretty", Name: "Pretty-print JSON", Group: "JSON", Tags: []string{"format", "indent"}},
	})
}

func TestEmptyQueryPreservesOrder(t *testing.T) {
	ix := testIndex()
	got := ix.Search("")
	if len(got) != 4 {
		t.Fatalf("got %d items, want 4", len(got))
	}
	// The palette shows this list before anything is typed, so the caller's
	// ordering must survive untouched.
	if got[0].ID != "case.camel" || got[3].ID != "json.pretty" {
		t.Errorf("order changed: %s ... %s", got[0].ID, got[3].ID)
	}
}

func TestSearchMatchesName(t *testing.T) {
	ix := testIndex()
	got := ix.Search("camel")
	if len(got) == 0 || got[0].ID != "case.camel" {
		t.Fatalf("Search(camel) = %v, want case.camel first", ids(got))
	}
}

func TestSearchMatchesTag(t *testing.T) {
	ix := testIndex()
	// "posix" appears only in a tag, never in the visible name.
	got := ix.Search("posix")
	if len(got) == 0 || got[0].ID != "path.forward" {
		t.Fatalf("Search(posix) = %v, want path.forward first", ids(got))
	}
}

func TestSearchMatchesGroup(t *testing.T) {
	ix := testIndex()
	got := ix.Search("json")
	if len(got) == 0 || got[0].ID != "json.pretty" {
		t.Fatalf("Search(json) = %v, want json.pretty first", ids(got))
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	ix := testIndex()
	lower := ids(ix.Search("camel"))
	upper := ids(ix.Search("CAMEL"))
	if lower != upper {
		t.Errorf("case sensitivity leaked: %q vs %q", lower, upper)
	}
}

func TestSearchNoMatch(t *testing.T) {
	if got := testIndex().Search("zzzzqqq"); len(got) != 0 {
		t.Errorf("expected no matches, got %v", ids(got))
	}
}

func TestWhitespaceQueryIsEmptyQuery(t *testing.T) {
	if got := testIndex().Search("   "); len(got) != 4 {
		t.Errorf("whitespace query returned %d items, want all 4", len(got))
	}
}

func ids(items []Item) string {
	s := ""
	for i, it := range items {
		if i > 0 {
			s += ","
		}
		s += it.ID
	}
	return s
}

func TestNameMatchesOutrankContextMatches(t *testing.T) {
	// The shape that made the notes list look broken: a folder whose whole
	// context is one short word, and the notes inside it whose names are what
	// the user is actually typing.
	ix := New([]Item{
		{ID: "notebooks", Name: "notebooks", Group: "", Tags: []string{"notebooks"}},
		{ID: "notebooks/todo.txt", Name: "todo", Group: "notebooks", Tags: []string{"notebooks/todo.txt"}},
	})
	// Both match: the folder on its name, the note on its context. The folder's
	// haystack is far shorter, which is exactly the case where one combined
	// haystack used to bury the note.
	got := ids(ix.Search("noteb"))
	if got != "notebooks,notebooks/todo.txt" {
		t.Errorf("Search(noteb) = %q", got)
	}
	// And a name match beats a context match regardless of length.
	if got := ids(ix.Search("todo")); got != "notebooks/todo.txt" {
		t.Errorf("Search(todo) = %q, want the note itself", got)
	}
}

func TestContextStillMatchesWhenTheNameDoesNot(t *testing.T) {
	ix := New([]Item{
		{ID: "work/vpn.conf", Name: "vpn", Group: "work", Tags: []string{"work/vpn.conf"}},
	})
	if got := ids(ix.Search("work")); got != "work/vpn.conf" {
		t.Errorf("Search(work) = %q, want the note found by its folder", got)
	}
	// And by a path fragment spanning both.
	if got := ids(ix.Search("work/vpn")); got != "work/vpn.conf" {
		t.Errorf("Search(work/vpn) = %q", got)
	}
}
