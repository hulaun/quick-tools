//go:build windows

// Package ui implements the palette window.
//
// The window is created once at startup and then merely shown and hidden. That
// is the whole basis of the app feeling instant: opening it is a ShowWindow
// call against a window that already exists, not a window being built.
package ui

import (
	"fmt"
	"strings"
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
)

// Palette is the command palette window.
type Palette struct {
	wnd     *ui.Main
	search  *ui.Edit
	list    *ui.ListView
	preview *ui.Edit

	cfg   config.Config
	reg   *transform.Registry
	index *fuzzy.Index

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

	// shown guards against re-entering hide() while hiding, which the
	// WM_ACTIVATE handler would otherwise cause.
	shown bool
}

// New builds the palette window and its controls. Nothing is displayed yet --
// the window is created hidden and stays that way until the hotkey fires.
func New(cfg config.Config, reg *transform.Registry) *Palette {
	wnd := ui.NewMain(
		ui.OptsMain().
			Title("quick-tools").
			Size(ui.Dpi(winW, winH)).
			Style(co.WS_POPUP | co.WS_BORDER | co.WS_CLIPCHILDREN).
			// TOOLWINDOW keeps it out of the taskbar and Alt+Tab; TOPMOST keeps it
			// above the window we are about to paste into.
			ExStyle(co.WS_EX_TOOLWINDOW | co.WS_EX_TOPMOST).
			CmdShow(co.SW_HIDE),
	)

	search := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(pad, pad)).
			Width(ui.DpiX(winW-pad*2)).
			Height(ui.DpiY(searchH)),
	)

	listTop := pad + searchH + rowGap
	listH := winH - listTop - pad

	list := ui.NewListView(wnd,
		ui.OptsListView().
			Position(ui.Dpi(pad, listTop)).
			Size(ui.Dpi(listW, listH)).
			CtrlStyle(co.LVS_REPORT|co.LVS_SINGLESEL|co.LVS_NOCOLUMNHEADER|co.LVS_SHOWSELALWAYS).
			CtrlExStyle(co.LVS_EX_FULLROWSELECT).
			// One column holding "Group: Name". A separate group column was harder
			// to read than simply writing the label out as a phrase.
			Column("Transform", listW-24),
	)

	preview := ui.NewEdit(wnd,
		ui.OptsEdit().
			Position(ui.Dpi(pad+listW+rowGap, listTop)).
			Width(ui.DpiX(winW-listW-rowGap-pad*2)).
			Height(ui.DpiY(listH)).
			CtrlStyle(co.ES_MULTILINE|co.ES_READONLY|co.ES_AUTOVSCROLL).
			WndStyle(co.WS_CHILD|co.WS_VISIBLE|co.WS_BORDER|co.WS_VSCROLL|co.WS_TABSTOP),
	)

	p := &Palette{
		wnd:     wnd,
		search:  search,
		list:    list,
		preview: preview,
		cfg:     cfg,
		reg:     reg,
	}
	p.rebuildIndex()
	p.events()
	return p
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
		return 0
	})

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
	})

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

	// Keep the preview in step with whatever row is highlighted.
	p.list.On().LvnItemChanged(func(nm *win.NMLISTVIEW) {
		if nm.UNewState&co.LVIS_SELECTED != 0 {
			p.updatePreview()
		}
	})
}

// show captures the context and puts the palette on screen. It deliberately
// snapshots the clipboard and the target window before doing anything that
// could change either.
func (p *Palette) show() {
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

	p.list.DeleteAllItems()
	for _, it := range p.visible {
		p.list.AddItem(label(it))
	}
	if len(p.visible) > 0 {
		p.list.Item(0).Select(true).Focus()
	}
	p.updatePreview()
}

// selected returns the highlighted item.
func (p *Palette) selected() (fuzzy.Item, bool) {
	if len(p.visible) == 0 {
		return fuzzy.Item{}, false
	}
	item, ok := p.list.FocusedItem()
	if !ok {
		return p.visible[0], true
	}
	i := item.Index()
	if i < 0 || i >= len(p.visible) {
		return p.visible[0], true
	}
	return p.visible[i], true
}

func (p *Palette) moveSelection(delta int) {
	if len(p.visible) == 0 {
		return
	}
	cur := 0
	if item, ok := p.list.FocusedItem(); ok {
		cur = item.Index()
	}
	next := cur + delta
	switch {
	case next < 0:
		next = len(p.visible) - 1 // wrap, so Up from the top reaches the bottom
	case next >= len(p.visible):
		next = 0
	}
	p.list.Item(next).Select(true).Focus().EnsureVisible()
	p.updatePreview()
}

// updatePreview runs the highlighted transform against the captured clipboard
// text and shows the result.
//
// M1 runs this synchronously, which is safe because every built-in transform is
// pure string manipulation measured in microseconds. This function is the single
// chokepoint where that must change: when goja scripts arrive in M2, the call to
// Run must move to a worker goroutine that posts its result back, or a slow
// script will freeze the window.
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
	out, err := t.Run(p.input)
	if err != nil {
		p.preview.SetText(toCRLF("error: " + err.Error()))
		return
	}
	p.preview.SetText(toCRLF(out))
}

// apply runs the highlighted transform for real: result to the clipboard,
// palette away, focus back, and optionally a synthesised paste.
func (p *Palette) apply() {
	it, ok := p.selected()
	if !ok {
		return
	}
	t, ok := it.Data.(*transform.Transform)
	if !ok {
		return
	}

	out, err := t.Run(p.input)
	if err != nil {
		// Stay open on failure. Closing would hide the reason and leave the user
		// wondering why nothing happened.
		p.preview.SetText(toCRLF("error: " + err.Error()))
		return
	}

	if err := winapi.SetClipboardText(out); err != nil {
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
