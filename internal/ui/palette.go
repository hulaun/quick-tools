//go:build windows

// Package ui implements the palette window.
//
// The window is created once at startup and then merely shown and hidden. That
// is the whole basis of the app feeling instant: opening it is a ShowWindow
// call against a window that already exists, not a window being built.
package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/api"
	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/place"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/winapi"
)

const hotkeyID = 1

// Layout.
//
// These constants are the only numbers that decide how the palette looks and
// where it sits. Change them and rebuild -- nothing else needs touching.
const (
	winW, winH = 750, 500 // overall window size
	margin     = 8        // gap from the corner of the screen's work area
	pad        = 10       // padding inside the window
	tabsH      = 26       // height of the mode tabs
	tabW       = 96       // width of one mode tab
	tabGap     = 6        // gap between the two tabs
	searchH    = 24       // height of the search box
	listW      = 250      // width of the list
	rowGap     = 8        // gap between the search box and the panes below

	// Corner radii. Two scales on purpose: the chrome you aim at -- the tabs and
	// the search box -- is a capsule, half its own height, matching the pill the
	// highlighted row is drawn as; the panels you read out of are barely
	// rounded, because a big radius on a big rectangle eats the corner of the
	// text rather than framing it.
	searchRadius = searchH / 2 // capsule
	tabRadius    = tabsH / 2   // capsule
	paneRadius   = 8           // the lists and the right-hand panes

	// The window's own corners are the desktop window manager's and cannot be
	// made rounder. DWMWCP_ROUND is a fixed 8 with no attribute for more; a
	// region does not round these windows at all (see maskCorners); and painting
	// the corners out, which is what rounds everything else here, cannot work on
	// the window itself -- the acrylic is drawn behind the whole window
	// rectangle, so painting a corner transparent just shows more acrylic. The
	// palette is as round as Windows will make it.

	// textPad is the left inset for text inside the search box and the panes.
	// A Win32 edit puts its text hard against the frame, which read as cramped
	// once the frame was rounded and the corner started cutting into it.
	textPad = 10

	// The highlighted row is a rounded rectangle drawn inside the row, not the
	// row itself: a list view row runs the full width of the control and butts
	// against the one above it, so a highlight that filled it would have nothing
	// to round. The insets are what give the corners somewhere to be.
	selRadius = 10 // corner radius of the highlighted row
	selInsetX = 4  // gap between the highlight and the sides of the list
	selInsetY = 1  // gap between the highlight and the rows above and below
	rowTextX  = 8  // where a row's text starts, measured from the highlight's edge

	// pathH is the height of the strip at the top of the right-hand pane that
	// shows a place's resolved path. Two lines, because a resolved path is
	// routinely longer than the pane is wide.
	pathH = 44

	// Derived, so that the row rectangle the in-place name box is placed on can
	// be worked out from the same numbers the controls were laid out with.
	chainX  = pad + modeCount*(tabW+tabGap) // the strip beside the tabs
	chainW  = winW - pad - chainX
	searchY = pad + tabsH + rowGap
	listTop = searchY + searchH + rowGap
	listH   = winH - listTop - pad

	// The right-hand pane: the note editor and the transform preview fill it,
	// and places mode splits it into the path strip and the action list.
	rightX = pad + listW + rowGap
	rightW = winW - listW - rowGap - pad*2
	optsY  = listTop + pathH + rowGap
	optsH  = listH - pathH - rowGap

	// The API tab splits the right-hand side into the request above and the
	// response below, with a one-line strip between them for the status.
	//
	// The split favours the response: you re-read a response far more often than
	// the request that produced it. Both panes scroll, and Ctrl+Down moves five
	// lines, which is what makes the split affordable at this window size.
	reqH    = 180
	statusH = 22
	statusY = listTop + reqH + rowGap
	respY   = statusY + statusH
	respH   = listH - reqH - rowGap - statusH
)

// The five things the palette can be: a transform runner over the clipboard, a
// notes store, a list of places on disk, a set of recorded keystroke macros, or
// a set of saved HTTP requests. They share the search box and the list, and the
// mode decides what they are filled with and what Enter does.
//
// The right-hand pane is where they differ: transforms, notes and macros all
// use the editor -- a preview in the first and third, an editable note in the
// second -- places uses a path strip above a list of actions, and the API tab
// uses a request pane above a status strip above a response pane. Only one set
// is ever visible.
//
// Macros sit between places and API because that is where they belong by
// weight: the first three tabs act on text or on the filesystem and cost
// nothing, and the last two reach outside the machine -- one synthesises input
// into another application, the other sends a request over the network.
const (
	modeTransforms = iota
	modeNotes
	modePlaces
	modeMacros
	modeAPI
	modeCount
)

// tabTitle is the label on a mode tab. The dot marks unsaved edits; the tab is
// the only chrome always on screen while typing into a note.
func tabTitle(mode int, dirty bool) string {
	switch mode {
	case modeNotes:
		if dirty {
			return "Notes *"
		}
		return "Notes"
	case modePlaces:
		return "Places"
	case modeMacros:
		return "Macros"
	case modeAPI:
		if dirty {
			return "API *"
		}
		return "API"
	default:
		return "Transforms"
	}
}

// Palette is the command palette window.
type Palette struct {
	wnd      *ui.Main
	tabs     [modeCount]*ui.Static
	search   *ui.Edit
	list     *ui.ListView
	preview  *ui.Edit
	nameEdit *ui.Edit

	// The right-hand pane in places mode: the resolved path, and the list of
	// things that can be done with it.
	pathLabel *ui.Static
	opts      *ui.ListView

	// The right-hand side in API mode.
	reqPane     *ui.Edit
	statusLabel *ui.Static
	respPane    *ui.Edit

	// chainLabel is the strip beside the tabs listing the transforms already
	// applied to the working input.
	chainLabel *ui.Static

	cfg      config.Config
	reg      *transform.Registry
	theme    *theme
	tray     *tray
	loader   scriptLoader
	snippets snippetStore
	places   placeStore
	runner   *runner

	// openers maps an opener id to the executable serving it, detected once at
	// startup and overridden from config.
	openers place.Openers

	// actions is what the right-hand list is showing for the highlighted place,
	// optIdx is the highlighted one, and optFocus says the arrow keys are
	// driving that list rather than the places list.
	actions   []action
	optIdx    int
	optFocus  bool
	statCache *statCache

	// placesErr is a malformed places.json, reported in the pane rather than
	// swallowed: an empty list with no reason looks like a broken tab.
	placesErr error

	// The Macros tab. rec is the recording in progress, if any, and undo is the
	// one-shot watch for the Ctrl+Z after a replay. Both own a keyboard hook, so
	// at most one of them is ever set.
	macros    macroStore
	macrosErr error
	rec       *recording
	undo      *undoArm

	// mode is which of the two the palette is currently showing, and indexes
	// holds one searchable set per mode. They are kept apart rather than merged
	// so that a search for a note cannot surface a transform, and so that Enter
	// has one unambiguous meaning at a time.
	mode    int
	indexes [modeCount]*fuzzy.Index

	// curNote is the id of the note currently in the editor, and dirty says it
	// has unsaved edits. quiet suppresses the change notification while the
	// editor is being filled programmatically, which would otherwise look
	// exactly like the user typing.
	curNote string
	dirty   bool
	quiet   bool

	// The API tab. requests is the .http tree, env the environments, sender the
	// off-thread HTTP client, and curRequest the id showing in the request pane.
	//
	// apiFocus says which of the three regions the keyboard is in. Unlike places
	// mode's optFocus this is a real focus change, and safely so: the two panes
	// are Edits, and it is a *list view* that cannot be focused without selecting
	// a row and losing its custom-drawn highlight.
	requests   snippetStore
	env        *api.Env
	envPath    string
	envErr     error
	sender     *sender
	curRequest string
	apiFocus   int

	// reqDirty is unsaved text in the request pane, and envEditing says that
	// pane is currently holding env.json rather than a request.
	reqDirty   bool
	envEditing bool

	// envSeen is the modification stamp the watcher compares against. It is read
	// and written only by the watcher goroutine -- envChanged is its sole user --
	// so it needs no lock, and must not grow another caller without one.
	envSeen envStamp

	// renaming is the row the in-place name box is currently over, if any.
	renaming renameState

	// chain is the transforms already applied to input this time round. Empty
	// means input is still exactly what was on the clipboard.
	chain []chainStep

	// scriptIDs are the transform ids currently contributed by scripts, so the
	// previous generation can be removed on reload.
	scriptIDs []string

	// visible mirrors the rows currently in the list view, so a row index can be
	// mapped back to the item it represents.
	visible []fuzzy.Item

	// input is the clipboard text captured when the palette opened. Everything
	// previews and runs against this snapshot rather than re-reading the
	// clipboard, so the result cannot change under the user mid-selection.
	input string

	// target is the window that had focus when the palette opened, and where the
	// result is pasted back.
	target uintptr

	// selIdx is the highlighted row. Custom draw runs once per row per repaint,
	// so it reads this rather than querying the control each time.
	selIdx int

	// look ensures the one-off appearance setup runs exactly once.
	look sync.Once

	// sbDrag is the overlay scrollbar thumb currently being dragged, if any.
	// One field for every control, because mouse capture means only one of them
	// can be in a drag at a time.
	sbDrag scrollDrag

	// shown guards against re-entering hide() while hiding, which the
	// WM_ACTIVATE handler would otherwise cause.
	shown bool
}

// New builds the palette window and its controls. Nothing is displayed yet --
// the window is created hidden and stays that way until the hotkey fires.
func New(cfg config.Config, reg *transform.Registry, loader scriptLoader, snippets snippetStore, places placeStore, macros macroStore, requests snippetStore, envPath string) (*Palette, error) {
	th, err := newTheme()
	if err != nil {
		return nil, err
	}

	wnd := ui.NewMain(
		ui.OptsMain().
			Title("quick-tools").
			Size(ui.Dpi(winW, winH)).
			Style(co.WS_POPUP | co.WS_CLIPCHILDREN).
			// TOOLWINDOW keeps it out of the taskbar and Alt+Tab; TOPMOST keeps it
			// above the window we are about to paste into.
			//
			// COMPOSITED renders the window and every child into one off-screen
			// buffer. LVS_EX_DOUBLEBUFFER only covers painting *inside* the list;
			// when the scrollbar appears or disappears the control's non-client
			// area changes and the parent repaints the strip underneath it, and
			// that hand-off between two windows is the frame that flashes dark.
			// Only compositing the whole hierarchy removes it.
			ExStyle(co.WS_EX_TOOLWINDOW | co.WS_EX_TOPMOST | co.WS_EX_COMPOSITED).
			ClassBrush(th.bg).
			// Suppress the window loop's IsDialogMessage call. It exists for
			// dialog-style Tab navigation between controls, which this window does
			// not use -- focus stays in the search box and the list is driven from
			// there. Left on, it swallows Enter and turns it into a dialog IDOK
			// command, so the key never reaches the search box subclass and
			// selecting an entry silently does nothing.
			ProcessDlgMsgs(false).
			CmdShow(co.SW_HIDE),
	)

	// The mode tabs are plain static controls rather than a tab control. A real
	// tab control brings its own themed chrome, which cannot be made dark
	// without owner-drawing it anyway -- at which point it is two labels with a
	// background colour, which is exactly what these are.
	var tabs [modeCount]*ui.Static
	for i := range tabs {
		tabs[i] = ui.NewStatic(wnd,
			ui.OptsStatic().
				Text(tabTitle(i, false)).
				Position(ui.Dpi(pad+i*(tabW+tabGap), pad)).
				Size(ui.Dpi(tabW, tabsH)).
				CtrlStyle(co.SS_CENTER|co.SS_CENTERIMAGE|co.SS_NOTIFY).
				WndStyle(co.WS_CHILD|co.WS_VISIBLE).
				WndExStyle(co.WS_EX_LEFT),
		)
	}

	chainLabel := ui.NewStatic(wnd,
		ui.OptsStatic().
			Position(ui.Dpi(chainX, pad)).
			Size(ui.Dpi(chainW, tabsH)).
			CtrlStyle(co.SS_LEFT|co.SS_CENTERIMAGE|co.SS_ENDELLIPSIS).
			WndStyle(co.WS_CHILD|co.WS_VISIBLE).
			WndExStyle(co.WS_EX_LEFT),
	)

	search := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(pad, searchY)).
			Width(ui.DpiX(winW-pad*2)).
			Height(ui.DpiY(searchH)).
			// No border. windigo defaults every control to WS_EX_CLIENTEDGE -- the
			// sunken 3D edge -- so the extended style must be overridden too;
			// clearing WndStyle alone leaves the border in place.
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	list := ui.NewListView(wnd,
		ui.OptsListView().
			Position(ui.Dpi(pad, listTop)).
			Size(ui.Dpi(listW, listH)).
			CtrlStyle(co.LVS_REPORT|co.LVS_SINGLESEL|co.LVS_NOCOLUMNHEADER).
			CtrlExStyle(co.LVS_EX_FULLROWSELECT|co.LVS_EX_DOUBLEBUFFER).
			// One column holding "Group: Name". A separate group column was harder
			// to read than simply writing the label out as a phrase.
			//
			// Column() passes its width straight to the control as raw pixels --
			// unlike Position and Size, it does no DPI scaling -- so the value has
			// to be scaled here. refilter() then stretches it to fill exactly.
			Column("Transform", ui.DpiX(listW-24)).
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	preview := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(rightX, listTop)).
			Width(ui.DpiX(rightW)).
			Height(ui.DpiY(listH)).
			// Read-only to begin with because the palette opens in transform mode,
			// where the pane is a preview. Notes mode lifts it with EM_SETREADONLY.
			// ES_WANTRETURN is what lets Enter insert a line break in a note
			// instead of being treated as "the default button".
			CtrlStyle(co.ES_MULTILINE|co.ES_READONLY|co.ES_AUTOVSCROLL|co.ES_WANTRETURN|co.ES_NOHIDESEL).
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	// The right-hand pane in places mode. Both are created hidden: the palette
	// opens in transform mode, where the editor occupies the same rectangle, and
	// only one of the two may be on screen at a time.
	pathLabel := ui.NewStatic(wnd,
		ui.OptsStatic().
			Position(ui.Dpi(rightX, listTop)).
			Size(ui.Dpi(rightW, pathH)).
			// No SS_ENDELLIPSIS: a path truncated in the middle of a folder name
			// says less than one wrapped onto a second line, and there is room.
			CtrlStyle(co.SS_LEFT|co.SS_EDITCONTROL).
			WndStyle(co.WS_CHILD).
			WndExStyle(co.WS_EX_LEFT),
	)

	opts := ui.NewListView(wnd,
		ui.OptsListView().
			Position(ui.Dpi(rightX, optsY)).
			Size(ui.Dpi(rightW, optsH)).
			CtrlStyle(co.LVS_REPORT|co.LVS_SINGLESEL|co.LVS_NOCOLUMNHEADER).
			CtrlExStyle(co.LVS_EX_FULLROWSELECT|co.LVS_EX_DOUBLEBUFFER).
			Column("Action", ui.DpiX(rightW-24)).
			WndStyle(co.WS_CHILD).
			WndExStyle(co.WS_EX_LEFT),
	)

	// The right-hand side in API mode: the request above, a status strip, and
	// the response below. Created hidden, like the places pane, because the
	// palette opens in transform mode and only one set may be on screen.
	//
	// Both panes are read-only for now -- editing a request is its own step --
	// but they are Edits rather than Statics because they have to scroll, select
	// and be copied out of, which is three things a Static cannot do.
	reqPane := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(rightX, listTop)).
			Width(ui.DpiX(rightW)).
			Height(ui.DpiY(reqH)).
			CtrlStyle(co.ES_MULTILINE|co.ES_READONLY|co.ES_AUTOVSCROLL|co.ES_WANTRETURN|co.ES_NOHIDESEL).
			WndStyle(co.WS_CHILD|co.WS_VSCROLL|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	statusLabel := ui.NewStatic(wnd,
		ui.OptsStatic().
			Position(ui.Dpi(rightX, statusY)).
			Size(ui.Dpi(rightW, statusH)).
			CtrlStyle(co.SS_LEFT|co.SS_CENTERIMAGE|co.SS_ENDELLIPSIS).
			WndStyle(co.WS_CHILD).
			WndExStyle(co.WS_EX_LEFT),
	)

	respPane := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(rightX, respY)).
			Width(ui.DpiX(rightW)).
			Height(ui.DpiY(respH)).
			CtrlStyle(co.ES_MULTILINE|co.ES_READONLY|co.ES_AUTOVSCROLL|co.ES_WANTRETURN|co.ES_NOHIDESEL).
			WndStyle(co.WS_CHILD|co.WS_VSCROLL|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	// The in-place name box. It is created hidden and parked off the top of the
	// window; renaming moves it over the highlighted row.
	//
	// It is our own control rather than the list view's built-in label editing
	// (LVS_EDITLABELS). That would hand the naming to the control, and the
	// control is exactly what this window has spent two milestones prising
	// control away from: label editing gives the row focus, and the row is never
	// allowed to be focused or selected, because a selected row ignores custom
	// draw and paints itself in the system accent colour. A plain Edit moved to
	// the row's rectangle looks the same and touches none of that.
	nameEdit := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(pad, -100)).
			Width(ui.DpiX(listW)).
			Height(ui.DpiY(searchH)).
			WndStyle(co.WS_CHILD).
			WndExStyle(co.WS_EX_LEFT),
	)

	tr, err := newTray()
	if err != nil {
		return nil, err
	}

	p := &Palette{
		wnd:      wnd,
		tabs:     tabs,
		search:   search,
		list:     list,
		preview:  preview,
		nameEdit: nameEdit,

		pathLabel: pathLabel,
		opts:      opts,

		reqPane:     reqPane,
		statusLabel: statusLabel,
		respPane:    respPane,

		chainLabel: chainLabel,
		cfg:        cfg,
		reg:        reg,
		theme:      th,
		tray:       tr,
		loader:     loader,
		snippets:   snippets,
		places:     places,
		macros:     macros,
		requests:   requests,
		envPath:    envPath,
		runner:     newRunner(),
		sender:     newSender(),

		openers:   place.DetectOpeners(cfg.Openers),
		statCache: newStatCache(),
	}
	p.reloadScripts()
	p.reloadNotes()
	p.reloadPlaces()
	p.reloadMacros()
	p.reloadRequests()
	p.reloadEnv()
	p.events()
	return p, nil
}

// Run shows nothing and blocks, pumping messages until the app exits.
func (p *Palette) Run() int { return p.wnd.RunAsMain() }

// rebuildIndex refreshes the transform index from the registry. Called again on
// every script hot-reload.
func (p *Palette) rebuildIndex() {
	all := p.reg.All()
	items := make([]fuzzy.Item, 0, len(all))
	for _, t := range all {
		items = append(items, fuzzy.Item{
			ID: t.ID, Name: t.Name, Group: t.Group, Tags: t.Tags, Data: t,
		})
	}
	p.indexes[modeTransforms] = fuzzy.New(items)
}

// showRightPane swaps the right-hand side to whichever set of controls the
// mode uses: the editor, the places pane, or the API panes.
//
// SW_HIDE rather than moving them off-screen, so a hidden control cannot be
// tabbed into or painted over the visible one. It takes the mode rather than a
// flag because there are now three sets and a boolean cannot say which.
func (p *Palette) showRightPane(mode int) {
	vis := func(on bool) co.SW {
		if on {
			return co.SW_SHOW
		}
		return co.SW_HIDE
	}

	editor := mode == modeTransforms || mode == modeNotes || mode == modeMacros
	p.preview.Hwnd().ShowWindow(vis(editor))

	p.pathLabel.Hwnd().ShowWindow(vis(mode == modePlaces))
	p.opts.Hwnd().ShowWindow(vis(mode == modePlaces))

	api := mode == modeAPI
	p.reqPane.Hwnd().ShowWindow(vis(api))
	p.statusLabel.Hwnd().ShowWindow(vis(api))
	p.respPane.Hwnd().ShowWindow(vis(api))
}

// setMode switches the palette between transforms, notes and places.
//
// Everything visible is derived from the mode: which index the search runs
// over, what the rows say, what the right-hand pane holds, whether it can be
// typed into, and what Enter does. Focus always ends up back in the search box,
// because switching mode is something you do in order to start typing.
func (p *Palette) setMode(mode int) {
	if mode < 0 || mode >= modeCount {
		return
	}
	p.saveIfDirty()
	p.mode = mode

	switch mode {
	case modeTransforms:
		p.curNote = ""
		p.setEditable(false)
		setCueBanner(p.search.Hwnd(), "Search transforms")
	case modeNotes:
		setCueBanner(p.search.Hwnd(), "Search notes")
	case modePlaces:
		p.curNote = ""
		p.setEditable(false)
		setCueBanner(p.search.Hwnd(), "Search places")
	case modeMacros:
		p.curNote = ""
		p.setEditable(false)
		setCueBanner(p.search.Hwnd(), "Search macros")
	case modeAPI:
		p.curNote = ""
		p.setEditable(false)
		setCueBanner(p.search.Hwnd(), "Search requests")
	}

	// Only one right-hand pane is ever on screen. Places replaces the editor
	// with the path strip and the action list rather than sharing it: an action
	// list is not text, and pretending otherwise would mean an editor that
	// sometimes is not one. The API tab does the same with two panes and a
	// status strip.
	p.showRightPane(mode)

	// The strip beside the tabs belongs to whichever mode can use it: the chain
	// in transforms, the active environment in API, nothing in between.
	p.updateStripLabel()

	// Tab starts back in the search box whichever pane it was left in.
	p.apiFocus = apiFocusList

	// The arrow keys always start on the places list, whichever list they were
	// driving when the mode was last left.
	p.optFocus = false
	p.optIdx = 0

	for _, t := range p.tabs {
		t.Hwnd().InvalidateRect(nil, true)
	}

	// A query typed against one mode's entries means nothing in the other's.
	p.search.SetText("")
	p.refilter()
	p.search.Hwnd().SetFocus()
}

func (p *Palette) events() {
	// The hotkey is registered once the window exists, and routed here by the
	// framework's own message loop -- we do not run a second one.
	p.wnd.On().WmCreate(func(_ ui.WmCreate) int {
		mods, vk, err := winapi.ParseHotkey(p.cfg.Hotkey)
		if err != nil {
			p.fatal("Invalid hotkey in config", err)
			return 0
		}
		if err := winapi.RegisterHotkeyFor(uintptr(p.wnd.Hwnd()), hotkeyID, mods, vk); err != nil {
			p.fatal(fmt.Sprintf("Could not register %s", p.cfg.Hotkey), err)
		}

		p.watchSources(p.wnd.Hwnd())

		if err := p.tray.add(p.wnd.Hwnd(), "quick-tools -- "+p.cfg.Hotkey); err != nil {
			// Without the icon there is no way to quit short of Task Manager, so
			// this is worth telling the user about rather than starting anyway.
			p.fatal("Could not create the tray icon", err)
		}
		return 0
	})

	// Edit controls ask their parent what colour to be. Without this they stay
	// white, which on a dark window is the single most obvious thing wrong.
	p.wnd.On().Wm(co.WM_CTLCOLOREDIT, func(m ui.Wm) uintptr {
		return p.theme.paintControlBackground(m)
	})

	// The tabs are statics too, so this handler has to tell them apart from the
	// rest -- the active one is the only control on the window painted in the
	// accent colour. LPARAM carries the handle of the control asking.
	p.wnd.On().Wm(co.WM_CTLCOLORSTATIC, func(m ui.Wm) uintptr {
		h := win.HWND(m.LParam)
		for i, t := range p.tabs {
			if t.Hwnd() == h {
				return p.theme.paintTab(m, i == p.mode)
			}
		}
		if h == p.chainLabel.Hwnd() {
			return p.theme.paintChainLabel(m)
		}
		if h == p.pathLabel.Hwnd() || h == p.statusLabel.Hwnd() {
			return p.theme.paintPathLabel(m)
		}
		return p.theme.paintControlBackground(m)
	})

	for i := range p.tabs {
		mode := i
		p.tabs[i].On().StnClicked(func() { p.setMode(mode) })
	}

	p.wnd.On().Wm(co.WM_HOTKEY, func(_ ui.Wm) uintptr {
		p.onHotkey()
		return 0
	})

	// A second copy of the app broadcasts this instead of starting up, so
	// double-clicking the exe again opens the palette rather than doing nothing.
	if msg := winapi.ShowPaletteMessage; msg != 0 {
		p.wnd.On().Wm(co.WM(msg), func(_ ui.Wm) uintptr {
			p.show()
			return 0
		})
	}

	// Refuse to enter menu mode. This is what makes Alt+D safe.
	//
	// Releasing Alt makes DefWindowProc send SC_KEYMENU, and its handling of
	// that runs the *menu modal loop* -- which takes the keyboard until it is
	// dismissed. This window has no menu, so nothing appears: the palette simply
	// stops responding to every key, and only a mouse click gets out of it.
	//
	// Normally a character key pressed while Alt is held stops that from
	// happening. But Alt+D is handled by swallowing its WM_SYSKEYDOWN (see the
	// createKeys subclass below, and gotcha 19 -- returning 0 there is what
	// suppresses the beep), so DefWindowProc never sees the D at all and still
	// believes the Alt press was a bare one. Suppressing the beep is what armed
	// the lockup.
	//
	// Guarding here rather than on each control is deliberate: SC_KEYMENU is
	// sent to the top-level window whichever child had focus, so this one
	// handler covers the search box, the editor and the name box at once --
	// including the case where focus moved between the Alt going down and
	// coming back up, which is exactly what Alt+D does.
	p.wnd.On().Wm(co.WM_SYSCOMMAND, func(m ui.Wm) uintptr {
		if co.SC(m.WParam&0xfff0) == co.SC_KEYMENU {
			return 0
		}
		return p.wnd.Hwnd().DefWindowProc(co.WM_SYSCOMMAND, m.WParam, m.LParam)
	})

	// Clicking away dismisses the palette, the same as Esc.
	p.wnd.On().Wm(co.WM_ACTIVATE, func(m ui.Wm) uintptr {
		if m.WParam.LoWord() == 0 { // WA_INACTIVE
			p.hide(false)
		}
		return 0
	})

	p.wnd.On().WmDestroy(func() {
		// A keyboard hook outliving the window it was installed from would leave
		// the callback pointing into a process that is on its way out, in front of
		// every key on the machine. Cancel rather than stop: stopRecording saves
		// and brings the window back, which is not a thing to do to a window that
		// is already being destroyed.
		p.cancelRecording()
		p.disarmUndo()
		winapi.UnregisterHotkeyFor(uintptr(p.wnd.Hwnd()), hotkeyID)
		p.tray.remove(p.wnd.Hwnd())
		p.theme.destroy()
	})

	p.trayEvents()
	p.runEvents()
	p.renameEvents()
	p.macroEvents()

	p.search.On().EnChange(func() { p.refilter() })

	// An Edit swallows arrow keys and Enter, so the search box is subclassed to
	// steer them at the list instead. This is the standard cost of putting a
	// list under a text field in Win32.
	p.search.OnSubclass().Wm(co.WM_KEYDOWN, func(m ui.Wm) uintptr {
		vk := co.VK(m.WParam)
		switch vk {
		case co.VK_DOWN:
			p.moveDown(arrowStep())
			return 0
		case co.VK_UP:
			p.moveDown(-arrowStep())
			return 0
		case co.VK_RIGHT:
			// Right is the other way into the action list, for when reaching for
			// Tab is more thought than pointing at the pane it moves to.
			if p.mode == modePlaces && !p.optFocus && p.atEndOfSearch() {
				p.toggleOptFocus()
				return 0
			}
		case co.VK_LEFT:
			if p.mode == modePlaces && p.optFocus {
				p.toggleOptFocus()
				return 0
			}
		case co.VK_RETURN:
			p.apply()
			return 0
		case co.VK_F5:
			// A second name for send, for the muscle memory that expects it.
			if p.mode == modeAPI {
				p.sendRequest()
				return 0
			}
		case co.VK_ESCAPE:
			// Esc means "stop what is happening" before it means "close". A
			// request in flight is the one thing on this window that can be
			// interrupted, so it gets the key first.
			if p.mode == modeAPI && p.cancelSend() {
				return 0
			}
			p.hide(true)
			return 0
		case co.VK('E'):
			if ctrlDown() && p.mode == modeAPI {
				if shiftDown() {
					p.toggleEnvEditor()
				} else {
					p.cycleEnv()
				}
				return 0
			}
		case co.VK('C'):
			// Ctrl+Shift+C, not Ctrl+C: the plain one still copies a selection out
			// of a pane, which is what it does in every other text field.
			if ctrlDown() && shiftDown() && p.mode == modeAPI {
				p.copyAsCurl()
				return 0
			}
		case co.VK('R'):
			// Re-record over the highlighted macro. Alt+D makes a new one; this is
			// the second take, and it keeps the name and any tuning.
			if ctrlDown() && p.mode == modeMacros {
				p.recordIntoSelected()
				return 0
			}
		case co.VK_TAB:
			// Tab is "carry on from here" in every mode: into the note in notes
			// mode, into the action list in places mode, into the request pane in
			// API mode, and into the next transform in transforms mode. The window
			// turned off dialog message processing, so nothing moves unless we
			// move it.
			switch p.mode {
			case modeTransforms:
				p.chainStep()
			case modeNotes:
				p.preview.Hwnd().SetFocus()
			case modePlaces:
				p.toggleOptFocus()
			case modeAPI:
				p.setAPIFocus(nextAPIFocus(apiFocusList, shiftDown()))
			}
			return 0
		case co.VK_F2:
			p.beginRename()
			return 0
		case co.VK_DELETE:
			// Del removes the highlighted note or place -- but only when it has no
			// text left to delete, exactly like Backspace below. Otherwise typing a
			// query and reaching for Del to fix a typo would delete a file instead
			// of a character. The caret is at the end after typing, so the usual
			// flow of searching and then pressing Del still works.
			if !ctrlDown() && p.atEndOfSearch() {
				p.deleteEntry()
				return 0
			}
		case co.VK_BACK:
			// Only when there is nothing left to delete, so it never competes with
			// editing the query itself.
			if !ctrlDown() && p.search.Text() == "" && p.popChain() {
				return 0
			}
		}
		if p.editKeys(p.search.Hwnd(), vk) {
			return 0
		}
		return p.search.Hwnd().DefSubclassProc(co.WM_KEYDOWN, m.WParam, m.LParam)
	})

	// The note editor. Esc and Ctrl+S have to be handled here as well as in the
	// search box: once focus is in the editor, that is where the keys arrive.
	p.preview.OnSubclass().Wm(co.WM_KEYDOWN, func(m ui.Wm) uintptr {
		vk := co.VK(m.WParam)
		switch vk {
		case co.VK_ESCAPE:
			p.hide(true)
			return 0
		case co.VK_TAB:
			p.search.Hwnd().SetFocus()
			return 0
		case co.VK_F2:
			p.beginRename()
			return 0
		}
		if p.editKeys(p.preview.Hwnd(), vk) {
			return 0
		}
		return p.preview.Hwnd().DefSubclassProc(co.WM_KEYDOWN, m.WParam, m.LParam)
	})

	// The API panes. Esc and Tab have to be handled here as well as in the
	// search box: once focus is in a pane, that is where the keys arrive.
	//
	// Ctrl+Enter sends from anywhere, because a request you have just scrolled
	// through in the response pane is exactly when you want to send it again
	// without reaching back to the list first.
	apiPaneKeys := func(c *ui.Edit, which int) func(ui.Wm) uintptr {
		return func(m ui.Wm) uintptr {
			vk := co.VK(m.WParam)
			switch vk {
			case co.VK_ESCAPE:
				if p.cancelSend() {
					return 0
				}
				p.setAPIFocus(apiFocusList)
				return 0
			case co.VK_TAB:
				p.setAPIFocus(nextAPIFocus(which, shiftDown()))
				return 0
			case co.VK_RETURN:
				if ctrlDown() {
					p.sendRequest()
					return 0
				}
			case co.VK('E'):
				if ctrlDown() {
					if shiftDown() {
						p.toggleEnvEditor()
					} else {
						p.cycleEnv()
					}
					return 0
				}
			case co.VK('C'):
				if ctrlDown() && shiftDown() {
					p.copyAsCurl()
					return 0
				}
			}
			if p.editKeys(c.Hwnd(), vk) {
				return 0
			}
			return c.Hwnd().DefSubclassProc(co.WM_KEYDOWN, m.WParam, m.LParam)
		}
	}
	p.reqPane.OnSubclass().Wm(co.WM_KEYDOWN, apiPaneKeys(p.reqPane, apiFocusRequest))
	p.respPane.OnSubclass().Wm(co.WM_KEYDOWN, apiPaneKeys(p.respPane, apiFocusResponse))

	// Alt+D and Shift+Alt+D create a note and a folder.
	//
	// An Alt combination arrives as WM_SYSKEYDOWN, not WM_KEYDOWN -- Windows
	// routes it as a menu accelerator. Swallowing it here rather than passing it
	// on is also what stops the beep that an unhandled Alt key produces.
	//
	// The handle is fetched inside the handler, never captured when it is wired
	// up: the controls have no window handles until WM_CREATE, so a handle taken
	// here would be zero for the life of the program and every message passed to
	// DefSubclassProc would go nowhere.
	createKeys := func(c *ui.Edit) func(ui.Wm) uintptr {
		return func(m ui.Wm) uintptr {
			if co.VK(m.WParam) == co.VK('D') {
				p.createEntry(shiftDown())
				return 0
			}
			return c.Hwnd().DefSubclassProc(co.WM_SYSKEYDOWN, m.WParam, m.LParam)
		}
	}
	p.search.OnSubclass().Wm(co.WM_SYSKEYDOWN, createKeys(p.search))
	p.preview.OnSubclass().Wm(co.WM_SYSKEYDOWN, createKeys(p.preview))
	p.reqPane.OnSubclass().Wm(co.WM_SYSKEYDOWN, createKeys(p.reqPane))
	p.respPane.OnSubclass().Wm(co.WM_SYSKEYDOWN, createKeys(p.respPane))

	// An Alt combination arrives as a character as well as a key, and
	// DefWindowProc answers a character it cannot match to a menu mnemonic with
	// a beep. This window has no menu, so every one of them is a beep with
	// nothing behind it. The name box gets this too, where Alt+D is not a
	// command at all: naming a new note is no time to create another one.
	swallowSysChar := func(_ ui.Wm) uintptr { return 0 }
	p.search.OnSubclass().Wm(co.WM_SYSCHAR, swallowSysChar)
	p.preview.OnSubclass().Wm(co.WM_SYSCHAR, swallowSysChar)
	p.nameEdit.OnSubclass().Wm(co.WM_SYSCHAR, swallowSysChar)
	p.reqPane.OnSubclass().Wm(co.WM_SYSCHAR, swallowSysChar)
	p.respPane.OnSubclass().Wm(co.WM_SYSCHAR, swallowSysChar)

	// Swallow the characters those shortcuts would otherwise insert. Ctrl+
	// Backspace inserts DEL and Ctrl+S inserts 0x13; an Edit has no idea what
	// either means and puts a box glyph in the text.
	swallow := func(c *ui.Edit) func(ui.Wm) uintptr {
		return func(m ui.Wm) uintptr {
			if editChars(uint16(m.WParam)) {
				return 0
			}
			return c.Hwnd().DefSubclassProc(co.WM_CHAR, m.WParam, m.LParam)
		}
	}

	// Chaining used to live on Space, which meant a space could only be typed
	// into the search box with Shift held. Tab does the same job without taking
	// a printable character away from the query, so Space is now just a space.
	p.search.OnSubclass().Wm(co.WM_CHAR, swallow(p.search))
	p.preview.OnSubclass().Wm(co.WM_CHAR, swallow(p.preview))
	p.reqPane.OnSubclass().Wm(co.WM_CHAR, swallow(p.reqPane))
	p.respPane.OnSubclass().Wm(co.WM_CHAR, swallow(p.respPane))

	// Typing in the editor marks the note unsaved. quiet is set while the editor
	// is being filled from a file, which raises the same notification.
	p.preview.On().EnChange(func() {
		if p.mode == modeNotes && !p.quiet && p.curNote != "" {
			p.setDirty(true)
		}
	})

	// The same for the request pane, which is editable whenever it is holding a
	// request or the environments file.
	p.reqPane.On().EnChange(func() {
		if p.mode == modeAPI && !p.quiet && (p.curRequest != "" || p.envEditing) {
			p.setReqDirty(true)
		}
	})

	// Refuse every selection change the control tries to make.
	//
	// Not selecting from our own code was not enough: clicking a row makes the
	// control select it, and a selected row is painted in the system accent
	// colour with custom draw ignored. Vetoing here leaves the control with
	// nothing ever selected, so the highlight is only ever the one we draw.
	p.list.On().LvnItemChanging(func(_ *win.NMLISTVIEW) bool { return true })

	// Clicking a row moves the highlight to it -- and hands the keyboard back to
	// the search box.
	//
	// Clicking a list view gives it Win32 focus, and this window's whole keyboard
	// model is that focus lives in the search box and the list is driven from
	// there. Without this, clicking a row and then pressing Enter did nothing at
	// all: the key went to the list control, which has no handler for it, so the
	// entry was highlighted and could not be run. The same trap as gotcha 18 --
	// the display path worked and the commit path did not.
	p.list.On().NmClick(func(nm *win.NMITEMACTIVATE) {
		if nm.IItem >= 0 {
			p.setSelection(int(nm.IItem))
		}
		switch p.mode {
		case modePlaces:
			p.optFocus = false
		case modeAPI:
			p.apiFocus = apiFocusList
		}
		p.search.Hwnd().SetFocus()
		p.list.Hwnd().InvalidateRect(nil, true)
	})

	// Double-clicking runs it, the same as Enter.
	p.list.On().NmDblClk(func(nm *win.NMITEMACTIVATE) {
		if nm.IItem >= 0 {
			p.setSelection(int(nm.IItem))
			p.apply()
		}
	})

	// Paint the rows ourselves.
	//
	// A list view refuses to honour custom-draw colours for a row it considers
	// selected -- it always paints that row with system colours, which on a dark
	// window is flat grey with black text. So the control is never allowed to
	// select anything: the highlight is ours, tracked in selIdx and drawn here.
	p.list.On().NmCustomDraw(func(nm *win.NMLVCUSTOMDRAW) co.CDRF {
		// In places mode the arrow keys drive one of two lists, and which one is
		// shown by which highlight is lit: the active list keeps the accent
		// colour, the other dims to a muted one. Nothing else on the window says
		// where the keys are going.
		active := true
		switch p.mode {
		case modePlaces:
			active = !p.optFocus
		case modeAPI:
			active = p.apiFocus == apiFocusList
		}
		return p.drawRow(p.list, nm, p.selIdx, active)
	})

	// The action list on the right, in places mode. It gets the same treatment
	// as the main list for the same reason: a list view paints a selected row in
	// system colours and ignores custom draw, so it is never allowed to select.
	p.opts.On().LvnItemChanging(func(_ *win.NMLISTVIEW) bool { return true })

	p.opts.On().NmClick(func(nm *win.NMITEMACTIVATE) {
		if nm.IItem >= 0 && int(nm.IItem) < len(p.actions) {
			p.optIdx = int(nm.IItem)
			p.optFocus = true
			p.list.Hwnd().InvalidateRect(nil, true)
			p.opts.Hwnd().InvalidateRect(nil, true)
		}
	})

	p.opts.On().NmDblClk(func(nm *win.NMITEMACTIVATE) {
		if nm.IItem >= 0 && int(nm.IItem) < len(p.actions) {
			p.optIdx = int(nm.IItem)
			p.apply()
		}
	})

	p.opts.On().NmCustomDraw(func(nm *win.NMLVCUSTOMDRAW) co.CDRF {
		return p.drawRow(p.opts, nm, p.optIdx, p.mode == modePlaces && p.optFocus)
	})

	// How each control meets the acrylic backdrop, and which of them carry the
	// overlay scrollbar. Solid is everything holding text that is read or typed;
	// the two lists and the window background are the glass. Subclassing has to
	// be asked for before a control exists, which is why this is here and not in
	// applyLook with the rest of the appearance work -- see wireSurface.
	glass := func() win.HBRUSH { return p.theme.bg }
	solid := func() win.HBRUSH { return p.theme.surface }
	for _, s := range []surface{
		{ctrl: p.list, scrolls: true, fill: glass, radius: paneRadius},
		{ctrl: p.opts, scrolls: true, fill: glass, radius: paneRadius},
		{ctrl: p.preview, solid: true, scrolls: true, fill: solid, radius: paneRadius},
		{ctrl: p.reqPane, solid: true, scrolls: true, fill: solid, radius: paneRadius},
		{ctrl: p.respPane, solid: true, scrolls: true, fill: solid, radius: paneRadius},
		{ctrl: p.search, solid: true, radius: searchRadius},
		{ctrl: p.nameEdit, solid: true},
		{ctrl: p.pathLabel, solid: true, radius: paneRadius,
			label: co.DT_LEFT | co.DT_WORDBREAK | co.DT_NOPREFIX},
		{ctrl: p.statusLabel, solid: true, radius: paneRadius,
			label: co.DT_LEFT | co.DT_SINGLELINE | co.DT_VCENTER | co.DT_END_ELLIPSIS | co.DT_NOPREFIX},
	} {
		p.wireSurface(s)
	}
	// The tabs round the same way, and are the only controls that do it over the
	// window background rather than over a panel of their own.
	for i := range p.tabs {
		p.wireSurface(surface{ctrl: p.tabs[i], radius: tabRadius})
	}
}

// drawRow paints one list row. active says the list is the one the arrow keys
// are driving, which decides whether its highlight is the accent colour or the
// muted one.
//
// The row is drawn from scratch rather than handed back to the control with
// colours attached. Setting ClrTextBk is the cheap way and was what this did
// until the highlight had to be rounded: the control fills the row rectangle
// with that colour, and a row rectangle is the full width of the list with the
// next row hard against it, so there is no corner in it to round. Painting it
// here means a shape of our choosing inside the row, and the price is drawing
// the text as well -- affordable only because every list in this window is one
// column of plain text with no icons.
func (p *Palette) drawRow(lv *ui.ListView, nm *win.NMLVCUSTOMDRAW, sel int, active bool) co.CDRF {
	switch nm.Nmcd.DwDrawStage {
	case co.CDDS_PREPAINT:
		return co.CDRF_NOTIFYITEMDRAW
	case co.CDDS_ITEMPREPAINT:
		row := int(nm.Nmcd.DwItemSpec)
		hdc := nm.Nmcd.Hdc
		rc := lv.Item(row).ItemRect(co.LVIR_BOUNDS)
		// The row's own background has already been erased in the list's
		// background colour by the time the item is drawn, so an unhighlighted
		// row needs no fill at all -- which is what leaves the backdrop showing
		// through it.
		text := colText
		if row == sel {
			brush, pen := p.theme.accent, p.theme.accPen
			if active {
				text = colSelText
			} else {
				pen = p.theme.dimPen
			}
			oldBrush, _ := hdc.SelectObjectBrush(brush)
			oldPen, _ := hdc.SelectObjectPen(pen)
			// RoundRect takes the ellipse the corner is cut from, which is twice
			// the radius -- the same conversion roundCorners does.
			round := int32(ui.DpiX(selRadius) * 2)
			hdc.RoundRect(win.RECT{
				Left:   rc.Left + int32(ui.DpiX(selInsetX)),
				Top:    rc.Top + int32(ui.DpiY(selInsetY)),
				Right:  int32(contentWidth(lv.Hwnd()) - ui.DpiX(selInsetX)),
				Bottom: rc.Bottom - int32(ui.DpiY(selInsetY)),
			}, win.SIZE{Cx: round, Cy: round})
			hdc.SelectObjectBrush(oldBrush)
			hdc.SelectObjectPen(oldPen)
		}

		hdc.SetBkMode(co.BKMODE_TRANSPARENT)
		hdc.SetTextColor(text)
		box := win.RECT{
			Left:   rc.Left + int32(ui.DpiX(selInsetX+rowTextX)),
			Top:    rc.Top,
			Right:  int32(contentWidth(lv.Hwnd()) - ui.DpiX(selInsetX+rowTextX)),
			Bottom: rc.Bottom,
		}
		hdc.DrawText(lv.Item(row).Text(0), &box,
			co.DT_SINGLELINE|co.DT_VCENTER|co.DT_END_ELLIPSIS|co.DT_NOPREFIX)

		// Nothing more for the control to do. Letting it draw on top would put
		// its own text over ours, in its own colours, at its own offset.
		return co.CDRF_SKIPDEFAULT
	}
	return co.CDRF_DODEFAULT
}

// show captures the context and puts the palette on screen. It deliberately
// snapshots the clipboard and the target window before doing anything that
// could change either.
func (p *Palette) show() {
	// Appearance is applied here rather than in WM_CREATE: while the parent is
	// still being created the child controls do not reliably have window handles
	// yet, and styling a zero handle fails silently -- which looks exactly like
	// the styling code being wrong.
	firstShow := false
	p.look.Do(func() {
		p.applyLook()
		firstShow = true
	})

	// Asking for the palette during a recording means the recording is over --
	// clicking the tray icon or launching the app again is a way out if the stop
	// key does not reach us. stopRecording brings the window back itself, on the
	// macros tab, so there is nothing more to do here.
	if p.rec != nil {
		p.stopRecording()
		return
	}

	// Whatever the palette is being opened for, it is not the Ctrl+Z that was
	// being watched for.
	p.disarmUndo()

	if p.shown {
		p.search.Hwnd().SetFocus()
		return
	}

	p.target = winapi.ForegroundWindow()
	text, err := winapi.GetClipboardText()
	if err != nil {
		// No text on the clipboard is a normal state, not a failure: the user may
		// have copied a file or an image, or nothing at all.
		text = ""
	}
	p.input = text
	p.resetChain()

	p.search.SetText("")
	p.refilter()
	p.position()

	p.shown = true
	p.wnd.Hwnd().ShowWindow(co.SW_SHOW)
	winapi.RestoreForeground(uintptr(p.wnd.Hwnd()))
	p.search.Hwnd().SetFocus()
	if firstShow {
		p.wnd.Hwnd().PostMessage(wmRoundCorners, 0, 0)
	}
}

// onHotkey is what the global hotkey does.
//
// Closed, it opens the palette. Open, it flips to the other mode instead of
// closing -- one key gets you to the transforms and to the notes, and Esc is
// the way out. That is only reasonable because switching costs nothing: both
// indexes are already built and in memory, so the flip is a list rebuild and a
// repaint.
func (p *Palette) onHotkey() {
	if p.shown {
		p.setMode((p.mode + 1) % modeCount)
		// The window can be on screen without being the active one: Windows is
		// entitled to refuse a foreground change, and does when the user is busy
		// typing somewhere else. Without this the hotkey would flip the mode of a
		// window that cannot receive a keystroke, and the typing would go on
		// landing in whatever app really has focus.
		winapi.RestoreForeground(uintptr(p.wnd.Hwnd()))
		p.search.Hwnd().SetFocus()
		return
	}
	p.show()
}

// applyLook does the one-off appearance work: window chrome, fonts, control
// colours, dark scrollbars and rounded corners.
func (p *Palette) applyLook() {
	for _, err := range applyChrome(p.wnd.Hwnd()) {
		// Not fatal: these are Windows 11 features, absent on 10. Reported so a
		// square corner has a stated reason rather than being a mystery.
		fmt.Fprintln(os.Stderr, "window chrome:", err)
	}

	for _, h := range []win.HWND{p.search.Hwnd(), p.list.Hwnd(), p.preview.Hwnd(), p.opts.Hwnd(),
		p.pathLabel.Hwnd(), p.reqPane.Hwnd(), p.respPane.Hwnd(), p.statusLabel.Hwnd()} {
		p.theme.applyFont(h)
		darkenScrollbars(h)
	}

	// The overlay scrollbar's second half: push each scrolling control's native
	// bar out of sight by widening the control, and keep the editors from
	// wrapping their text into the strip that is about to be clipped away. The
	// painting and the drag were wired up in events(), long before this.
	//
	// Order matters -- the region below is measured from the window rectangle,
	// so it has to be applied after the widening.
	hideNativeVScroll(p.list.Hwnd())
	hideNativeVScroll(p.opts.Hwnd())
	for _, e := range []*ui.Edit{p.preview, p.reqPane, p.respPane} {
		hideNativeVScroll(e.Hwnd())
		setEditFormatRect(e.Hwnd())
	}

	trim := scrollbarInset()
	roundCorners(p.search.Hwnd(), ui.DpiX(searchRadius), 0)
	setEditMargins(p.search.Hwnd())
	for _, h := range []win.HWND{p.pathLabel.Hwnd(), p.statusLabel.Hwnd()} {
		roundCorners(h, ui.DpiX(paneRadius), 0)
	}
	for _, h := range []win.HWND{p.list.Hwnd(), p.opts.Hwnd(),
		p.preview.Hwnd(), p.reqPane.Hwnd(), p.respPane.Hwnd()} {
		roundCorners(h, ui.DpiX(paneRadius), trim)
	}
	// The name box is not rounded and not themed as a panel: it is an editing
	// overlay that appears on a row, and reading as a plain box is what makes it
	// obvious the row is being typed into.
	p.theme.applyFont(p.nameEdit.Hwnd())
	p.theme.applyFont(p.chainLabel.Hwnd())

	tr := ui.DpiX(tabRadius)
	for _, t := range p.tabs {
		p.theme.applyFont(t.Hwnd())
		roundCorners(t.Hwnd(), tr, 0)
	}
	darkenListView(p.list.Hwnd())
	darkenListView(p.opts.Hwnd())
	setCueBanner(p.search.Hwnd(), "Search transforms")
	winapi.HideFocusRectangles(uintptr(p.wnd.Hwnd()))
}

// hide dismisses the palette. restoreFocus is false when Windows has already
// moved focus elsewhere, in which case forcing it back would yank the user out
// of whatever they just clicked on.
func (p *Palette) hide(restoreFocus bool) {
	if !p.shown {
		return
	}
	p.commitRename()
	// Closing must not be a way to lose a note. The window can go away because
	// Esc was pressed, because something else was clicked, or because a paste
	// took focus back -- none of them is a decision to discard the text.
	p.saveIfDirty()
	p.shown = false
	p.wnd.Hwnd().ShowWindow(co.SW_HIDE)
	if restoreFocus {
		winapi.RestoreForeground(p.target)
	}
}

// position parks the window in the bottom-right corner.
//
// It measures against the work area rather than the raw screen, so the palette
// sits above the taskbar instead of underneath it. If that query fails for any
// reason it falls back to full screen metrics, which is merely less tidy.
func (p *Palette) position() {
	w, h := ui.Dpi(winW, winH)
	m := ui.DpiX(margin)

	right := int(win.GetSystemMetrics(co.SM_CXSCREEN))
	bottom := int(win.GetSystemMetrics(co.SM_CYSCREEN))

	var work win.RECT
	if err := win.SystemParametersInfo(co.SPI_GETWORKAREA, 0, unsafe.Pointer(&work), co.SPIF(0)); err == nil {
		right, bottom = int(work.Right), int(work.Bottom)
	}

	p.wnd.Hwnd().SetWindowPos(win.HWND(0),
		win.POINT{X: int32(right - w - m), Y: int32(bottom - h - m)},
		win.SIZE{}, co.SWP_NOSIZE|co.SWP_NOZORDER|co.SWP_NOACTIVATE)
}

// refilter re-runs the search and repopulates the list.
func (p *Palette) refilter() { p.refilterKeeping("") }

// refilterKeeping is refilter with a row to land on: the id to re-select once
// the list has been rebuilt, or "" to go back to the top.
func (p *Palette) refilterKeeping(keep string) {
	p.visible = p.indexes[p.mode].Search(p.search.Text())

	// Hold painting off while the list is torn down and rebuilt.
	//
	// Every delete and insert would otherwise repaint, and so would the scrollbar
	// appearing or disappearing as the row count changes -- which is why the
	// flicker only showed up once the list was long enough to need one. With
	// redraw suppressed, all of that happens invisibly and the control paints
	// once at the end.
	p.list.SetRedraw(false)

	p.list.DeleteAllItems()
	for _, it := range p.visible {
		switch p.mode {
		case modeNotes:
			p.list.AddItem(noteLabel(it))
		case modePlaces:
			p.list.AddItem(placeLabel(it))
		case modeMacros:
			p.list.AddItem(macroLabel(it))
		case modeAPI:
			p.list.AddItem(apiLabel(it))
		default:
			p.list.AddItem(label(it))
		}
	}

	// Stretch the column to the full client width. Done after populating,
	// because whether a vertical scrollbar is present changes that width -- and
	// see columnWidth for why it is the full width rather than the width the
	// text is allowed to use.
	if p.list.ColCount() > 0 {
		p.list.Col(0).SetWidth(columnWidth(p.list.Hwnd()))
	}

	p.list.SetRedraw(true)

	sel := 0
	if keep != "" {
		for i, it := range p.visible {
			if it.ID == keep {
				sel = i
				break
			}
		}
	}
	p.setSelection(sel)
}

// currentID is the id of the highlighted row, or "" if there is none. Used to
// stay on the same entry when the list is rebuilt underneath it.
func (p *Palette) currentID() string {
	if it, ok := p.selected(); ok {
		return it.ID
	}
	return ""
}

// selected returns the highlighted item.
func (p *Palette) selected() (fuzzy.Item, bool) {
	if len(p.visible) == 0 {
		return fuzzy.Item{}, false
	}
	if p.selIdx < 0 || p.selIdx >= len(p.visible) {
		return p.visible[0], true
	}
	return p.visible[p.selIdx], true
}

func (p *Palette) moveSelection(delta int) {
	if len(p.visible) == 0 {
		return
	}
	p.setSelection(step(p.selIdx, delta, len(p.visible)))
}

// jumpRows is how far Ctrl+Arrow moves in one press. Five is enough to cross a
// list quickly without losing your place in it, which a full page does.
const jumpRows = 5

// arrowStep is one row, or a jump while Ctrl is held.
func arrowStep() int {
	if ctrlDown() {
		return jumpRows
	}
	return 1
}

// step applies a movement to an index in a list of n, and is the one place that
// decides what happens at the ends.
//
// A single step wraps: Up from the top reaching the bottom is how every palette
// behaves and is what makes a short list quick to cycle. A jump clamps instead
// -- Ctrl+Down four rows from the end means "take me to the end", and wrapping
// it round to the top would lose the position the jump was made from, which is
// the one thing the key exists to preserve.
func step(cur, delta, n int) int {
	next := cur + delta
	switch {
	case delta < -1 || delta > 1:
		if next < 0 {
			next = 0
		}
		if next >= n {
			next = n - 1
		}
	case next < 0:
		next = n - 1
	case next >= n:
		next = 0
	}
	return next
}

// setSelection moves the highlight, scrolls it into view and refreshes the
// preview. Redrawing the whole list is cheap at this size and avoids tracking
// which two rows changed.
func (p *Palette) setSelection(i int) {
	// The box is placed over one specific row. Moving the highlight, filtering
	// the list or switching mode all leave it hanging over the wrong thing, so
	// the name is taken as given first -- the same as clicking away.
	p.commitRename()
	p.selIdx = i
	if i >= 0 && i < len(p.visible) {
		p.list.Item(i).EnsureVisible()
	}
	p.list.Hwnd().InvalidateRect(nil, true)
	p.updatePreview()
}

// updatePreview runs the highlighted transform against the captured clipboard
// text and shows the result.
//
// The run happens on a worker goroutine and the answer arrives back as a
// wmRunDone message. A user script is arbitrary JavaScript -- running it inline
// would freeze the window for as long as it takes, up to the two-second
// timeout for one with an accidental infinite loop.
func (p *Palette) updatePreview() {
	switch p.mode {
	case modeNotes:
		p.showNote()
		return
	case modePlaces:
		// A different place means a different set of actions, so the highlight
		// goes back to the first -- which is the one Enter runs.
		p.optIdx = 0
		p.showPlace()
		return
	case modeMacros:
		p.showMacro()
		return
	case modeAPI:
		p.showRequest()
		return
	}

	it, ok := p.selected()
	if !ok {
		p.preview.SetText("")
		return
	}
	t, ok := it.Data.(*transform.Transform)
	if !ok {
		p.preview.SetText("")
		return
	}
	p.runner.start(p.wnd.Hwnd(), t, p.input, runPreview)
}

// apply runs the highlighted transform for real: result to the clipboard,
// palette away, focus back, and optionally a synthesised paste.
//
// Like the preview it runs on a worker; finishApply does the rest once the
// result comes back.
func (p *Palette) apply() {
	switch p.mode {
	case modeNotes:
		p.applyNote()
		return
	case modePlaces:
		p.applyPlace()
		return
	case modeMacros:
		p.applyMacro()
		return
	case modeAPI:
		p.sendRequest()
		return
	}

	it, ok := p.selected()
	if !ok {
		return
	}
	t, ok := it.Data.(*transform.Transform)
	if !ok {
		return
	}
	p.runner.start(p.wnd.Hwnd(), t, p.input, runApply)
}

// finishApply handles a completed apply run, back on the UI thread.
func (p *Palette) finishApply(res runResult) {
	if res.err != nil {
		// Stay open on failure. Closing would hide the reason and leave the user
		// wondering why nothing happened.
		p.preview.SetText(toCRLF("error: " + res.err.Error()))
		return
	}

	if err := winapi.SetClipboardText(res.out); err != nil {
		p.preview.SetText(toCRLF("could not write to clipboard: " + err.Error()))
		return
	}

	p.hide(true)

	if p.cfg.AutoPaste {
		winapi.SendPaste()
	}
}

// label renders a palette row as a readable phrase: "Cases: Camel case".
func label(it fuzzy.Item) string {
	if it.Group == "" {
		return it.Name
	}
	return it.Group + ": " + it.Name
}

// toCRLF converts newlines for display: a Win32 Edit control renders a bare \n
// as a box rather than a line break.
func toCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// fromCRLF undoes it, for text on its way back out of a control.
func fromCRLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func (p *Palette) fatal(title string, err error) {
	p.wnd.Hwnd().MessageBox(err.Error(), title, co.MB_ICONERROR)
}

// confirm asks before something irreversible.
//
// The default button is No, because this is a palette driven entirely by the
// keyboard: Del followed by a reflexive Enter must not be a way to lose a file.
func (p *Palette) confirm(title, text string) bool {
	answer, err := p.wnd.Hwnd().MessageBox(text, title,
		co.MB_YESNO|co.MB_ICONWARNING|co.MB_DEFBUTTON2)
	// A dialog that could not be shown is not consent.
	return err == nil && answer == co.ID_YES
}

// createEntry is Alt+D: a new note, a new folder, or a new place, depending on
// which tab is showing.
func (p *Palette) createEntry(asFolder bool) {
	switch p.mode {
	case modePlaces:
		p.createPlace()
	case modeMacros:
		p.createMacro()
	case modeAPI:
		p.createRequest(asFolder)
	default:
		p.createNote(asFolder)
	}
}

// deleteEntry is Del: the highlighted note, or the highlighted place. Both ask
// first, and what they mean by "delete" differs -- a note is a file and goes
// for good, a place is a shortcut and only the shortcut goes.
func (p *Palette) deleteEntry() {
	switch p.mode {
	case modeNotes:
		p.deleteNote()
	case modePlaces:
		p.deletePlace()
	case modeMacros:
		p.deleteMacro()
	case modeAPI:
		p.deleteRequest()
	}
}
