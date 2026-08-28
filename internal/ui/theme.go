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

// Colours.
//
// A dark surface set close to the Windows 11 palette. Change these and rebuild;
// they are the only place colour is decided.
var (
	colBg      = win.RGB(0x1f, 0x1f, 0x1f) // window background
	colSurface = win.RGB(0x2b, 0x2b, 0x2b) // search box, list, preview
	colText    = win.RGB(0xe8, 0xe8, 0xe8) // primary text
	colBorder  = win.RGB(0x3a, 0x3a, 0x3a) // the window's thin outer border
	colSel     = win.RGB(0x0b, 0x53, 0x94) // highlighted row
	colSelText = win.RGB(0xff, 0xff, 0xff) // text on a highlighted row
)

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
	font    win.HFONT
}

// solidBrush creates a plain filled brush. windigo exposes only the general
// CreateBrushIndirect, so this wraps the LOGBRUSH boilerplate once.
func solidBrush(c win.COLORREF) (win.HBRUSH, error) {
	return win.CreateBrushIndirect(&win.LOGBRUSH{Style: co.BRS_SOLID, Color: c})
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

	return &theme{bg: bg, surface: surface, font: font}, nil
}

func (t *theme) destroy() {
	if t == nil {
		return
	}
	t.bg.DeleteObject()
	t.surface.DeleteObject()
	t.font.DeleteObject()
}

// applyFont sets the theme font on a control.
func (t *theme) applyFont(h win.HWND) {
	h.SendMessage(co.WM_SETFONT, win.WPARAM(t.font), win.LPARAM(1))
}

// applyChrome asks the desktop window manager for modern window decoration:
// rounded corners, dark scrollbars and context menus, and a subtle border.
//
// All three attributes are Windows 11 only, and on Windows 10 they fail
// harmlessly, leaving square corners. The errors are returned rather than
// swallowed so that a missing rounded corner has a stated reason instead of
// being a mystery; the caller logs them and carries on.
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
// dark style. Without it, a black list keeps bright white scrollbars, which is
// the most obviously dated thing left on the window.
func darkenScrollbars(h win.HWND) {
	winapi.SetWindowTheme(uintptr(h), "DarkMode_Explorer")
}

// paintControlBackground handles WM_CTLCOLOR* for a control, colouring its text
// and background and handing back the brush Windows should fill it with.
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
func darkenListView(h win.HWND) {
	h.SendMessage(co.LVM_SETBKCOLOR, 0, win.LPARAM(colSurface))
	h.SendMessage(co.LVM_SETTEXTBKCOLOR, 0, win.LPARAM(colSurface))
	h.SendMessage(co.LVM_SETTEXTCOLOR, 0, win.LPARAM(colText))
}

// roundCorners clips a child control to a rounded rectangle.
//
// Child controls get no help from the desktop window manager -- its corner
// preference rounds top-level windows only -- so the shape has to be imposed by
// clipping. The corners are hard-edged rather than antialiased, which is barely
// visible given how little the panel and window backgrounds differ.
func roundCorners(h win.HWND, radius int) {
	rc, err := h.GetWindowRect()
	if err != nil {
		return
	}
	w := int(rc.Right - rc.Left)
	ht := int(rc.Bottom - rc.Top)
	rgn := winapi.CreateRoundRectRgn(0, 0, w+1, ht+1, radius, radius)
	if rgn == 0 {
		return
	}
	// SetWindowRgn takes ownership of the region; it must not be deleted here.
	h.SetWindowRgn(win.HRGN(rgn), true)
}
