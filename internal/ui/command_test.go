//go:build windows

package ui

import "testing"

func TestCommandQuery(t *testing.T) {
	cases := []struct {
		in   string
		rest string
		ok   bool
	}{
		{"", "", false},
		{"auth", "", false},
		{">", "", true},
		{">api", "api", true},
		{"> api ", "api", true},
		// The prefix has to be at the very front. Anywhere else it is a
		// character in a query, and a note called "a > b" has to be findable.
		{"a > b", "", false},
		{"note>", "", false},
	}
	for _, c := range cases {
		rest, ok := commandQuery(c.in)
		if ok != c.ok || rest != c.rest {
			t.Errorf("commandQuery(%q) = %q, %v; want %q, %v", c.in, rest, ok, c.rest, c.ok)
		}
	}
}

// The command list has to find every tab by its own name, or the feature is a
// list you have to read rather than a thing you type at.
func TestCommandIndexFindsEveryTab(t *testing.T) {
	p := &Palette{}
	p.buildCommandIndex()

	if got := p.commands.Len(); got != modeCount {
		t.Fatalf("indexed %d tabs, want %d", got, modeCount)
	}

	for mode := 0; mode < modeCount; mode++ {
		name := tabTitle(mode, false)
		hits := p.commands.Search(name)
		if len(hits) == 0 {
			t.Errorf("%q found nothing", name)
			continue
		}
		r, ok := hits[0].Data.(commandRow)
		if !ok || r.mode != mode {
			t.Errorf("%q ranked %v first, want mode %d", name, hits[0].Name, mode)
		}
	}
}

// An empty command query lists the tabs in tab order, so ">" then Enter goes to
// the first one rather than to whichever the matcher happened to like.
func TestCommandIndexEmptyQueryIsTabOrder(t *testing.T) {
	p := &Palette{}
	p.buildCommandIndex()

	hits := p.commands.Search("")
	if len(hits) != modeCount {
		t.Fatalf("got %d rows, want %d", len(hits), modeCount)
	}
	for i, it := range hits {
		r, ok := it.Data.(commandRow)
		if !ok || r.mode != i {
			t.Errorf("row %d is %v, want mode %d", i, it.Name, i)
		}
	}
}

// The tags are what make a tab findable by what it is for rather than by what
// it is called -- ">login" is a reasonable way to ask for the notes.
func TestCommandIndexFindsTabsByTag(t *testing.T) {
	cases := map[string]int{
		"login":  modeNotes,
		"http":   modeAPI,
		"replay": modeMacros,
		"folder": modePlaces,
		"json":   modeTransforms,
	}
	p := &Palette{}
	p.buildCommandIndex()

	for query, want := range cases {
		hits := p.commands.Search(query)
		if len(hits) == 0 {
			t.Errorf("%q found nothing", query)
			continue
		}
		r, ok := hits[0].Data.(commandRow)
		if !ok || r.mode != want {
			t.Errorf("%q ranked %v first, want %v", query, hits[0].Name, tabTitle(want, false))
		}
	}
}
