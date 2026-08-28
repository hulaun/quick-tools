//go:build windows

package ui

import (
	"fmt"
	"strings"

	"github.com/hulaun/quick-tools/internal/snippet"
	"github.com/hulaun/quick-tools/internal/transform"
)

// snippetStore is the subset of snippet.Store the palette needs.
type snippetStore interface {
	Load() []snippet.Snippet
	Changed() bool
}

// reloadSnippets rebuilds the snippet entries in the registry.
//
// A snippet is registered as a Transform whose Run ignores its input and
// returns the file's contents. That looks odd written down, but from the
// palette's point of view pasting stored text and transforming the clipboard
// are the same operation: pick an entry, get a string, put it on the clipboard.
// Modelling it this way means the fuzzy index, the preview pane, the paste path
// and the auto-paste setting all work with no new code.
//
// Must run on the UI thread: it mutates the registry and the fuzzy index.
func (p *Palette) reloadSnippets() {
	items := p.snippets.Load()

	for _, id := range p.snippetIDs {
		p.reg.Remove(id)
	}
	p.snippetIDs = p.snippetIDs[:0]

	for _, s := range items {
		sn := s

		group := "Snippets"
		if sn.Folder != "" {
			// The folder becomes the group, so "work/db-conn.txt" reads as
			// "work: db-conn" and the tree structure is visible in the list.
			group = sn.Folder
		}

		tags := []string{"snippet"}
		tags = append(tags, strings.Split(sn.Folder, "/")...)

		id := "snippet." + sn.ID
		p.reg.Add(transform.Transform{
			ID:    id,
			Name:  sn.Name,
			Group: group,
			Tags:  tags,
			Run: func(string) (string, error) {
				text, err := sn.Text()
				if err != nil {
					return "", fmt.Errorf("could not read %s: %w", sn.ID, err)
				}
				if snippet.LooksBinary(text) {
					return "", fmt.Errorf("%s is not text", sn.ID)
				}
				return text, nil
			},
		})
		p.snippetIDs = append(p.snippetIDs, id)
	}

	p.rebuildIndex()
	if p.shown {
		p.refilter()
	}
}
