//go:build windows

package ui

import (
	"path"
	"strings"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"
)

// renameState is the in-place name box: which row it is over, and whether it
// is up at all.
type renameState struct {
	active bool
	id     string // the note, folder or place being named
	isDir  bool

	// A macro is renamed through the same box -- but it has no file behind it,
	// so the new name is written into macros.json rather than applied with
	// os.Rename. entryIdx is its position in that file and not the row: the list
	// is sorted for reading and its order is not the file's.
	isMacro  bool
	entryIdx int
	was      string // the name the box opened with, to spot "nothing changed"

	// isRequest sends the rename to the requests tree instead of the notes one.
	// Both are files under a snippet.Store, so the work is identical -- only the
	// store and the index to rebuild afterwards differ.
	isRequest bool
}

// beginRename floats the name box over the highlighted row, pre-filled and
// selected, the way F2 works everywhere else.
//
// A new note goes through here too. It exists on disk with a placeholder name
// by the time the box appears, which is what Explorer does as well: the file is
// real, and naming it is a rename that has been started for you. The
// alternative -- holding a nameless pending file until Enter -- means every
// other path in the palette has to know about a note that is not there yet.
func (p *Palette) beginRename() {
	var st renameState
	var show string

	switch p.mode {
	case modeNotes:
		r, ok := p.selectedNote()
		if !ok {
			return
		}
		p.saveIfDirty()
		st = renameState{active: true, id: r.id, isDir: r.isDir, was: r.name}
		show = r.name
	case modeMacros:
		r, ok := p.selectedMacro()
		if !ok {
			return
		}
		st = renameState{active: true, isMacro: true, entryIdx: r.Index, was: r.Name}
		show = r.Name
	case modeAPI:
		r, ok := p.selectedRequest()
		if !ok {
			return
		}
		p.saveRequestIfDirty()
		st = renameState{active: true, isRequest: true, id: r.id, isDir: r.isDir, was: r.name}
		show = r.name
	default:
		return
	}

	rc, ok := p.rowRect(p.selIdx)
	if !ok {
		return
	}
	p.renaming = st

	h := p.nameEdit.Hwnd()
	h.SetWindowPos(win.HWND(0), // HWND_TOP: above the list it is sitting on
		win.POINT{X: rc.Left, Y: rc.Top},
		win.SIZE{Cx: rc.Right - rc.Left, Cy: rc.Bottom - rc.Top},
		co.SWP_SHOWWINDOW)

	// The name shown is the name in the list: no extension, no folder path.
	// Whatever the user types replaces exactly what they can see.
	p.nameEdit.SetText(show)
	selectAll(h)
	h.SetFocus()
}

// rowRect is the highlighted row's rectangle in the parent window's
// coordinates, which is what the name box needs to sit exactly on top of it.
func (p *Palette) rowRect(i int) (win.RECT, bool) {
	if i < 0 || i >= len(p.visible) {
		return win.RECT{}, false
	}

	// LVM_GETITEMRECT takes the row index in the rect's own Left field, which is
	// one of the odder corners of the list view API.
	rc := win.RECT{Left: int32(co.LVIR_BOUNDS)}
	ok, err := p.list.Hwnd().SendMessage(co.LVM_GETITEMRECT,
		win.WPARAM(i), win.LPARAM(unsafe.Pointer(&rc)))
	if err != nil || ok == 0 {
		return win.RECT{}, false
	}

	// The rectangle comes back relative to the list's client area, and the name
	// box is a sibling of the list positioned in the parent's. The list has no
	// border and no non-client edge, so its client origin is exactly where the
	// layout constants put it.
	x, y := ui.Dpi(pad, listTop)
	rc.Left += int32(x)
	rc.Right += int32(x)
	rc.Top += int32(y)
	rc.Bottom += int32(y)

	// A row's bounds run the full width of the list even when the text does not.
	if limit := int32(x + ui.DpiX(listW)); rc.Right > limit {
		rc.Right = limit
	}
	return rc, true
}

// commitRename applies what was typed, or quietly does nothing if it is the
// same name.
func (p *Palette) commitRename() {
	if !p.renaming.active {
		return
	}
	name := strings.TrimSpace(p.nameEdit.Text())
	st := p.renaming
	p.endRename()

	if st.isMacro {
		p.commitMacroRename(st, name)
		return
	}

	current := path.Base(st.id)
	if !st.isDir {
		current = strings.TrimSuffix(current, path.Ext(current))
	}
	if name == "" || name == current {
		p.selectByID(st.id)
		p.focusAfterRename(st)
		return
	}

	store := p.snippets
	if st.isRequest {
		store = p.requests
	}

	newID, err := store.Rename(st.id, name)
	if err != nil {
		// Worth interrupting for: a rename that collides has done nothing, and
		// the name the user chose is still on screen to be corrected.
		p.fatal("Could not rename", err)
		p.selectByID(st.id)
		return
	}

	// The file the pane is holding has moved, so it is reopened under the new id
	// rather than saved back to a path that no longer exists.
	if st.isRequest {
		p.curRequest = ""
		p.reloadRequests()
	} else {
		p.curNote = ""
		p.reloadNotes()
	}
	p.refilter()
	p.selectByID(newID)
	p.focusAfterRename(renameState{id: newID, isDir: st.isDir, isRequest: st.isRequest})
}


// cancelRename drops the name box without touching the file. The placeholder
// name a new note was created with is left in place, exactly as Esc leaves
// "New folder" in Explorer.
func (p *Palette) cancelRename() {
	if !p.renaming.active {
		return
	}
	st := p.renaming
	p.endRename()
	if st.isMacro {
		p.selectByMacroIndex(st.entryIdx)
		p.search.Hwnd().SetFocus()
		return
	}
	p.selectByID(st.id)
	p.focusAfterRename(st)
}

// endRename hides the box. The active flag is cleared first so that the focus
// it is about to lose does not come back round as a second commit.
func (p *Palette) endRename() {
	p.renaming.active = false
	h := p.nameEdit.Hwnd()
	h.ShowWindow(co.SW_HIDE)
	h.SetWindowPos(win.HWND(0), win.POINT{X: 0, Y: -1000}, win.SIZE{},
		co.SWP_NOSIZE|co.SWP_NOZORDER|co.SWP_NOACTIVATE)
}

// focusAfterRename puts the caret where the user is most likely going next: the
// body of a note they have just named, or the search box for a folder.
func (p *Palette) focusAfterRename(st renameState) {
	if st.isDir {
		p.search.Hwnd().SetFocus()
		return
	}
	if st.isRequest {
		p.setAPIFocus(apiFocusRequest)
		return
	}
	p.preview.Hwnd().SetFocus()
}

// renameEvents wires the name box up.
func (p *Palette) renameEvents() {
	p.nameEdit.OnSubclass().Wm(co.WM_KEYDOWN, func(m ui.Wm) uintptr {
		switch co.VK(m.WParam) {
		case co.VK_RETURN:
			p.commitRename()
			return 0
		case co.VK_ESCAPE:
			p.cancelRename()
			return 0
		}
		if p.editKeys(p.nameEdit.Hwnd(), co.VK(m.WParam)) {
			return 0
		}
		return p.nameEdit.Hwnd().DefSubclassProc(co.WM_KEYDOWN, m.WParam, m.LParam)
	})

	p.nameEdit.OnSubclass().Wm(co.WM_CHAR, func(m ui.Wm) uintptr {
		// Enter and Esc reach an Edit as characters too, and an Edit answers a
		// character it cannot use with a beep.
		if ch := uint16(m.WParam); ch == '\r' || ch == '\n' || ch == 0x1b || editChars(ch) {
			return 0
		}
		return p.nameEdit.Hwnd().DefSubclassProc(co.WM_CHAR, m.WParam, m.LParam)
	})

	// Clicking elsewhere keeps the name, which is what every file manager does.
	p.nameEdit.On().EnKillFocus(func() { p.commitRename() })
}
