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
