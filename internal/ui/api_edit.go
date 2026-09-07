//go:build windows

package ui

import (
	"os"
	"strconv"
	"strings"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/api"
)

// Editing in the API tab: the request pane, and the environments file it
// borrows that pane to show.

// requestExt is what a new request file is called. The extension is what makes
// the file mean something to other tools, so unlike a note's .txt it is not
// really optional.
const requestExt = ".http"

// newRequestTemplate is what a freshly created request starts with.
//
// Not empty. An empty file parses as an error, so a new request would open
// showing a complaint rather than an invitation -- and the one thing someone
// creating a request already knows is that they are about to write a URL.
const newRequestTemplate = "GET {{base}}/\nAccept: application/json\n"

// newEnvTemplate is offered when there is no environments file yet. The file
// not existing is the ordinary state before the first environment is written,
// and an empty pane would not say what belongs in it.
const newEnvTemplate = `{
  "local": {
    "base": "http://localhost:8080",
    "user": "admin"
  },
  "dev": {
    "base": "https://api.dev.internal",
    "user": "svc-dev"
  }
}
`

// setReqEditable toggles the request pane between a read-only view and
// something that can be typed into.
func (p *Palette) setReqEditable(on bool) {
	ro := win.WPARAM(1)
	if on {
		ro = 0
	}
	p.reqPane.Hwnd().SendMessage(co.EM_SETREADONLY, ro, 0)
}

// setReqDirty records unsaved edits and shows them on the tab, which is the
// only chrome always visible while typing into the pane.
func (p *Palette) setReqDirty(dirty bool) {
	if p.reqDirty == dirty {
		return
	}
	p.reqDirty = dirty
	p.tabs[modeAPI].Hwnd().SetWindowText(tabTitle(modeAPI, dirty))
	p.tabs[modeAPI].Hwnd().InvalidateRect(nil, true)
}

// saveRequest writes the request pane back to disk. It is what Ctrl+S runs in
// this mode -- and it saves the environments instead when that is what the
// pane is currently holding.
func (p *Palette) saveRequest() {
	if p.envEditing {
		p.saveEnv()
		return
	}
	if p.curRequest == "" {
		return
	}
	text := fromCRLF(p.reqPane.Text())
	if err := p.requests.Save(p.curRequest, text); err != nil {
		p.fatal("Could not save the request", err)
		return
	}
	p.setReqDirty(false)
	// The saved text is the new truth, so the summary is recomputed from it: a
	// URL that has just been edited should say where it now points.
	p.setStatus(p.summarise(text))
}

// saveRequestIfDirty saves before something is about to discard the edits.
//
// The same rule notes follow: closing the window, switching tab and moving to
// another request are none of them a decision to throw text away.
func (p *Palette) saveRequestIfDirty() {
	if p.reqDirty {
		p.saveRequest()
	}
}

// createRequest is Alt+D and Shift+Alt+D in this tab.
//
// It mirrors createNote -- same store, same create-then-name flow, so the file
// is real by the time the name box opens over it -- differing only in the
// extension and in starting the file from a template rather than empty.
func (p *Palette) createRequest(asFolder bool) {
	name := strings.TrimSpace(p.search.Text())

	folder := ""
	if r, ok := p.selectedRequest(); ok {
		if r.isDir {
			folder = r.id
		} else {
			folder = r.folder
		}
	}
	if folder == "." {
		folder = ""
	}

	p.saveRequestIfDirty()
	p.stopEditingEnv()

	var id string
	if asFolder {
		if name == "" {
			name = "New folder"
		}
		newID, err := p.requests.CreateFolder(folder, name)
		if err != nil {
			p.fatal("Could not create the folder", err)
			return
		}
		id = newID
	} else {
		if name == "" {
			name = "New request"
		}
		// CreateNote supplies its own extension only when the name has none, so
		// naming the file here is all it takes to get .http rather than .txt.
		sn, err := p.requests.CreateNote(folder, name+requestExt)
		if err != nil {
			p.fatal("Could not create the request", err)
			return
		}
		id = sn.ID
		if err := p.requests.Save(id, newRequestTemplate); err != nil {
			p.fatal("Could not write the new request", err)
			return
		}
	}

	// Clear the search so the new row is definitely in the list: the name was
	// typed as a query, and a query it does not match would hide it.
	p.search.SetText("")
	p.curRequest = ""
	p.reloadRequests()
	p.refilter()
	p.selectByID(id)
	p.beginRename()
}

// deleteRequest is Del in this tab.
//
// A request is a file, so this follows the notes rule rather than the places
// one: the thing itself goes, permanently, and it asks first.
func (p *Palette) deleteRequest() {
	r, ok := p.selectedRequest()
	if !ok {
		return
	}

	what := "the request \"" + r.id + "\""
	if r.isDir {
		what = "the folder \"" + r.id + "\""
		if n := p.countRequestsUnder(r.id); n > 0 {
			what += " and the " + strconv.Itoa(n) + " requests in it"
		}
	}
	if !p.confirm("Delete", "Permanently delete "+what+"?\n\n"+
		"This does not go to the Recycle Bin and cannot be undone.") {
		return
	}

	// Drop the pane's claim on the file before it goes, so the save-on-close
	// path cannot write it back out again a moment later.
	if r.id == p.curRequest || (r.isDir && strings.HasPrefix(p.curRequest, r.id+"/")) {
		p.curRequest = ""
		p.setReqDirty(false)
	}

	if err := p.requests.Delete(r.id); err != nil {
		p.fatal("Could not delete", err)
		return
	}

	// Stay at the same position rather than jumping to the top, so clearing
	// several out is one key pressed repeatedly.
	at := p.selIdx
	p.reloadRequests()
	p.refilter()
	if at >= len(p.visible) {
		at = len(p.visible) - 1
	}
	if at >= 0 {
		p.setSelection(at)
	}
}

// countRequestsUnder is how many requests live inside a folder, at any depth.
// Counted from the index already in memory rather than by walking the disk.
func (p *Palette) countRequestsUnder(folder string) int {
	idx := p.indexes[modeAPI]
	if idx == nil {
		return 0
	}
	n := 0
	for _, it := range idx.Search("") {
		r, ok := it.Data.(apiRow)
		if ok && !r.isDir && strings.HasPrefix(r.id, folder+"/") {
			n++
		}
	}
	return n
}

// toggleEnvEditor is Ctrl+Shift+E: it borrows the request pane to show
// env.json, and gives it back.
//
// A borrowed pane rather than a fourth control, because the environment is
// edited rarely and a panel of its own would cost the requests list a quarter
// of the only column it has. The status strip says which of the two the pane
// is holding, since the pane itself looks identical either way.
func (p *Palette) toggleEnvEditor() {
	if p.envEditing {
		p.saveRequestIfDirty() // Ctrl+S is the documented save; leaving is not a discard
		p.stopEditingEnv()
		// Put the request back. curRequest is cleared first because showRequest
		// returns early when it believes the pane already holds that request.
		p.curRequest = ""
		p.showRequest()
		return
	}

	if p.envPath == "" {
		p.setStatus("no environments file is configured")
		return
	}

	p.saveRequestIfDirty()

	data, err := os.ReadFile(p.envPath)
	if os.IsNotExist(err) {
		data = []byte(newEnvTemplate)
	} else if err != nil {
		p.setStatus("could not read " + p.envPath + ": " + err.Error())
		return
	}

	p.envEditing = true
	p.curRequest = ""
	p.setPaneText(p.reqPane, string(data))
	p.setReqEditable(true)
	p.setPaneText(p.respPane, "")
	p.setStatus("editing " + p.envPath + "  --  Ctrl+S saves, Ctrl+Shift+E goes back")
}

// stopEditingEnv leaves the environment editor.
func (p *Palette) stopEditingEnv() {
	if !p.envEditing {
		return
	}
	p.envEditing = false
	p.setReqDirty(false)
}

// saveEnv writes the environments file and reloads it.
//
// It is written first and parsed after, deliberately. Refusing to save text
// that does not parse would mean a half-finished edit could not be put down,
// which is the wrong trade for a file you are typing JSON into -- but the
// result is reported, because an env.json that saved cleanly and then silently
// stopped resolving would look like the substitution being broken.
func (p *Palette) saveEnv() {
	text := fromCRLF(p.reqPane.Text())

	// 0600: this file holds hostnames and, in practice, credentials.
	if err := os.WriteFile(p.envPath, []byte(text), 0o600); err != nil {
		p.fatal("Could not save the environments", err)
		return
	}
	p.setReqDirty(false)

	fresh, err := api.LoadEnv(p.envPath)
	if err != nil {
		p.envErr = err
		p.updateStripLabel()
		p.setStatus("saved, but it does not parse: " + err.Error())
		return
	}
	p.envErr = nil
	fresh.Adopt(p.env)
	p.env = fresh
	p.updateStripLabel()

	names := p.env.Names()
	if len(names) == 0 {
		p.setStatus("saved -- but there are no environments in it")
		return
	}
	p.setStatus("saved  --  " + strings.Join(names, ", ") + "  (Ctrl+E cycles)")
}
