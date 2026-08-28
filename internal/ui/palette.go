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

	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/winapi"
)

const hotkeyID = 1

// Layout.
//
// These constants are the only numbers that decide how the palette looks and
// where it sits. Change them and rebuild -- nothing else needs touching.
const (
	winW, winH = 600, 360 // overall window size
	margin     = 20       // gap from the corner of the screen's work area
	pad        = 10       // padding inside the window
	searchH    = 24       // height of the search box
	listW      = 250      // width of the transform list
	rowGap     = 8        // gap between the search box and the panes below
	radius     = 10       // corner radius of the search box, list and preview
)

// Palette is the command palette window.
type Palette struct {
	wnd     *ui.Main
	search  *ui.Edit
	list    *ui.ListView
	preview *ui.Edit

	cfg    config.Config
	reg    *transform.Registry
	index  *fuzzy.Index
	theme  *theme
	tray   *tray
	loader scriptLoader
	runner *runner

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

	// shown guards against re-entering hide() while hiding, which the
	// WM_ACTIVATE handler would otherwise cause.
	shown bool
}

// New builds the palette window and its controls. Nothing is displayed yet --
// the window is created hidden and stays that way until the hotkey fires.
func New(cfg config.Config, reg *transform.Registry, loader scriptLoader) (*Palette, error) {
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
			CmdShow(co.SW_HIDE),
	)

	search := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(pad, pad)).
			Width(ui.DpiX(winW-pad*2)).
			Height(ui.DpiY(searchH)).
			// No border. windigo defaults every control to WS_EX_CLIENTEDGE -- the
			// sunken 3D edge -- so the extended style must be overridden too;
			// clearing WndStyle alone leaves the border in place.
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	listTop := pad + searchH + rowGap
	listH := winH - listTop - pad

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
			Position(ui.Dpi(pad+listW+rowGap, listTop)).
			Width(ui.DpiX(winW-listW-rowGap-pad*2)).
			Height(ui.DpiY(listH)).
			CtrlStyle(co.ES_MULTILINE|co.ES_READONLY|co.ES_AUTOVSCROLL).
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_VSCROLL|co.WS_TABSTOP).
			WndExStyle(co.WS_EX_LEFT),
	)

	tr, err := newTray()
	if err != nil {
		return nil, err
	}

	p := &Palette{
		wnd:     wnd,
		search:  search,
		list:    list,
		preview: preview,
		cfg:     cfg,
		reg:     reg,
		theme:   th,
		tray:    tr,
		loader:  loader,
		runner:  newRunner(),
	}
	p.reloadScripts()
	p.events()
	return p, nil
}

// Run shows nothing and blocks, pumping messages until the app exits.
func (p *Palette) Run() int { return p.wnd.RunAsMain() }

// rebuildIndex refreshes the searchable index from the registry. It will be
// called again on script hot-reload in M2.
func (p *Palette) rebuildIndex() {
	all := p.reg.All()
	items := make([]fuzzy.Item, 0, len(all))
	for _, t := range all {
		items = append(items, fuzzy.Item{
			ID: t.ID, Name: t.Name, Group: t.Group, Tags: t.Tags, Data: t,
		})
	}
	p.index = fuzzy.New(items)
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

		p.watchScripts(p.wnd.Hwnd())

		if err := p.tray.add(p.wnd.Hwnd(), "quick-tools -- "+p.cfg.Hotkey); err != nil {
			// Without the icon there is no way to quit short of Task Manager, so
			// this is worth telling the user about rather than starting anyway.
			p.fatal("Could not create the tray icon", err)
		}
		return 0
	})

	// Edit controls ask their parent what colour to be. Without this they stay
	// white, which on a dark window is the single most obvious thing wrong.
	colorEdit := func(m ui.Wm) uintptr { return p.theme.paintControlBackground(m) }
	p.wnd.On().Wm(co.WM_CTLCOLOREDIT, colorEdit)
	p.wnd.On().Wm(co.WM_CTLCOLORSTATIC, colorEdit)

	p.wnd.On().Wm(co.WM_HOTKEY, func(_ ui.Wm) uintptr {
		p.show()
		return 0
	})

	// Clicking away dismisses the palette, the same as Esc.
	p.wnd.On().Wm(co.WM_ACTIVATE, func(m ui.Wm) uintptr {
		if m.WParam.LoWord() == 0 { // WA_INACTIVE
			p.hide(false)
		}
		return 0
	})

	p.wnd.On().WmDestroy(func() {
		winapi.UnregisterHotkeyFor(uintptr(p.wnd.Hwnd()), hotkeyID)
		p.tray.remove(p.wnd.Hwnd())
		p.theme.destroy()
	})

	p.trayEvents()
	p.runEvents()

	p.search.On().EnChange(func() { p.refilter() })

	// An Edit swallows arrow keys and Enter, so the search box is subclassed to
	// steer them at the list instead. This is the standard cost of putting a
	// list under a text field in Win32.
	p.search.OnSubclass().Wm(co.WM_KEYDOWN, func(m ui.Wm) uintptr {
		switch co.VK(m.WParam) {
		case co.VK_DOWN:
			p.moveSelection(1)
			return 0
		case co.VK_UP:
			p.moveSelection(-1)
			return 0
		case co.VK_RETURN:
			p.apply()
			return 0
		case co.VK_ESCAPE:
			p.hide(true)
			return 0
		}
		return p.search.Hwnd().DefSubclassProc(co.WM_KEYDOWN, m.WParam, m.LParam)
	})

	// Refuse every selection change the control tries to make.
	//
	// Not selecting from our own code was not enough: clicking a row makes the
	// control select it, and a selected row is painted in the system accent
	// colour with custom draw ignored. Vetoing here leaves the control with
	// nothing ever selected, so the highlight is only ever the one we draw.
	p.list.On().LvnItemChanging(func(_ *win.NMLISTVIEW) bool { return true })

	// Clicking a row moves the highlight to it.
	p.list.On().NmClick(func(nm *win.NMITEMACTIVATE) {
		if nm.IItem >= 0 {
			p.setSelection(int(nm.IItem))
		}
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
		switch nm.Nmcd.DwDrawStage {
		case co.CDDS_PREPAINT:
			return co.CDRF_NOTIFYITEMDRAW
		case co.CDDS_ITEMPREPAINT:
			if int(nm.Nmcd.DwItemSpec) == p.selIdx {
				nm.ClrTextBk, nm.ClrText = colSel, colSelText
			} else {
				nm.ClrTextBk, nm.ClrText = colSurface, colText
			}
			// Clearing the focus bit is what removes the dotted rectangle the list
			// draws around a clicked row. WM_UPDATEUISTATE alone does not cover a
			// row focused by mouse.
			nm.Nmcd.UItemState &^= co.CDIS_FOCUS
			return co.CDRF_NEWFONT
		}
		return co.CDRF_DODEFAULT
	})
}

// show captures the context and puts the palette on screen. It deliberately
// snapshots the clipboard and the target window before doing anything that
// could change either.
func (p *Palette) show() {
	// Appearance is applied here rather than in WM_CREATE: while the parent is
	// still being created the child controls do not reliably have window handles
	// yet, and styling a zero handle fails silently -- which looks exactly like
	// the styling code being wrong.
	p.look.Do(p.applyLook)

	if p.shown {
		p.hide(true)
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

	p.search.SetText("")
	p.refilter()
	p.position()

	p.shown = true
	p.wnd.Hwnd().ShowWindow(co.SW_SHOW)
	winapi.RestoreForeground(uintptr(p.wnd.Hwnd()))
	p.search.Hwnd().SetFocus()
}

// applyLook does the one-off appearance work: window chrome, fonts, control
// colours, dark scrollbars and rounded corners.
func (p *Palette) applyLook() {
	for _, err := range applyChrome(p.wnd.Hwnd()) {
		// Not fatal: these are Windows 11 features, absent on 10. Reported so a
		// square corner has a stated reason rather than being a mystery.
		fmt.Fprintln(os.Stderr, "window chrome:", err)
	}

	r := ui.DpiX(radius)
	for _, h := range []win.HWND{p.search.Hwnd(), p.list.Hwnd(), p.preview.Hwnd()} {
		p.theme.applyFont(h)
		darkenScrollbars(h)
		roundCorners(h, r)
	}
	darkenListView(p.list.Hwnd())
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
func (p *Palette) refilter() {
	p.visible = p.index.Search(p.search.Text())

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
		p.list.AddItem(label(it))
	}

	// Stretch the column to the full client width. Done after populating,
	// because whether a vertical scrollbar is present changes that width.
	if p.list.ColCount() > 0 {
		p.list.Col(0).SetWidthToFill()
	}

	p.list.SetRedraw(true)
	p.setSelection(0)
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
	next := p.selIdx + delta
	switch {
	case next < 0:
		next = len(p.visible) - 1 // wrap, so Up from the top reaches the bottom
	case next >= len(p.visible):
		next = 0
	}
	p.setSelection(next)
}

// setSelection moves the highlight, scrolls it into view and refreshes the
// preview. Redrawing the whole list is cheap at this size and avoids tracking
// which two rows changed.
func (p *Palette) setSelection(i int) {
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
	p.runner.start(p.wnd.Hwnd(), t, p.input, false)
}

// apply runs the highlighted transform for real: result to the clipboard,
// palette away, focus back, and optionally a synthesised paste.
//
// Like the preview it runs on a worker; finishApply does the rest once the
// result comes back.
func (p *Palette) apply() {
	it, ok := p.selected()
	if !ok {
		return
	}
	t, ok := it.Data.(*transform.Transform)
	if !ok {
		return
	}
	p.runner.start(p.wnd.Hwnd(), t, p.input, true)
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

func (p *Palette) fatal(title string, err error) {
	p.wnd.Hwnd().MessageBox(err.Error(), title, co.MB_ICONERROR)
}
