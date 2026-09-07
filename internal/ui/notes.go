//go:build windows

package ui

import (
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/snippet"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// snippetStore is the subset of snippet.Store the palette needs.
type snippetStore interface {
	Load() []snippet.Snippet
	Folders() []string
	Changed() bool
	Save(id, text string) error
	CreateNote(folder, name string) (snippet.Snippet, error)
	CreateFolder(parent, name string) (string, error)
	Rename(id, name string) (string, error)
	Delete(id string) error
}

// noteRow is one line in the notes list: either a stored note or the folder
// holding some.
//
// Folders are listed as rows of their own, even though nothing happens when
// one is chosen, because a folder is the target for the next Alt+D -- without
// a row to highlight there would be no way to say where a new note should go,
// and a folder created with Shift+Alt+D would vanish until it had contents.
type noteRow struct {
	id     string // path relative to the snippets root: "work/db-conn.txt"
	name   string // "db-conn"
	folder string // "work", or "" at the root
	path   string // absolute path on disk; empty for a folder
	isDir  bool
}

// target returns the folder a new note or folder should be created in when
// this row is the highlighted one.
func (r noteRow) target() string {
	if r.isDir {
		return r.id
	}
	return r.folder
}

// reloadNotes rebuilds the notes index from the snippets folder.
//
// Notes deliberately do not go through the transform registry any more. Until
// this milestone a snippet was registered as a Transform whose Run ignored its
// input and returned the file, which made pasting stored text and transforming
// the clipboard the same operation and cost no extra code. Editing breaks that
// symmetry: a note has a file behind it that gets written back, and a
// transform has no such thing. So the two now live in separate indexes and the
// palette switches between them.
//
// Must run on the UI thread: it replaces the index the window procedure reads.
func (p *Palette) reloadNotes() {
	var rows []noteRow

	for _, folder := range p.snippets.Folders() {
		rows = append(rows, noteRow{
			id:     folder,
			name:   path.Base(folder),
			folder: path.Dir(folder),
			isDir:  true,
		})
	}
	for _, sn := range p.snippets.Load() {
		rows = append(rows, noteRow{
			id:     sn.ID,
			name:   sn.Name,
			folder: sn.Folder,
			path:   sn.Path,
		})
	}

	// Sorting by the full relative path is what puts a folder immediately above
	// its own notes, so the flat list reads as the tree it came from.
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].id) < strings.ToLower(rows[j].id)
	})

	items := make([]fuzzy.Item, 0, len(rows))
	for _, r := range rows {
		// The name is what gets matched first; the folder and the full path are
		// context, so "work/vpn" finds it but a folder never outranks the notes
		// inside it. Nothing carries a "note" tag: every row had one, so it
		// matched every query it was a subsequence of and did nothing but blur
		// the ranking.
		items = append(items, fuzzy.Item{
			ID: r.id, Name: r.name, Group: r.folder, Tags: []string{r.id}, Data: r,
		})
	}
	p.indexes[modeNotes] = fuzzy.New(items)

	if p.mode == modeNotes {
		// Keep the note that is open open, and land on it directly rather than
		// selecting row 0 first. The store polls for edits and saving is itself an
		// edit, so a second after every Ctrl+S the list is rebuilt underneath the
		// note being written -- passing through another row on the way would
		// reload the file and throw the caret back to the top of it.
		p.refilterKeeping(p.curNote)
	}
}

// noteLabel renders a notes row: "work: db-conn", or "work/" for a folder.
func noteLabel(it fuzzy.Item) string {
	r, ok := it.Data.(noteRow)
	if !ok {
		return it.Name
	}
	if r.isDir {
		if r.folder == "." || r.folder == "" {
			return r.name + "/"
		}
		return r.folder + ": " + r.name + "/"
	}
	if r.folder == "" {
		return r.name
	}
	return r.folder + ": " + r.name
}

// selectedNote returns the highlighted notes row.
func (p *Palette) selectedNote() (noteRow, bool) {
	it, ok := p.selected()
	if !ok {
		return noteRow{}, false
	}
	r, ok := it.Data.(noteRow)
	return r, ok
}

// selectByID moves the highlight to the row with the given id, if it is still
// on screen. Used after a reload or a create, where the list has been rebuilt
// underneath the selection.
func (p *Palette) selectByID(id string) {
	if id == "" {
		return
	}
	for i, it := range p.visible {
		if it.ID == id {
			p.setSelection(i)
			return
		}
	}
}

// showNote puts the highlighted note in the editor.
//
// Reading the file here rather than holding every note in memory is the same
// choice the store makes: these files hold configs and credentials, and only
// one is ever open at a time.
func (p *Palette) showNote() {
	r, ok := p.selectedNote()
	if !ok {
		p.saveIfDirty()
		p.curNote = ""
		p.setEditorText("")
		p.setEditable(false)
		return
	}

	if r.id == p.curNote && !p.dirty {
		// Already showing it. Re-reading would reset the caret to the top for no
		// reason -- which is what the user would see after every save, because a
		// save is a change and the watcher reloads on one.
		return
	}
	p.saveIfDirty()

	if r.isDir {
		p.curNote = ""
		p.setEditorText("")
		p.setEditable(false)
		return
	}

	data, err := os.ReadFile(r.path)
	if err != nil {
		p.curNote = ""
		p.setEditorText("could not read " + r.id + ": " + err.Error())
		p.setEditable(false)
		return
	}
	if snippet.LooksBinary(string(data)) {
		p.curNote = ""
		p.setEditorText(r.id + " is not text")
		p.setEditable(false)
		return
	}

	p.curNote = r.id
	p.setEditorText(string(data))
	p.setEditable(true)
}

// setEditorText replaces the editor contents without marking them dirty.
//
// SetText raises EN_CHANGE exactly like typing does, so the change flag has to
// be suppressed around it or every note would look edited the moment it opened.
func (p *Palette) setEditorText(text string) {
	p.quiet = true
	p.preview.SetText(toCRLF(text))
	p.quiet = false
	p.setDirty(false)
}

// setEditable toggles the editor between a note that can be typed into and a
// read-only preview.
func (p *Palette) setEditable(on bool) {
	ro := win.WPARAM(1)
	if on {
		ro = 0
	}
	p.preview.Hwnd().SendMessage(co.EM_SETREADONLY, ro, 0)
}

// setDirty records whether the editor holds unsaved changes and shows it on the
// tab, which is the only piece of chrome always visible while editing.
func (p *Palette) setDirty(dirty bool) {
	if p.dirty == dirty {
		return
	}
	p.dirty = dirty
	p.tabs[modeNotes].Hwnd().SetWindowText(tabTitle(modeNotes, dirty))
	p.tabs[modeNotes].Hwnd().InvalidateRect(nil, true)
}

// saveNote writes the editor back to disk. It is what Ctrl+S runs.
func (p *Palette) saveNote() {
	if p.mode != modeNotes || p.curNote == "" {
		return
	}
	if err := p.snippets.Save(p.curNote, p.preview.Text()); err != nil {
		p.fatal("Could not save the note", err)
		return
	}
	p.setDirty(false)
}

// saveIfDirty saves unsaved edits before something is about to discard them.
//
// Ctrl+S is the documented way to save, but moving to another note or closing
// the window would otherwise throw the text away silently. Saving is the only
// behaviour that cannot lose work, and these are the user's own configs and
// passwords.
func (p *Palette) saveIfDirty() {
	if p.dirty {
		p.saveNote()
	}
	// The API tab has an editable pane of its own, and the same promise applies
	// to it: switching tab or closing the window is not a way to lose text.
	p.saveRequestIfDirty()
}

// createNote makes a new note, or a new folder, and opens the name box on it.
//
// It lands in the folder of whatever is highlighted, so the folder rows in the
// list double as the place-picker. The name comes from the search box if
// something was typed there -- type what you searched for and did not find,
// press Alt+D, and it exists -- and otherwise from a placeholder that the name
// box then offers, selected, for you to type over.
//
// The file is created before it is named, rather than held pending until the
// name is confirmed. It is the same order Explorer uses, and it keeps every
// other path in the palette from having to know about a note that does not
// exist yet.
func (p *Palette) createNote(asFolder bool) {
	name := strings.TrimSpace(p.search.Text())

	if p.mode != modeNotes {
		p.setMode(modeNotes)
	}

	folder := ""
	if r, ok := p.selectedNote(); ok {
		folder = r.target()
	}
	if folder == "." {
		folder = ""
	}

	p.saveIfDirty()

	var id string
	if asFolder {
		if name == "" {
			name = "New folder"
		}
		newID, err := p.snippets.CreateFolder(folder, name)
		if err != nil {
			p.fatal("Could not create the folder", err)
			return
		}
		id = newID
	} else {
		if name == "" {
			name = "New note"
		}
		sn, err := p.snippets.CreateNote(folder, name)
		if err != nil {
			p.fatal("Could not create the note", err)
			return
		}
		id = sn.ID
	}

	// Clear the search so the new row is definitely in the list: the name was
	// typed as a query, and a query it does not match would hide it.
	p.search.SetText("")
	p.curNote = ""
	p.reloadNotes()
	p.refilter()
	p.selectByID(id)
	p.beginRename()
}

// deleteNote is Del in notes mode.
//
// The delete is permanent -- nothing goes to the Recycle Bin -- so it asks
// first, and says how many notes a folder is taking with it. These are the
// user's configs and passwords: leaving copies in the Bin would defeat the
// point, and deleting them silently would be worse.
func (p *Palette) deleteNote() {
	r, ok := p.selectedNote()
	if !ok {
		return
	}

	what := "the note \"" + r.id + "\""
	if r.isDir {
		what = "the folder \"" + r.id + "\""
		if n := p.countUnder(r.id); n > 0 {
			what += " and the " + strconv.Itoa(n) + " notes in it"
		}
	}
	if !p.confirm("Delete", "Permanently delete "+what+"?\n\n"+
		"This does not go to the Recycle Bin and cannot be undone.") {
		return
	}

	// Drop the editor's claim on the file before it goes, so the save-on-close
	// path cannot write it back out again a moment later.
	if r.id == p.curNote || (r.isDir && strings.HasPrefix(p.curNote, r.id+"/")) {
		p.curNote = ""
		p.setDirty(false)
	}

	if err := p.snippets.Delete(r.id); err != nil {
		p.fatal("Could not delete", err)
		return
	}

	// Stay at the same position in the list rather than jumping to the top, so
	// clearing several notes out is one key pressed repeatedly.
	at := p.selIdx
	p.reloadNotes()
	p.refilter()
	if at >= len(p.visible) {
		at = len(p.visible) - 1
	}
	if at >= 0 {
		p.setSelection(at)
	}
}

// countUnder is how many notes live inside a folder, at any depth. It counts
// from the index already in memory rather than walking the disk again.
func (p *Palette) countUnder(folder string) int {
	idx := p.indexes[modeNotes]
	if idx == nil {
		return 0
	}
	n := 0
	for _, it := range idx.Search("") {
		r, ok := it.Data.(noteRow)
		if ok && !r.isDir && strings.HasPrefix(r.id, folder+"/") {
			n++
		}
	}
	return n
}

// applyNote is Enter in notes mode: the note goes to the clipboard and the
// palette gets out of the way, which is the Win+V replacement this was built
// for.
//
// It copies what is in the editor rather than what is on disk, so an edit made
// and not yet saved is still what gets pasted -- the visible text is the
// promise.
func (p *Palette) applyNote() {
	r, ok := p.selectedNote()
	if !ok || r.isDir {
		return
	}
	text := strings.TrimRight(fromCRLF(p.preview.Text()), "\r\n")

	p.saveIfDirty()

	if err := winapi.SetClipboardText(text); err != nil {
		p.setEditorText("could not write to clipboard: " + err.Error())
		return
	}

	p.hide(true)
	if p.cfg.AutoPaste {
		winapi.SendPaste()
	}
}
