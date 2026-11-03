//go:build windows

package ui

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/winapi"
)

// colNoBorder is DWMWA_COLOR_NONE, not a colour: handed to
// DWMWA_BORDER_COLOR it removes the window's outer border entirely rather than
// painting one. Any real colour draws a one-pixel line around the window.
const colNoBorder win.COLORREF = 0xffff_fffe

// Colours: a flat dark theme, zinc.
//
// Change these and rebuild; they are the only place colour is decided, and
// since the window became opaque (see applyChrome) none of them is structural
// any more -- every one is a free choice.
//
// It did not used to be. The window sat on an acrylic blur until 2026-10-27,
// and the background had to be *black* because black is what the compositor
// reads as "nothing painted here, show the material". That is why the window
// and the list behind the rows were once the same colour and could not be
// separated. They can be now, and the depth comes from three flat values
// instead: the window, a panel on it, and a row highlighted on the panel.
var (
	colBg      = win.RGB(0x09, 0x09, 0x0b) // window background, and the list behind the rows
	colSurface = win.RGB(0x18, 0x18, 0x1b) // the panels: search box, editor, request, response
	colText    = win.RGB(0xfa, 0xfa, 0xfa) // primary text
	colBorder  = win.RGB(0x27, 0x27, 0x2a) // the window's outer border: one pixel
	colSel     = win.RGB(0x27, 0x27, 0x2a) // highlighted row and the active tab
	colSelDim  = win.RGB(0x27, 0x27, 0x2a) // highlighted row in the list that is not taking the arrow keys
	colSelText = win.RGB(0xfa, 0xfa, 0xfa) // text on a highlighted row
	colTextDim = win.RGB(0xa1, 0xa1, 0xaa) // the inactive tab
	colScroll  = win.RGB(0x52, 0x52, 0x5b) // the overlay scrollbar thumb
)

// There is no colour here for a text selection, and that is a decision rather
// than an omission. A selection is painted by the control in the system
// highlight colour -- the Windows accent -- and nothing overrides it: the
// colours a window hands back from WM_CTLCOLOREDIT are the unselected pair, and
// an edit has no message for the other one.
//
// It was recoloured to a grey afterwards until 2026-10-27, so that the caret
// kept its colour -- Windows draws a caret by inverting the pixels under it, so
// over the accent it comes out #ff872b and a selection means an orange caret.
// That is gone, and the caret is orange again. A recolour can only ever arrive
// after the control has painted the blue, and at the speed a mouse drag
// repaints, what you see is both of them, over and over. The only real fix is
// to draw the text ourselves, which is a milestone and not a colour. Gotcha 59.

// fontFace and fontPt set the UI font. Segoe UI is what the rest of Windows 11
// uses; the default GUI font is a decade older and looks it.
const (
	fontFace = "Segoe UI"
	fontPt   = 14
)

// theme owns the GDI objects the window paints with. They are created once and
// destroyed with the window -- GDI handles are a finite system resource, so
// creating them per paint is a leak that eventually takes down the desktop.
type theme struct {
	bg      win.HBRUSH
	surface win.HBRUSH
	accent  win.HBRUSH
	accPen  win.HPEN
	dimPen  win.HPEN
	scroll  win.HBRUSH
	scrPen  win.HPEN
	font    win.HFONT
}

// solidBrush creates a plain filled brush. windigo exposes only the general
// CreateBrushIndirect, so this wraps the LOGBRUSH boilerplate once.
func solidBrush(c win.COLORREF) (win.HBRUSH, error) {
	return win.CreateBrushIndirect(&win.LOGBRUSH{Style: co.BRS_SOLID, Color: c})
}

// solidPen creates a one-pixel pen. RoundRect outlines with the current pen and
// fills with the current brush, so every rounded shape here needs a pen of its
// own fill colour -- leave the stock black pen selected and the shape comes out
// with a dark ring around it.
func solidPen(c win.COLORREF) (win.HPEN, error) {
	return win.CreatePen(co.PS_SOLID, 1, c)
}

func newTheme() (*theme, error) {
	bg, err := solidBrush(colBg)
	if err != nil {
		return nil, err
	}
	surface, err := solidBrush(colSurface)
	if err != nil {
		return nil, err
	}
	accent, err := solidBrush(colSel)
	if err != nil {
		return nil, err
	}
	accPen, err := solidPen(colSel)
	if err != nil {
		return nil, err
	}
	dimPen, err := solidPen(colSelDim)
	if err != nil {
		return nil, err
	}
	scroll, err := solidBrush(colScroll)
	if err != nil {
		return nil, err
	}
	scrPen, err := solidPen(colScroll)
	if err != nil {
		return nil, err
	}

	var lf win.LOGFONT
	// A negative height asks for a character height in points rather than the
	// cell height, which is what every design tool means by "font size".
	lf.Height = int32(-ui.DpiY(fontPt))
	lf.Weight = co.FW_NORMAL
	lf.CharSet = co.CHARSET_DEFAULT
	lf.Quality = co.QUALITY_CLEARTYPE
	lf.SetFaceName(fontFace)

	font, err := win.CreateFontIndirect(&lf)
	if err != nil {
		return nil, err
	}

	return &theme{
		bg: bg, surface: surface, accent: accent,
		accPen: accPen, dimPen: dimPen,
		scroll: scroll, scrPen: scrPen,
		font: font,
	}, nil
}

func (t *theme) destroy() {
	if t == nil {
		return
	}
	t.bg.DeleteObject()
	t.surface.DeleteObject()
	t.accent.DeleteObject()
	t.accPen.DeleteObject()
	t.dimPen.DeleteObject()
	t.scroll.DeleteObject()
	t.scrPen.DeleteObject()
	t.font.DeleteObject()
}

// applyFont sets the theme font on a control.
func (t *theme) applyFont(h win.HWND) {
	h.SendMessage(co.WM_SETFONT, win.WPARAM(t.font), win.LPARAM(1))
}

// applyChrome asks for modern window decoration: rounded corners, dark
// scrollbars and context menus, and a one-pixel border.
//
// **The window is opaque, and that is a change.** It sat on a real acrylic blur
// until 2026-10-27, through the undocumented SetWindowCompositionAttribute --
// which worked, and cost more than it was worth. GDI has no alpha channel, so
// the compositor read every pixel the window painted as "nothing here"; keeping
// a control solid meant copying its finished pixels out, setting the alpha byte
// and copying them back, after every paint. That pass could only ever run
// *after* the control had painted, and a control dragging out a selection
// repaints without going through WM_PAINT at all -- so a composition frame
// landing in the gap showed the backdrop straight through the selected lines.
// Flat colour has no gap to land in. Gotcha 59, and the whole of winapi's
// opaque.go and acrylic.go went with it.
//
// The border matters more than it looks now. A flat dark window over a dark
// desktop has nothing to separate it from the wallpaper, and the blur used to
// do that work; colBorder is what does it instead.
//
// All three attributes are Windows 11 only and fail harmlessly on 10, leaving
// square corners. Every failure is returned rather than swallowed so that a
// square window has a stated reason instead of being a mystery; the caller logs
// them and carries on.
func applyChrome(h win.HWND) []error {
	var errs []error
	set := func(what string, a win.DwmAttr) {
		if err := h.DwmSetWindowAttribute(a); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", what, err))
		}
	}
	set("dark mode", win.DwmAttrUseImmersiveDarkMode(true))
	set("rounded corners", win.DwmAttrWindowCornerPreference(co.DWMWCP_ROUND))
	set("border colour", win.DwmAttrBorderColor(colBorder))
	return errs
}

// darkenScrollbars asks the theme engine to draw a control's scrollbars in the
// dark style.
//
// Every scrollbar the user sees is now the overlay drawn in scrollbar.go, so
// this no longer decides how they look. It stays because SetWindowTheme also
// darkens the control's context menu and tooltips, and because a control that
// somehow escapes the overlay should still not show a white bar.
func darkenScrollbars(h win.HWND) {
	winapi.SetWindowTheme(uintptr(h), "DarkMode_Explorer")
}

// paintControlBackground handles WM_CTLCOLOR* for a control, colouring its text
// and background and handing back the brush Windows should fill it with.
//
// This is the *panel* path: the search box and the editing panes take it, and
// the surface colour is what lifts them off the window behind them. Anything
// holding text you type or read at length is a panel; the window and the list
// are the ground it sits on.
//
// Returning the brush is the part that is easy to miss: setting the colours on
// the device context alone leaves the control's unpainted area the default
// white, so a dark theme ends up with light rectangles behind the text.
func (t *theme) paintControlBackground(m ui.Wm) uintptr {
	hdc := win.HDC(m.WParam)
	hdc.SetTextColor(colText)
	hdc.SetBkColor(colSurface)
	return uintptr(t.surface)
}

// paintTab colours one of the mode tabs. The active one is filled with the
// accent colour, which is the same fill the highlighted row uses -- the two
// together are what say "this list is showing this mode". An inactive tab takes
// the window background, so the tabs read as one label lit and the rest bare.
func (t *theme) paintTab(m ui.Wm, active bool) uintptr {
	hdc := win.HDC(m.WParam)
	if active {
		hdc.SetTextColor(colSelText)
		hdc.SetBkColor(colSel)
		return uintptr(t.accent)
	}
	hdc.SetTextColor(colTextDim)
	hdc.SetBkColor(colBg)
	return uintptr(t.bg)
}

// paintChainLabel colours the strip beside the tabs. It sits directly on the
// window rather than on a panel, so it takes the window background: a surface
// rectangle floating in the header would read as a control you could click.
func (t *theme) paintChainLabel(m ui.Wm) uintptr {
	hdc := win.HDC(m.WParam)
	hdc.SetTextColor(colTextDim)
	hdc.SetBkColor(colBg)
	return uintptr(t.bg)
}

// paintPathLabel colours the strip at the top of the places pane. It takes the
// panel surface rather than the window background, because it is the top half
// of the pane the action list finishes -- the two read as one panel.
func (t *theme) paintPathLabel(m ui.Wm) uintptr {
	hdc := win.HDC(m.WParam)
	hdc.SetTextColor(colTextDim)
	hdc.SetBkColor(colSurface)
	return uintptr(t.surface)
}

// setCueBanner puts placeholder text in an empty edit control.
func setCueBanner(h win.HWND, text string) {
	ptr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	h.SendMessage(co.EM_SETCUEBANNER, 1, win.LPARAM(unsafe.Pointer(ptr)))
}

// darkenListView recolours a list view, which ignores WM_CTLCOLOR entirely and
// has to be told its colours through its own messages.
//
// The background is the window colour, not the panel colour: the rows are
// owner-drawn (see drawRow) and paint no background of their own, so what is
// behind an unhighlighted row is whatever this erases the control with. The
// list reads as part of the window, with the highlighted row as the only panel
// in it.
func darkenListView(h win.HWND) {
	h.SendMessage(co.LVM_SETBKCOLOR, 0, win.LPARAM(colBg))
	h.SendMessage(co.LVM_SETTEXTBKCOLOR, 0, win.LPARAM(colBg))
	h.SendMessage(co.LVM_SETTEXTCOLOR, 0, win.LPARAM(colText))
}

// roundCorners clips a child control to a rounded rectangle.
//
// Child controls get no help from the desktop window manager -- its corner
// preference rounds top-level windows only -- so the shape has to be imposed by
// clipping. The corners are hard-edged rather than antialiased, which is barely
// visible given how little the panel and window backgrounds differ.
//
// radius is a corner radius, and the doubling below is why it can be. The two
// GDI calls that round a rectangle -- CreateRoundRectRgn here and RoundRect for
// the highlighted row -- both take the *ellipse* the corner is cut from, which
// is twice the radius. Handing them a radius silently gives half the corner
// asked for, and since it is half of something rather than nothing, the result
// looks like a value that did not take effect rather than one applied wrongly.
// Every radius in this file and in palette.go is a real radius; the conversion
// happens once, here.
//
// trimRight is how much of the control's right-hand edge to clip away as well
// as round. It is zero for everything except a scrolling control, where it is
// the width of the native scrollbar that hideNativeVScroll pushed out there --
// the same region does both jobs, so hiding the bar costs no extra call.
func roundCorners(h win.HWND, radius, trimRight int) {
	rc, err := h.GetWindowRect()
	if err != nil {
		return
	}
	w := int(rc.Right-rc.Left) - trimRight
	ht := int(rc.Bottom - rc.Top)
	ellipse := radius * 2
	rgn := winapi.CreateRoundRectRgn(0, 0, w+1, ht+1, ellipse, ellipse)
	if rgn == 0 {
		return
	}
	// SetWindowRgn takes ownership of the region; it must not be deleted here.
	h.SetWindowRgn(win.HRGN(rgn), true)
}
