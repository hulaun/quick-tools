// Package fuzzy ranks palette entries against what the user has typed.
//
// It sits between the transform registry and the palette UI so that both
// built-in transforms and, later, snippets are searched through one path and
// ranked on the same scale.
package fuzzy

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

// Item is anything the palette can list and run.
type Item struct {
	ID    string
	Name  string // shown in the list
	Group string // dim prefix, e.g. "Case"
	Tags  []string
	Data  any // the underlying *transform.Transform or note row

	// name and context are the two things matching runs against, kept apart on
	// purpose: what the entry is called, and everything else that describes it.
	// Both are built once at index time rather than per keystroke.
	name    string
	context string
}

// Index is a searchable set of items, held in the caller's preferred order.
type Index struct {
	items []Item
}

// A search runs in two passes: names first, then the rest.
//
// One combined haystack ranks badly as soon as the entries have context worth
// searching. The generic scorer rewards a short haystack, so a folder called
// "work" -- three words of context and nothing else -- outranks every note
// inside it for almost any query, and the notes look like they are not being
// searched at all. Matching what things are *called* first, and only then what
// they sit in, is both more predictable and what people mean when they type.
const (
	passName = iota
	passContext
	passCount
)

// New builds an index. The order given is the order shown when the query is
// empty, so callers should pass items already sorted the way they want.
func New(items []Item) *Index {
	idx := &Index{items: make([]Item, len(items))}
	copy(idx.items, items)
	for i := range idx.items {
		it := &idx.items[i]
		it.name = strings.ToLower(it.Name)
		it.context = strings.ToLower(strings.Join(append([]string{it.Group}, it.Tags...), " "))
	}
	return idx
}

// Len reports how many items are indexed.
func (ix *Index) Len() int { return len(ix.items) }

// String satisfies fuzzy.Source for the name pass, letting us match without
// building a parallel []string on every keystroke.
func (ix *Index) String(i int) string { return ix.items[i].name }

// Search returns the items matching query, best first. An empty query returns
// everything in the original order, which is what makes the palette useful the
// instant it opens, before anything is typed.
func (ix *Index) Search(query string) []Item {
	query = strings.TrimSpace(query)
	if query == "" {
		out := make([]Item, len(ix.items))
		copy(out, ix.items)
		return out
	}

	query = strings.ToLower(query)
	out := make([]Item, 0, len(ix.items))
	seen := make(map[int]bool, len(ix.items))

	for pass := 0; pass < passCount; pass++ {
		for _, m := range fuzzy.FindFrom(query, sourceAdapter{ix, pass}) {
			if seen[m.Index] {
				continue // already matched on its name, and ranked there
			}
			seen[m.Index] = true
			out = append(out, ix.items[m.Index])
		}
	}
	return out
}

// sourceAdapter exposes one of the two haystacks to the matcher. It exists
// because fuzzy.Source needs Len(), and Index.Len is already part of our own
// API with the same signature -- this keeps both.
type sourceAdapter struct {
	ix   *Index
	pass int
}

func (s sourceAdapter) String(i int) string {
	if s.pass == passName {
		return s.ix.items[i].name
	}
	return s.ix.items[i].context
}

func (s sourceAdapter) Len() int { return len(s.ix.items) }
