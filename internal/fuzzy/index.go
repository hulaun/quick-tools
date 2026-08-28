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
	Data  any // the underlying *transform.Transform or snippet

	// haystack is what matching actually runs against: the name, group and tags
	// joined. Built once at index time rather than per keystroke.
	haystack string
}

// Index is a searchable set of items, held in the caller's preferred order.
type Index struct {
	items []Item
}

// New builds an index. The order given is the order shown when the query is
// empty, so callers should pass items already sorted the way they want.
func New(items []Item) *Index {
	idx := &Index{items: make([]Item, len(items))}
	copy(idx.items, items)
	for i := range idx.items {
		it := &idx.items[i]
		parts := append([]string{it.Name, it.Group}, it.Tags...)
		it.haystack = strings.ToLower(strings.Join(parts, " "))
	}
	return idx
}

// Len reports how many items are indexed.
func (ix *Index) Len() int { return len(ix.items) }

// String and LenSource satisfy fuzzy.Source, letting us match without building
// a parallel []string on every keystroke.
func (ix *Index) String(i int) string { return ix.items[i].haystack }

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

	matches := fuzzy.FindFrom(strings.ToLower(query), sourceAdapter{ix})
	out := make([]Item, 0, len(matches))
	for _, m := range matches {
		out = append(out, ix.items[m.Index])
	}
	return out
}

// sourceAdapter exists because fuzzy.Source needs Len(), and Index.Len is
// already part of our own API with the same signature -- this keeps both.
type sourceAdapter struct{ ix *Index }

func (s sourceAdapter) String(i int) string { return s.ix.String(i) }
func (s sourceAdapter) Len() int            { return len(s.ix.items) }
