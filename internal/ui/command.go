//go:build windows

package ui

import (
	"strings"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/fuzzy"
)

// The command line: type ">" at the front of the query, or press Ctrl+Shift+P,
// and the list stops showing the tab's entries and starts showing the tabs
// themselves. Enter goes to the one highlighted.
//
// It exists because the hotkey cycles forwards only, so reaching the fifth tab
// from the first is four presses and a count in your head. ">api" is one
// thought. VS Code's ">" is deliberately the same key for the same reason --
// nobody has to be told what it does.
//
// It is a *view over the search box*, not a sixth mode. p.mode does not change
// while it is showing, so the tab you came from stays lit and the right-hand
// pane keeps whatever it was holding; nothing is rebuilt, saved or discarded
// for a prefix that may be gone again on the next keystroke.
const commandPrefix = ">"

// commandQuery splits a query into the part that follows the prefix. ok is
// false for an ordinary query, which is every query that does not begin with
// ">" -- the prefix has to be at the very front, so a ">" typed inside a search
// for a note is still a character.
func commandQuery(text string) (rest string, ok bool) {
	if !strings.HasPrefix(text, commandPrefix) {
		return "", false
	}
	return strings.TrimSpace(text[len(commandPrefix):]), true
}

// commanding reports whether the palette is currently showing the command list
// rather than the mode's own entries.
func (p *Palette) commanding() bool {
	_, ok := commandQuery(p.search.Text())
	return ok
}

// commandTags is what a tab matches on besides its name. The point is that the
// words you would reach for describe the tab even when they are not its label:
// ">config" and ">login" should both find Notes.
func commandTags(mode int) []string {
	switch mode {
	case modeTransforms:
		return []string{"case", "json", "encode", "decode", "path", "lines", "text", "clipboard"}
	case modeNotes:
		return []string{"snippets", "config", "login", "password", "scratch"}
	case modeMacros:
		return []string{"keystrokes", "record", "replay", "keys"}
	case modeAPI:
		return []string{"http", "request", "rest", "curl", "endpoint"}
	}
	return nil
}

// buildCommandIndex builds the searchable list of tabs. It is built once, at
// startup: the tabs are fixed and their names do not depend on anything that
// changes, unlike the dirty marker in tabTitle -- a "Notes *" that only matched
// "notes" some of the time would be a worse list, not a livelier one.
func (p *Palette) buildCommandIndex() {
	items := make([]fuzzy.Item, 0, modeCount)
	for mode := 0; mode < modeCount; mode++ {
		items = append(items, fuzzy.Item{
			ID:    tabTitle(mode, false),
			Name:  tabTitle(mode, false),
			Group: "Tab",
			Tags:  commandTags(mode),
			Data:  commandRow{mode: mode},
		})
	}
	p.commands = fuzzy.New(items)
}

// commandRow is what a command list row carries.
type commandRow struct{ mode int }

// enterCommand is Ctrl+Shift+P: put the prefix in the box, keeping nothing
// after it. The query it replaces is remembered for the tab first, so Esc can
// put it back.
func (p *Palette) enterCommand() {
	if p.commanding() {
		return
	}
	p.rememberQuery()
	p.search.SetText(commandPrefix)
	setCaretToEnd(p.search.Hwnd())
	p.refilter()
}

// leaveCommand is Esc while the command list is showing: back to the query the
// tab was being searched with, rather than out of the palette altogether. A
// prefix typed by accident should cost one key to undo, not the window.
func (p *Palette) leaveCommand() bool {
	if !p.commanding() {
		return false
	}
	p.restoreQuery()
	return true
}

// runCommand is Enter on a command row: go to that tab.
func (p *Palette) runCommand() {
	it, ok := p.selected()
	if !ok {
		return
	}
	r, ok := it.Data.(commandRow)
	if !ok {
		return
	}
	// The prefix must not be saved as the tab's remembered query -- the query
	// being left behind is the one stashed when the prefix was typed, and
	// setMode is about to restore the target tab's own.
	p.search.SetText("")
	p.setMode(r.mode)
}

// Remembered queries.
//
// Closing the palette used to clear the box, so finding the same note twice
// meant typing the same thing twice -- and copying one part of a config,
// pasting it and coming back for the next part is the single most common thing
// this app is used for. The query is kept per tab, because a query typed
// against one tab's entries means nothing in another's.
//
// It is restored *selected*, which is what makes it free rather than something
// to clear: the list is already filtered to what you were looking at, and the
// first character you type replaces the lot.

// rememberQuery stores the current query against the current tab. It is a no-op
// while the command list is showing -- ">ap" is a way of getting somewhere, not
// a search anyone wants handed back.
func (p *Palette) rememberQuery() {
	if p.commanding() {
		return
	}
	p.queries[p.mode] = p.search.Text()
}

// restoreQuery puts the current tab's remembered query back in the box, wholly
// selected, and refilters to match.
func (p *Palette) restoreQuery() {
	p.search.SetText(p.queries[p.mode])
	p.refilter()
	selectAllText(p.search.Hwnd())
}

// selectAllText selects everything in an edit, so the next character typed
// replaces it. EM_SETSEL reads an end of -1 as "to the end"; its parameters are
// unsigned, so that is spelled as the bit pattern rather than as a negative.
func selectAllText(h win.HWND) {
	h.SendMessage(co.EM_SETSEL, 0, win.LPARAM(allBits))
}

// setCaretToEnd puts the caret after the last character with nothing selected.
// Both ends are clamped to the length of the text, so a number no query will
// ever reach means the end without having to measure it -- and it has to be
// that rather than the -1 that selectAllText uses, because -1 as the *start*
// means "deselect" and says nothing about where the caret lands.
func setCaretToEnd(h win.HWND) {
	h.SendMessage(co.EM_SETSEL, win.WPARAM(pastEnd), win.LPARAM(pastEnd))
}

const (
	allBits = ^uintptr(0) // -1 as EM_SETSEL's unsigned parameters carry it
	pastEnd = 0x7fffffff  // further than any query goes
)
