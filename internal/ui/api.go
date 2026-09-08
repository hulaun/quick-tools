//go:build windows

package ui

import (
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"

	"github.com/hulaun/quick-tools/internal/api"
	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// Which of the API tab's three regions the keyboard is in.
//
// Tab cycles them. These are real Win32 focus changes, which places mode
// deliberately avoids -- but the thing places moves to is a second list view,
// and a focused list view selects a row, and a selected row ignores custom draw
// and paints itself in the system accent colour. The API panes are Edits, like
// the note editor that already takes focus on Tab, so none of that applies.
const (
	apiFocusList = iota
	apiFocusRequest
	apiFocusResponse
)

// apiRow is one line in the requests list: a saved .http file, or a folder
// holding some.
//
// Structurally the same as a note row, because both come from the same tree
// walker. Kept separate anyway: the two lists have different labels, different
// Enter behaviour, and a request will grow fields a note has no use for.
type apiRow struct {
	id     string // path relative to the requests root: "auth/login.http"
	name   string // "login"
	folder string // "auth", or "" at the root
	path   string // absolute path on disk; empty for a folder
	isDir  bool
}

// reloadRequests rebuilds the API index from the requests folder.
//
// Must run on the UI thread: it replaces an index the window procedure reads.
func (p *Palette) reloadRequests() {
	if p.requests == nil {
		return
	}

	var rows []apiRow
	for _, folder := range p.requests.Folders() {
		rows = append(rows, apiRow{
			id:     folder,
			name:   path.Base(folder),
			folder: path.Dir(folder),
			isDir:  true,
		})
	}
	for _, sn := range p.requests.Load() {
		// A project's env.json lives among the requests it serves, which is what
		// makes it travel with them -- but it is not one, and listing it would
		// put a row called "env" in every project and match it on every search
		// for something else. Alt+E is how it is reached.
		if path.Base(sn.ID) == api.EnvFileName {
			continue
		}
		rows = append(rows, apiRow{
			id:     sn.ID,
			name:   sn.Name,
			folder: sn.Folder,
			path:   sn.Path,
		})
	}

	// Sorted by the full relative path, so a folder sits immediately above its
	// own requests and the flat list reads as the tree it came from.
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].id) < strings.ToLower(rows[j].id)
	})

	items := make([]fuzzy.Item, 0, len(rows))
	for _, r := range rows {
		items = append(items, fuzzy.Item{
			ID: r.id, Name: r.name, Group: r.folder, Tags: []string{r.id}, Data: r,
		})
	}
	p.indexes[modeAPI] = fuzzy.New(items)

	// Only when this tab is the one on screen. The watcher reloads every source
	// together, so refiltering unconditionally would rebuild this list -- and
	// drop its highlight -- every time a note or a script was saved.
	if p.mode == modeAPI {
		p.refilterKeeping(p.curRequest)
	}
}

// reloadEnv re-reads the base file and every project env.json, carrying the
// session values across.
//
// The error is recorded rather than returned, and the fresh environment is
// installed either way: LoadEnvTree skips only the file that does not parse, so
// keeping the old one would throw away the good projects' values as well as the
// broken one's. Recorded rather than swallowed, because an environment that
// silently stopped resolving would look like the substitution being broken.
func (p *Palette) reloadEnv() {
	if p.envPath == "" {
		return
	}
	fresh, err := api.LoadEnvTree(p.envPath, p.requestsDir())
	p.envErr = err
	if fresh == nil {
		return
	}
	fresh.Adopt(p.env)
	p.env = fresh
	if p.mode == modeAPI {
		p.syncEnvScope()
		p.updateStripLabel()
	}
}

// requestsDir is the root of the requests tree, or "" when the tab has no
// store behind it. The project environment files are found by walking it.
func (p *Palette) requestsDir() string {
	if p.requests == nil {
		return ""
	}
	return p.requests.Dir()
}

// syncEnvScope points the environment at the project the highlight is in, so
// {{base}} means whatever that project's env.json says it means.
//
// A folder row scopes to itself: highlighting a project and pressing Alt+E is
// how its file gets created, and it would be odd for the row named after the
// project to be resolving against its parent.
func (p *Palette) syncEnvScope() {
	if p.env == nil {
		return
	}
	folder := ""
	if r, ok := p.selectedRequest(); ok {
		folder = r.folder
		if r.isDir {
			folder = r.id
		}
	}
	p.env.SetScope(folder)
}

// requestsChanged is the watcher's question for the requests tree. It is a
// method rather than a direct call because the store is optional -- a config
// with no requests folder leaves it nil, and the tab is simply empty.
func (p *Palette) requestsChanged() bool {
	if p.requests == nil {
		return false
	}
	return p.requests.Changed()
}

// envChanged reports whether the base env.json has been written since it was
// last read.
//
// Only the base one. The project files live inside the requests tree, so
// requestsChanged already sees them -- and every reload runs together, so one
// source noticing is enough. A file that has appeared or vanished counts too:
// creating env.json for the first time has to be noticed without a restart.
func (p *Palette) envChanged() bool {
	if p.envPath == "" {
		return false
	}
	var st envStamp
	if fi, err := os.Stat(p.envPath); err == nil {
		st = envStamp{exists: true, mod: fi.ModTime(), size: fi.Size()}
	}
	if st == p.envSeen {
		return false
	}
	p.envSeen = st
	return true
}

// envStamp is what "unchanged" means for the environment file.
type envStamp struct {
	exists bool
	mod    time.Time
	size   int64
}

// apiLabel renders a requests row: "auth: login", or "auth/" for a folder.
func apiLabel(it fuzzy.Item) string {
	r, ok := it.Data.(apiRow)
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

// selectedRequest returns the highlighted requests row.
func (p *Palette) selectedRequest() (apiRow, bool) {
	it, ok := p.selected()
	if !ok {
		return apiRow{}, false
	}
	r, ok := it.Data.(apiRow)
	return r, ok
}

// showRequest puts the highlighted request in the top pane.
//
// The file is shown as written, variables and all, because that is the thing
// being edited and the thing the error messages count lines in. What it
// resolves to goes in the status strip, where it can be checked at a glance
// without the file appearing to change under you.
func (p *Palette) showRequest() {
	// While the pane is holding env.json it is not showing a request, and moving
	// the highlight must not silently replace what is being typed into it.
	if p.envEditing {
		return
	}

	// Which project the highlight is in decides what {{base}} resolves to, so
	// it is settled before anything is read or summarised -- including on the
	// paths below that return early.
	p.syncEnvScope()
	p.updateStripLabel()

	r, ok := p.selectedRequest()
	if !ok || r.isDir {
		p.saveRequestIfDirty()
		p.curRequest = ""
		p.setPaneText(p.reqPane, "")
		p.setPaneText(p.respPane, "")
		p.setReqEditable(false)
		p.setStatus("")
		return
	}

	if r.id == p.curRequest {
		return // already showing it; re-reading would only reset the scroll
	}
	// Moving to another request is not a decision to discard the edits to this
	// one, exactly as it is not for a note.
	p.saveRequestIfDirty()
	p.curRequest = r.id

	// A new request means the last response is no longer an answer to what is on
	// screen. Leaving it there would be the palette showing a reply to a
	// question nobody asked.
	p.setPaneText(p.respPane, "")

	data, err := os.ReadFile(r.path)
	if err != nil {
		p.curRequest = ""
		p.setPaneText(p.reqPane, "could not read "+r.id+": "+err.Error())
		p.setReqEditable(false)
		p.setStatus("")
		return
	}
	p.setPaneText(p.reqPane, string(data))
	p.setReqEditable(true)
	p.setStatus(p.summarise(string(data)))
}

// summarise is the one-line description of a request that sits in the status
// strip before it is sent: the method and the URL it resolves to, or why it
// cannot be read.
func (p *Palette) summarise(text string) string {
	req, err := api.Parse(text)
	if err != nil {
		if errors.Is(err, api.ErrEmpty) {
			return "empty -- write a request line, such as GET https://host/path"
		}
		return "cannot parse: " + err.Error()
	}

	url, missing := req.URL, []string(nil)
	if p.env != nil {
		url, missing = p.env.Expand(req.URL)
	}
	out := req.Method + " " + url
	if len(missing) > 0 {
		// Named, because the alternative is a request that looks fine and fails
		// at send time with a URL nobody wrote.
		out += "   (no value for " + strings.Join(missing, ", ") + ")"
	}
	return out
}

// sendRequest is Enter in API mode.
//
// It parses and expands on the UI thread -- both are string work on a file
// already in memory -- and hands only the network part to a worker. Nothing
// about a slow endpoint can reach the window from there except one message.
func (p *Palette) sendRequest() {
	r, ok := p.selectedRequest()
	if !ok || r.isDir {
		return
	}

	// Read from the pane rather than the file. They are the same today, and will
	// not be once the pane is editable -- and "send what I am looking at" is the
	// only rule that stays true either way.
	text := fromCRLF(p.reqPane.Text())

	req, err := api.Parse(text)
	if err != nil {
		p.setStatus("cannot parse: " + err.Error())
		return
	}
	if p.env != nil {
		var missing []string
		req, missing = p.env.ExpandRequest(req)
		if len(missing) > 0 {
			p.setStatus("no value for " + strings.Join(missing, ", ") +
				" in environment " + p.envName())
			return
		}
	}

	p.setStatus("sending...")
	p.setPaneText(p.respPane, "")
	p.sender.start(p.wnd.Hwnd(), req)
}

// finishSend handles a completed send, back on the UI thread.
func (p *Palette) finishSend(res sendResult) {
	if res.err != nil {
		p.setStatus("error: " + res.err.Error())
		p.setPaneText(p.respPane, "")
		return
	}

	resp := res.resp
	status := fmt.Sprintf("%d %s   %s   %s",
		resp.Status, resp.StatusText, formatDuration(resp.Elapsed), formatSize(resp.Size))
	if resp.Truncated {
		status += "   (truncated)"
	}

	// The hook runs after the response is on screen, so a hook that fails still
	// leaves the response there to look at -- which is the first thing you need
	// in order to work out why it failed.
	p.setPaneText(p.respPane, resp.Body)

	if res.hook != "" {
		changed, err := api.RunHook(res.hook, resp, p.env)
		switch {
		case err != nil:
			status += "   hook: " + err.Error()
		case len(changed) > 0:
			status += "   set " + strings.Join(changed, ", ")
		}
	}

	p.setStatus(status)
}

// copyAsCurl is Ctrl+Shift+C: the highlighted request as a curl command line,
// on the clipboard.
//
// It copies the *expanded* request, which is the whole point -- the line is
// going somewhere this app is not, usually an ssh session on the machine being
// debugged, where {{base}} means nothing. Unlike Enter it does not close the
// palette: copying a line to paste elsewhere is rarely the last thing you do
// with the request.
func (p *Palette) copyAsCurl() {
	if p.envEditing {
		return
	}
	r, ok := p.selectedRequest()
	if !ok || r.isDir {
		return
	}

	req, err := api.Parse(fromCRLF(p.reqPane.Text()))
	if err != nil {
		p.setStatus("cannot parse: " + err.Error())
		return
	}

	var missing []string
	if p.env != nil {
		req, missing = p.env.ExpandRequest(req)
	}

	if err := winapi.SetClipboardText(api.FormatCurl(req)); err != nil {
		p.setStatus("could not write to clipboard: " + err.Error())
		return
	}

	status := "copied as curl"
	if len(missing) > 0 {
		// Copied anyway, with the unexpanded names left in the line: a curl with
		// a visible {{token}} in it is more useful than a refusal, because the
		// value can be pasted in by hand at the other end.
		status += " -- but no value for " + strings.Join(missing, ", ")
	}
	p.setStatus(status)
}

// cancelSend is Esc while a request is in flight. It returns whether there was
// one, which is what lets Esc mean "stop this" before it means "close".
func (p *Palette) cancelSend() bool {
	if !p.sender.cancel() {
		return false
	}
	p.setStatus("cancelled")
	return true
}

// cycleEnv is Ctrl+E.
func (p *Palette) cycleEnv() {
	if p.env == nil {
		return
	}
	p.env.Next()
	p.updateStripLabel()

	// The status strip carries the resolved URL, which has just changed.
	if r, ok := p.selectedRequest(); ok && !r.isDir {
		p.setStatus(p.summarise(fromCRLF(p.reqPane.Text())))
	}
}

// envName is what the strip shows: the project whose file is being used and the
// stage it is on.
//
// Both halves, because with one file per project the stage alone does not say
// whose "uat" this is -- and the project alone does not say which of its four
// stages is live. A stage the project does not define is called out rather than
// shown as if it resolved, since the stage is global and can be pointed at a
// project that has never heard of it.
func (p *Palette) envName() string {
	if p.env == nil || p.env.Active() == "" {
		return "(none)"
	}
	name := p.env.Active()
	if proj := p.env.Project(); proj != "" {
		name = proj + " · " + name
	}
	if !p.env.Defined() {
		name += " (not set here)"
	}
	return name
}

// updateStripLabel fills the strip beside the tabs, which belongs to whichever
// mode has something to put there: the chain in transforms, the environment in
// API, nothing in the other two.
func (p *Palette) updateStripLabel() {
	if p.chainLabel == nil {
		return
	}
	switch p.mode {
	case modeTransforms:
		p.updateChainLabel()
		return
	case modeAPI:
		text := "env: " + p.envName()
		if p.envErr != nil {
			// Named, because with one file per project "env.json is broken" no
			// longer says which one.
			text = "broken: " + p.envErr.Error()
		}
		p.chainLabel.Hwnd().SetWindowText(text)
	default:
		p.chainLabel.Hwnd().SetWindowText("")
	}
	p.chainLabel.Hwnd().InvalidateRect(nil, true)
}

// setStatus writes the strip between the two panes.
func (p *Palette) setStatus(text string) {
	p.statusLabel.Hwnd().SetWindowText(text)
	p.statusLabel.Hwnd().InvalidateRect(nil, true)
}

// setPaneText fills a read-only pane.
//
// quiet is set around it for the same reason the note editor needs it:
// SetWindowText raises EN_CHANGE exactly like typing, and the handler that
// marks a note unsaved cannot tell the two apart.
func (p *Palette) setPaneText(pane *ui.Edit, text string) {
	p.quiet = true
	pane.SetText(toCRLF(text))
	p.quiet = false
	// A pane that has just been refilled should read from the top, not from
	// wherever the previous content was scrolled to.
	pane.Hwnd().SendMessage(co.EM_SETSEL, 0, 0)
	pane.Hwnd().SendMessage(co.EM_SCROLLCARET, 0, 0)

	// Text put there by the app is not an edit. quiet suppresses the change
	// notification, but the flag has to be cleared too -- otherwise a request
	// opened after an unsaved one would inherit its asterisk.
	if pane == p.reqPane {
		p.setReqDirty(false)
	}
}

// setAPIFocus moves the keyboard between the list, the request and the
// response, and is what Tab does in this mode.
func (p *Palette) setAPIFocus(which int) {
	p.apiFocus = which
	switch which {
	case apiFocusRequest:
		p.reqPane.Hwnd().SetFocus()
	case apiFocusResponse:
		p.respPane.Hwnd().SetFocus()
	default:
		p.search.Hwnd().SetFocus()
	}
	// The main list dims when the keys are elsewhere, the same signal places
	// mode uses between its two lists.
	p.list.Hwnd().InvalidateRect(nil, true)
}

// nextAPIFocus is the Tab order, wrapping.
func nextAPIFocus(cur int, back bool) int {
	if back {
		return (cur + 2) % 3
	}
	return (cur + 1) % 3
}

// formatDuration renders an elapsed time at a useful precision: milliseconds
// for anything under ten seconds, which is every request worth waiting for.
func formatDuration(d time.Duration) string {
	ms := d.Milliseconds()
	if ms < 10000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// formatSize renders a byte count the way a person reads one.
func formatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f kB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}
