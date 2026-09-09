//go:build windows

package ui

import (
	"time"

	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/script"
	"github.com/hulaun/quick-tools/internal/transform"
)

// scriptPollInterval is how often the scripts and snippets folders are checked
// for edits.
//
// A real filesystem watcher would be tidier, but it is another dependency and
// another failure mode for something that only has to feel immediate to a
// person saving a file. A second is well under that threshold.
const scriptPollInterval = time.Second

// reloadScripts rebuilds the script entries in the registry.
//
// It must run on the UI thread: it mutates the registry and the fuzzy index,
// which the window procedure reads while painting.
func (p *Palette) reloadScripts() {
	entries := p.loader.Load()

	// Drop the previous generation first, so a script that has been deleted or
	// renamed does not linger in the palette.
	for _, id := range p.scriptIDs {
		p.reg.Remove(id)
	}
	p.scriptIDs = p.scriptIDs[:0]

	for _, e := range entries {
		entry := e
		p.reg.Add(transform.Transform{
			ID:    entry.ID,
			Name:  entry.Name,
			Group: entry.Group,
			Tags:  entry.Tags,
			Run:   entry.Run,
		})
		p.scriptIDs = append(p.scriptIDs, entry.ID)
	}

	p.rebuildIndex()

	// If the palette is open while a script is saved, refresh what is on screen
	// so the edit is visible without closing and reopening -- staying on the row
	// that was highlighted.
	//
	// Only when that is the list actually on screen. The watcher reloads both
	// sources together, and saving a note is itself a change, so a refilter here
	// would rebuild the notes list one second after every Ctrl+S and drop the
	// selection back to the first row -- with reloadNotes then faithfully
	// preserving the wrong row.
	if p.shown && p.mode == modeTransforms {
		p.refilterKeeping(p.currentID())
	}
}

// watchSources polls both folders for edits and asks the UI thread to reload.
//
// The goroutine only ever signals; it never touches the registry itself. That
// keeps every mutation on the UI thread and avoids needing a lock around the
// palette's state.
func (p *Palette) watchSources(hwnd win.HWND) {
	go func() {
		for range time.Tick(scriptPollInterval) {
			// Every source is asked, and none short-circuits: || would stop at the
			// first one that changed and leave the others' timestamps unread, so
			// the next tick would report them as changed all over again.
			scripts, notes := p.loader.Changed(), p.snippets.Changed()
			macros, requests := p.macros.Changed(), p.requestsChanged()
			env := p.envChanged()
			if scripts || notes || macros || requests || env {
				hwnd.PostMessage(wmSourcesChanged, 0, 0)
			}
		}
	}()
}

// scriptLoader is the subset of script.Loader the palette needs, so tests and
// callers are not forced to build a real one.
type scriptLoader interface {
	Load() []script.Entry
	Changed() bool
}
