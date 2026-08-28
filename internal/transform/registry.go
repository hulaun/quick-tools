// Package transform holds the text transformations the palette can apply.
//
// Built-in transforms and user-written JavaScript transforms are both
// represented by the same Transform value, so nothing downstream -- the fuzzy
// index, the palette list, the preview pane -- needs to know which is which.
package transform

import "sort"

// Transform is one entry in the palette.
type Transform struct {
	ID    string   // stable identifier, e.g. "case.camel"
	Name  string   // shown in the palette, e.g. "camelCase"
	Group string   // shown as a dim prefix, e.g. "Case"
	Tags  []string // extra fuzzy-match aliases, e.g. "cc"
	Run   func(string) (string, error)
}

// Registry is an ordered, id-addressed set of transforms.
type Registry struct {
	byID  map[string]*Transform
	order []*Transform
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]*Transform)}
}

// Add inserts t, replacing any existing transform with the same ID. Replacing
// is what makes script hot-reload work: the loader re-adds a script under the
// same ID and the palette picks up the new body immediately.
func (r *Registry) Add(t Transform) {
	c := t
	if old, ok := r.byID[t.ID]; ok {
		*old = c
		return
	}
	r.byID[c.ID] = &c
	r.order = append(r.order, &c)
}

// Remove drops a transform, used when a script file is deleted.
func (r *Registry) Remove(id string) {
	if _, ok := r.byID[id]; !ok {
		return
	}
	delete(r.byID, id)
	for i, t := range r.order {
		if t.ID == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Get returns the transform with the given ID.
func (r *Registry) Get(id string) (*Transform, bool) {
	t, ok := r.byID[id]
	return t, ok
}

// All returns every transform, sorted by group then name for a stable palette
// order when no search query is active.
func (r *Registry) All() []*Transform {
	out := make([]*Transform, len(r.order))
	copy(out, r.order)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Registry) Len() int { return len(r.order) }

// pure adapts a plain string->string function to the Transform.Run signature.
// Most built-ins cannot fail, and this keeps them free of `, nil` noise.
func pure(f func(string) string) func(string) (string, error) {
	return func(s string) (string, error) { return f(s), nil }
}

// RegisterBuiltins fills r with every transform compiled into the binary.
func RegisterBuiltins(r *Registry) {
	registerCases(r)
	registerPaths(r)
	registerJSON(r)
	registerEncoding(r)
	registerLines(r)
	registerMisc(r)
}
