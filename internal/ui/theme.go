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

// Colours.
//
// A dark surface set close to the Windows 11 palette. Change these and rebuild;
// they are the only place colour is decided.
//
// Two of them are structural rather than decorative and are commented as such:
// colGlass is black *because* black is what the compositor treats as "let the
// acrylic through" (see applyChrome), and colSurface is whatever it likes only
// so long as it is not black -- an opaque colour is what makes a control solid
// over that acrylic.
var (
	colGlass   = win.RGB(0x00, 0x00, 0x00) // see applyChrome: black == show the acrylic backdrop
	colBg      = colGlass                  // window background, and the list behind the rows
	colSurface = win.RGB(0x1c, 0x1c, 0x1c) // the solid panels: search box, editor, request, response
	colText    = win.RGB(0xe8, 0xe8, 0xe8) // primary text
	colBorder  = colNoBorder               // the window's outer border: none
	colSel     = win.RGB(0x2b, 0x2b, 0x2b) // highlighted row and the active tab
	colSelDim  = win.RGB(0x2b, 0x2b, 0x2b) // highlighted row in the list that is not taking the arrow keys
	colSelText = win.RGB(0xff, 0xff, 0xff) // text on a highlighted row
	colTextDim = win.RGB(0x9a, 0x9a, 0x9a) // the inactive tab
	colScroll  = win.RGB(0x4e, 0x4e, 0x4e) // the overlay scrollbar thumb
	colTextSel = win.RGB(0x3a, 0x3a, 0x3a) // selected text in the search box and the panes
)

// colTextSel is the third structural colour, and the reason it is a grey rather
// than a blue is the caret.
//
// Windows draws a caret by inverting the pixels under it -- there is no colour
// to set -- so the caret is whatever the background it stands on is not. Over
// the panes (#1c1c1c) that comes out near-white, which is what a caret is
// expected to look like; over the system highlight (#0078d4 here) it comes out
// #ff872b, and a caret that turns orange the moment you select something is the
// thing this colour exists to fix. Keep it a grey close to the surface and the
// caret stays the colour it was. A blue selection means an orange caret again,
// and no code anywhere can decide otherwise.
//
// acrylicTint is the colour laid over the blur, and the one number here that is
// not a COLORREF: it is AABBGGRR, alpha first, and the alpha is what decides how
// much of the desktop comes through. Lower it for more of the desktop and less
// of the tint; at zero the window is a clear pane of glass and unreadable.
var acrylicTint uint32 = 0xB0_1A_1A_1A

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
	sel     *winapi.Selection
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
// with a dark ring around it, which over the acrylic backdrop is not a ring but
// a hole.
func solidPen(c win.COLORREF) (win.HPEN, error) {
	return win.CreatePen(co.PS_SOLID, 1, c)
}

// selectionRemap is the pair of blends winapi.MakeOpaque rewrites: what the
// control paints a selection in, and what it should look like instead.
//
// The system colours are read once, here, rather than baked in -- the highlight
// colour is the Windows accent and the user can change it, and reading it is
// what makes the "from" end right whatever they have chosen.
func selectionRemap() *winapi.Selection {
	rgb := func(c win.COLORREF) uint32 {
		return uint32(c.Red())<<16 | uint32(c.Green())<<8 | uint32(c.Blue())
	}
	return &winapi.Selection{
		FromBg: rgb(win.GetSysColor(co.COLOR_HIGHLIGHT)),
		FromFg: rgb(win.GetSysColor(co.COLOR_HIGHLIGHTTEXT)),
		ToBg:   rgb(colTextSel),
		ToFg:   rgb(colSelText),
	}
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
		sel: selectionRemap(),
		bg:  bg, surface: surface, accent: accent,
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
// scrollbars and context menus, a subtle border, and the acrylic behind the
// whole window.
//
// The acrylic is what makes the palette glass, and it is composited
// *underneath* the window's own painting -- so it shows only where the window
// paints nothing. GDI has no alpha channel and leaves that byte at zero, which
// the compositor reads as exactly that: nothing painted. So colGlass is black
// on purpose, and anything meant to stay solid has its alpha put back by
// winapi.MakeOpaque afterwards. Change colGlass to a near-black grey and the
// transparency silently disappears.
//
// The three DWM attributes are Windows 11 only and fail harmlessly on 10,
// leaving square corners. The acrylic is not a DWM attribute at all, for
// reasons measured and written down in winapi.SetAcrylic. Every failure is
// returned rather than swallowed so that a flat, square window has a stated
// reason instead of being a mystery; the caller logs them and carries on.
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
	// The acrylic itself. Not a DWM attribute: see winapi.SetAcrylic for why the
	// two documented routes do not work on a GDI window, and what was measured
	// before settling on this one.
	if !winapi.SetAcrylic(uintptr(h), acrylicTint) {
		errs = append(errs, fmt.Errorf("acrylic: SetWindowCompositionAttribute refused"))
	}
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
// This is the *solid* path: the search box and the editing panes take it, and
// the opaque surface colour is what stops the backdrop showing through them.
// Text is far harder to read over moving glass than over a flat panel, so
// anything holding text you type or read at length is painted, not glazed.
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
// together are what say "this list is showing this mode". An inactive tab is
// left on glass, so the five tabs read as one label lit and four floating.
func (t *theme) paintTab(m ui.Wm, active bool) uintptr {
	hdc := win.HDC(m.WParam)
	if active {
		hdc.SetTextColor(colSelText)
		hdc.SetBkColor(colSel)
		return uintptr(t.accent)
	}
	hdc.SetTextColor(colTextDim)
	hdc.SetBkColor(colGlass)
	return uintptr(t.bg)
}

// paintChainLabel colours the strip beside the tabs. It sits directly on the
// window rather than on a panel, so it takes the window background: a surface
// rectangle floating in the header would read as a control you could click.
func (t *theme) paintChainLabel(m ui.Wm) uintptr {
	hdc := win.HDC(m.WParam)
	hdc.SetTextColor(colTextDim)
	hdc.SetBkColor(colGlass)
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
// The background is the glass colour: the rows are owner-drawn (see drawRow)
// and paint no background of their own, so what is behind an unhighlighted row
// is whatever this erases the control with, and black is the backdrop.
func darkenListView(h win.HWND) {
	h.SendMessage(co.LVM_SETBKCOLOR, 0, win.LPARAM(colGlass))
	h.SendMessage(co.LVM_SETTEXTBKCOLOR, 0, win.LPARAM(colGlass))
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
