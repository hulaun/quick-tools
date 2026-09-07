//go:build windows

package ui

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/place"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// placeStore is the subset of place.Store the palette needs.
type placeStore interface {
	Load() ([]place.Place, error)
	Changed() bool
	Path() string
	Add(place.Place) (int, error)
	SetName(index int, name string) error
	Delete(index int) error
}

// placeRow is one row in the places list: the entry as written, plus the path
// resolved into the form Win32 will accept.
//
// Resolution happens once, at load, rather than at every keystroke -- it is
// purely textual and cannot fail, so there is nothing to gain by deferring it
// and one less thing to get wrong at the moment something is launched.
type placeRow struct {
	place.Place
	resolved string
}

// action is one entry in the list on the right: something that can be done
// with the highlighted place.
type action struct {
	label string
	id    string // the opener it runs, or OpenCopy
	exe   string // the executable, for the openers that need one
	dir   string // the folder a terminal should start in

	// reveal means Explorer opens with the item selected rather than opening the
	// item itself, which is the only way to get at a file whose own opener is not
	// the one wanted.
	reveal bool

	// elevate carries the place's setting down to the launch. Copying a path
	// never needs it, so it is per-action rather than read from the place.
	elevate bool
}

// statCache remembers what each path turned out to be.
//
// Classifying a path needs the disk -- hosts is a file with no extension and
// .m2 is a folder with what looks like one, so the text cannot say -- and a
// stat on a disconnected network drive blocks for seconds. Doing it on the UI
// thread would freeze the window on exactly the paths most likely to be in
// someone's places file. So it runs on a goroutine, the answer is remembered,
// and the pane fills in when it arrives. Locally that is under a millisecond
// and looks instant.
type statCache struct {
	mu       sync.Mutex
	kinds    map[string]place.Kind
	inflight map[string]bool
}

func newStatCache() *statCache {
	return &statCache{
		kinds:    make(map[string]place.Kind),
		inflight: make(map[string]bool),
	}
}

func (c *statCache) get(path string) (place.Kind, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k, ok := c.kinds[path]
	return k, ok
}

// request starts a stat if one is not already running or finished, and posts
// back to the window when it lands.
func (c *statCache) request(path string, hwnd win.HWND) {
	c.mu.Lock()
	if _, done := c.kinds[path]; done || c.inflight[path] {
		c.mu.Unlock()
		return
	}
	c.inflight[path] = true
	c.mu.Unlock()

	go func() {
		kind := place.Stat(path)

		c.mu.Lock()
		c.kinds[path] = kind
		delete(c.inflight, path)
		c.mu.Unlock()

		hwnd.PostMessage(wmPlaceStat, 0, 0)
	}()
}

// forget drops what is remembered, so a folder created since the palette
// opened is not still reported as missing. Called on every reload.
func (c *statCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.kinds = make(map[string]place.Kind)
}

// reloadPlaces rebuilds the places index from places.json.
//
// Must run on the UI thread: it replaces the index the window procedure reads.
func (p *Palette) reloadPlaces() {
	places, err := p.places.Load()
	p.placesErr = err

	rows := make([]placeRow, 0, len(places))
	for _, pl := range places {
		rows = append(rows, placeRow{Place: pl, resolved: place.Normalize(pl.Path)})
	}

	// Group first, then name, so entries written in any order read as a filed
	// list -- the same reason the notes list sorts by its relative path.
	sort.SliceStable(rows, func(i, j int) bool {
		gi, gj := strings.ToLower(rows[i].Group), strings.ToLower(rows[j].Group)
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})

	items := make([]fuzzy.Item, 0, len(rows))
	for i, r := range rows {
		// The resolved path is context, not a name: searching "m2" should find the
		// place called ".m2", and typing part of a path should still find it, but a
		// path must never outrank a name that matches better.
		items = append(items, fuzzy.Item{
			ID:    placeID(i, r),
			Name:  r.Name,
			Group: r.Group,
			Tags:  []string{r.resolved},
			Data:  r,
		})
	}
	p.indexes[modePlaces] = fuzzy.New(items)
	p.statCache.forget()

	// Only when this mode is the one on screen. The watcher reloads all three
	// sources together, and refiltering a list that is not showing drops its
	// highlight to the first row for no reason the user can see.
	if p.shown && p.mode == modePlaces {
		p.refilterKeeping(p.currentID())
	}
}

// placeID identifies a row across a reload. The index is part of it because
// two places may legitimately point at the same path under different names.
func placeID(i int, r placeRow) string {
	return strings.Join([]string{strconv.Itoa(i), r.Name, r.resolved}, "|")
}

// placeLabel renders a places row: "Work: Maven repo", or just the name.
func placeLabel(it fuzzy.Item) string {
	if it.Group == "" {
		return it.Name
	}
	return it.Group + ": " + it.Name
}

// selectedPlace returns the highlighted places row.
func (p *Palette) selectedPlace() (placeRow, bool) {
	it, ok := p.selected()
	if !ok {
		return placeRow{}, false
	}
	r, ok := it.Data.(placeRow)
	return r, ok
}

// showPlace fills the right-hand pane for the highlighted place: the resolved
// path above, and what can be done with it below.
func (p *Palette) showPlace() {
	r, ok := p.selectedPlace()
	if !ok {
		p.setActions(nil)
		p.setPathLabel(p.emptyMessage())
		return
	}

	kind, known := p.statCache.get(r.resolved)
	if !known {
		p.statCache.request(r.resolved, p.wnd.Hwnd())
		// Show the path immediately and leave the actions until the answer lands.
		// Guessing now and correcting a moment later would make the list flicker
		// between two different sets of options.
		p.setActions(nil)
		p.setPathLabel(r.resolved)
		return
	}

	p.setActions(p.actionsFor(r, kind))
	if kind == place.KindMissing {
		p.setPathLabel(r.resolved + "  --  not found")
	} else {
		p.setPathLabel(r.resolved)
	}
}

// emptyMessage explains an empty list, which otherwise looks like a broken
// tab rather than a file nobody has written yet.
func (p *Palette) emptyMessage() string {
	if p.placesErr != nil {
		return "could not read " + p.places.Path() + ": " + p.placesErr.Error()
	}
	if p.indexes[modePlaces] == nil || len(p.indexes[modePlaces].Search("")) == 0 {
		return "no places yet -- add them to " + p.places.Path()
	}
	return ""
}

// actionsFor builds the list on the right for one place.
//
// The order is the point: the first action is what Enter does, so the common
// case for each kind of path comes first and the rest are there for when it is
// not what was wanted. A place naming an opener moves that one to the front.
func (p *Palette) actionsFor(r placeRow, kind place.Kind) []action {
	if kind == place.KindMissing {
		return []action{{label: "Copy path", id: place.OpenCopy}}
	}

	dir := r.resolved
	if kind != place.KindDir {
		dir = filepath.Dir(r.resolved)
	}

	var acts []action
	add := func(label, id, exe string, reveal bool) {
		if exe == "" && (id == place.OpenCode || id == place.OpenNotepadPP || id == place.OpenTerminal) {
			return // not installed: offering it would only fail
		}
		acts = append(acts, action{
			label: label, id: id, exe: exe, dir: dir,
			reveal: reveal, elevate: r.Elevate && id != place.OpenCopy,
		})
	}

	code := p.openers[place.OpenCode]
	npp := p.openers[place.OpenNotepadPP]
	term := p.openers[place.OpenTerminal]

	switch kind {
	case place.KindDir:
		add("Open in Explorer", place.OpenExplorer, "", false)
		add(terminalLabel(term), place.OpenTerminal, term, false)
		add("Open in VS Code", place.OpenCode, code, false)
	case place.KindExe:
		add("Run", place.OpenDefault, "", false)
		add("Show in Explorer", place.OpenExplorer, "", true)
		add(terminalLabel(term)+" here", place.OpenTerminal, term, false)
	default: // KindFile
		add("Open", place.OpenDefault, "", false)
		add("Open in VS Code", place.OpenCode, code, false)
		add("Open in Notepad++", place.OpenNotepadPP, npp, false)
		add("Show in Explorer", place.OpenExplorer, "", true)
		add(terminalLabel(term)+" here", place.OpenTerminal, term, false)
	}
	add("Copy path", place.OpenCopy, "", false)

	// The place's own choice goes first, so Enter honours it.
	if r.Open != "" {
		for i, a := range acts {
			if a.id == r.Open && !a.reveal {
				moved := a
				acts = append(acts[:i], acts[i+1:]...)
				acts = append([]action{moved}, acts...)
				break
			}
		}
	}

	// Elevation is worth saying out loud: it is the difference between an editor
	// that can save to hosts and one that cannot, and it costs a UAC prompt.
	for i := range acts {
		if acts[i].elevate && acts[i].id != place.OpenExplorer {
			acts[i].label += "  (as admin)"
		}
	}
	return acts
}

// terminalLabel names the terminal that was actually found, because "Open in
// Terminal" landing in a bare cmd.exe is a small surprise worth removing.
func terminalLabel(exe string) string {
	if strings.EqualFold(filepath.Base(exe), "wt.exe") {
		return "Open in Terminal"
	}
	return "Open in Command Prompt"
}

// setActions replaces the list on the right.
func (p *Palette) setActions(acts []action) {
	p.actions = acts
	if p.optIdx >= len(acts) {
		p.optIdx = 0
	}

	p.opts.SetRedraw(false)
	p.opts.DeleteAllItems()
	for _, a := range acts {
		p.opts.AddItem(a.label)
	}
	if p.opts.ColCount() > 0 {
		p.opts.Col(0).SetWidth(columnWidth(p.opts.Hwnd()))
	}
	p.opts.SetRedraw(true)
	p.opts.Hwnd().InvalidateRect(nil, true)

	if len(acts) == 0 {
		// Nothing to move to, so arrow keys go back to driving the places list
		// rather than pointing at a row that is not there.
		p.optFocus = false
	}
}

func (p *Palette) setPathLabel(text string) {
	p.pathLabel.Hwnd().SetWindowText(text)
	p.pathLabel.Hwnd().InvalidateRect(nil, true)
}

// moveOption walks the action list, wrapping and clamping like the main list.
func (p *Palette) moveOption(delta int) {
	if len(p.actions) == 0 {
		return
	}
	p.optIdx = step(p.optIdx, delta, len(p.actions))
	p.opts.Item(p.optIdx).EnsureVisible()
	p.opts.Hwnd().InvalidateRect(nil, true)
}

// toggleOptFocus moves the arrow keys between the two lists.
//
// Focus stays in the search box throughout -- it is a flag, not a real focus
// change. Moving Win32 focus to the list would hand it the arrow keys and the
// selection along with them, and a selected row is exactly what this window
// cannot have: it ignores custom draw and paints itself in the system accent
// colour. Which list the arrows drive is shown by which highlight is lit.
func (p *Palette) toggleOptFocus() {
	if p.mode != modePlaces {
		return
	}
	if !p.optFocus && len(p.actions) == 0 {
		return
	}
	p.optFocus = !p.optFocus
	p.list.Hwnd().InvalidateRect(nil, true)
	p.opts.Hwnd().InvalidateRect(nil, true)
}

// createPlace is Alt+D in places mode: turn a path you already have into an
// entry, and open the name box on it.
//
// The path comes from the clipboard first, because that is where a path
// invariably is -- copied out of Explorer's address bar, a terminal prompt or a
// config file -- and the palette has already captured it. The search box is the
// fallback for one typed in directly. Neither is used unless it resolves to
// somewhere absolute: "vpn" in the search box is a query, not a place.
func (p *Palette) createPlace() {
	from := ""
	for _, candidate := range []string{p.input, p.search.Text()} {
		if resolved := place.Normalize(candidate); place.IsRooted(resolved) {
			from = resolved
			break
		}
	}
	if from == "" {
		p.setPathLabel("copy a path first, or type one -- Alt+D then makes it a place")
		return
	}

	idx, err := p.places.Add(place.Place{Name: place.BaseName(from), Path: from})
	if err != nil {
		p.fatal("Could not add the place", err)
		return
	}

	// Clear the query so the new row is definitely on screen: whatever was typed
	// was a search, and a search the new place does not match would hide it.
	p.search.SetText("")
	p.reloadPlaces()
	p.refilter()
	p.selectByPlaceIndex(idx)
	p.beginRename()
}

// selectByPlaceIndex highlights the row for a given position in places.json.
// The list is sorted for reading, so the file's order is not the screen's.
func (p *Palette) selectByPlaceIndex(idx int) {
	for i, it := range p.visible {
		if r, ok := it.Data.(placeRow); ok && r.Index == idx {
			p.setSelection(i)
			return
		}
	}
}

// deletePlace is Del in places mode.
//
// It removes the entry and leaves the folder alone, which is what deleting a
// shortcut has always meant. The confirmation says so out loud rather than
// leaving the user to wonder which of the two just happened.
func (p *Palette) deletePlace() {
	r, ok := p.selectedPlace()
	if !ok {
		return
	}
	if !p.confirm("Remove place",
		"Remove \""+r.Name+"\" from places.json?\n\n"+
			r.resolved+"\n\nThe folder or file itself is not touched.") {
		return
	}

	if err := p.places.Delete(r.Index); err != nil {
		p.fatal("Could not remove the place", err)
		return
	}

	// Land on the row that took its position, so a run of deletes does not send
	// the highlight back to the top between each one.
	at := p.selIdx
	p.reloadPlaces()
	p.refilter()
	if at >= len(p.visible) {
		at = len(p.visible) - 1
	}
	if at >= 0 {
		p.setSelection(at)
	}
}

// moveDown routes the arrow keys to whichever list they are currently driving.
func (p *Palette) moveDown(delta int) {
	if p.mode == modePlaces && p.optFocus {
		p.moveOption(delta)
		return
	}
	p.moveSelection(delta)
}

// atEndOfSearch reports whether the caret is at the end of the query with
// nothing selected, which is when Right has no text left to move over and can
// mean "into the action list" instead.
func (p *Palette) atEndOfSearch() bool {
	h := p.search.Hwnd()
	start, end := editSelection(h)
	if start != end {
		return false
	}
	text, err := h.GetWindowText()
	if err != nil {
		return true
	}
	// EM_GETSEL counts UTF-16 code units, so the length has to be measured in
	// the same units rather than in bytes or runes.
	return start >= len(utf16.Encode([]rune(text)))
}

// applyPlace is Enter in places mode: run the highlighted action against the
// highlighted place.
func (p *Palette) applyPlace() {
	r, ok := p.selectedPlace()
	if !ok || len(p.actions) == 0 {
		return
	}
	if p.optIdx < 0 || p.optIdx >= len(p.actions) {
		return
	}
	a := p.actions[p.optIdx]

	if a.id == place.OpenCopy {
		if err := winapi.SetClipboardText(r.resolved); err != nil {
			p.setPathLabel("could not write to clipboard: " + err.Error())
			return
		}
		p.hide(true)
		if p.cfg.AutoPaste {
			winapi.SendPaste()
		}
		return
	}

	if err := p.launch(a, r); err != nil {
		if err == winapi.ErrCancelled {
			// The elevation prompt was dismissed. That is a decision, not a
			// failure: leave the palette as it was.
			return
		}
		p.setPathLabel(r.resolved + "  --  " + err.Error())
		return
	}

	// Hide without restoring focus. The app that was just launched is coming to
	// the front, and handing focus back to whatever had it before would race it
	// -- the palette would disappear and the wrong window would end up active.
	p.hide(false)
}

func (p *Palette) launch(a action, r placeRow) error {
	switch {
	case a.reveal:
		return winapi.Reveal(r.resolved)
	case a.id == place.OpenTerminal:
		return winapi.OpenTerminalAt(a.exe, a.dir)
	case a.id == place.OpenExplorer, a.id == place.OpenDefault:
		return winapi.Open(r.resolved, a.elevate)
	default:
		return winapi.OpenWith(a.exe, r.resolved, a.elevate)
	}
}
