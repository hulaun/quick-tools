//go:build windows

package ui

import (
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/winapi"
)

// The overlay scrollbar.
//
// What VS Code has -- no arrow buttons, a track the same colour as whatever is
// behind it, and a short rounded thumb floating over the content -- is not a
// Windows setting. A control's scrollbar is non-client area drawn by the theme
// engine, and the only choice on offer is which theme class to use, which is
// what darkenScrollbars already does. Anything else is a repaint.
//
// The native bar is not hidden. It cannot usefully be: a list view puts
// WS_VSCROLL back whenever the row count changes, so anything that takes the
// style away is undone by the next refilter. It is pushed out of sight instead.
// The control is made wider than the space it occupies by exactly the width of
// a scrollbar, and the region that already rounds its corners clips that strip
// away (see roundCorners' trimRight). The control still scrolls, still answers
// GetScrollInfo, and simply has its own bar off the edge of the world.
//
// A pleasant side effect: the flash when the scrollbar appeared or disappeared
// -- gotcha 12, the one left standing after WS_EX_COMPOSITED -- cannot happen
// any more, because the transition now takes place outside the visible region
// and the client width the rows are laid out in never changes.
const (
	sbThumbW   = 6  // width of the thumb
	sbThumbPad = 4  // gap between the thumb and the control's visible right edge
	sbMinThumb = 24 // shortest the thumb is allowed to get, however long the list
)

// Scrollbar notification codes, for the WM_VSCROLL a thumb drag synthesises.
// windigo has no constants for these.
const (
	sbThumbTrack = 5
	sbEndScroll  = 8
)

// subclassable is what attachScrollbar needs of a control: a handle, and the
// subclass event set to hang the painting and the drag off. Both ListView and
// Edit satisfy it through windigo's embedded base control.
type subclassable interface {
	Hwnd() win.HWND
	OnSubclass() *ui.WindowEvents
}

// scrollDrag is the state of a thumb being dragged. hwnd doubles as the "a drag
// is in progress" flag, because mouse capture is on exactly one control and a
// zero handle can never be it.
type scrollDrag struct {
	hwnd  win.HWND
	grab  int // where in the thumb the pointer took hold, in pixels from its top
	thumb int // the thumb's height when the drag started
}

// scrollbarInset is the width of the native vertical scrollbar: how far a
// control is widened, and how much of its right edge the region clips away.
func scrollbarInset() int {
	return int(win.GetSystemMetrics(co.SM_CXVSCROLL))
}

// visibleWidth is the width of the part of a scrolling control that is not
// clipped away -- everything laid out inside one has to use this rather than
// the client width.
//
// It is measured from the *window* rectangle, and that is the whole point.
// GetClientRect already excludes a scrollbar when the control has one, so the
// client width of one of these is the intended width when the bar is showing
// and a scrollbar wider when it is not. Deriving this from the client rect
// therefore moved the overlay 26 pixels sideways the moment a list grew long
// enough to scroll -- a bug that only appears with enough rows to need a bar,
// which is exactly when anyone is looking at the scrollbar.
func visibleWidth(h win.HWND) int {
	rc, err := h.GetWindowRect()
	if err != nil {
		return 0
	}
	return int(rc.Right-rc.Left) - scrollbarInset()
}

// contentWidth is the width left for content once the overlay has taken its
// strip: where a row's text and highlight stop, and what an edit's text wraps
// at.
func contentWidth(h win.HWND) int {
	return visibleWidth(h) - ui.DpiX(sbThumbPad*2+sbThumbW)
}

// columnWidth is what a list view's single column is set to, which is not
// contentWidth and deliberately so.
//
// A report-mode list view draws a hairline down the right edge of every column,
// and nothing turns it off: the rows are drawn by hand and skip the default
// entirely, so it is not coming from them -- it is the control's own painting,
// and it showed up as a pale vertical line a few pixels left of the scrollbar.
// Proved by narrowing the column sixty pixels and watching the line move sixty
// pixels with it.
//
// So the column is run out to the full client width, which puts that hairline
// either underneath the overlay strip, which is repainted on top of it, or past
// the region the control is clipped to. The client width and not a pixel more:
// a column wider than the client is what makes a horizontal scrollbar appear.
func columnWidth(h win.HWND) int {
	rc, err := h.GetClientRect()
	if err != nil {
		return contentWidth(h)
	}
	return int(rc.Right)
}

// hideNativeVScroll widens a control by the width of a vertical scrollbar, so
// that the region clipping its corners takes the bar with it.
func hideNativeVScroll(h win.HWND) {
	rc, err := h.GetWindowRect()
	if err != nil {
		return
	}
	h.SetWindowPos(win.HWND(0), win.POINT{},
		win.SIZE{
			Cx: rc.Right - rc.Left + int32(scrollbarInset()),
			Cy: rc.Bottom - rc.Top,
		},
		co.SWP_NOMOVE|co.SWP_NOZORDER|co.SWP_NOACTIVATE)
}

// setEditFormatRect stops a multiline edit wrapping its text underneath the
// clipped strip, and insets it from the frame.
//
// The control is wider than it looks, and without this it would happily lay
// text out in the part nobody can see. The left inset is the other half of the
// job: an edit puts its first character hard against the frame, which a rounded
// corner then cuts into.
func setEditFormatRect(h win.HWND) {
	rc, err := h.GetClientRect()
	if err != nil {
		return
	}
	pad := int32(ui.DpiX(textPad))
	r := win.RECT{Left: pad, Top: pad / 2, Right: int32(contentWidth(h)), Bottom: rc.Bottom}
	h.SendMessage(co.EM_SETRECT, 0, win.LPARAM(unsafe.Pointer(&r)))
}

// setEditMargins insets the text of a single-line edit, which has no formatting
// rectangle to set and takes its padding as margins instead.
func setEditMargins(h win.HWND) {
	pad := uint32(ui.DpiX(textPad))
	// EC_LEFTMARGIN | EC_RIGHTMARGIN, then the two widths packed into the LPARAM.
	h.SendMessage(co.EM_SETMARGINS, win.WPARAM(0x1|0x2), win.LPARAM(pad|(pad<<16)))
}

// thumbRect works out where the thumb goes, in client coordinates. ok is false
// when the content fits and there is nothing to draw.
func thumbRect(h win.HWND) (rc win.RECT, ok bool) {
	var si win.SCROLLINFO
	si.SetCbSize()
	si.Mask = co.SIF_ALL
	if err := h.GetScrollInfo(co.SBB_VERT, &si); err != nil {
		return rc, false
	}
	rng := int(si.Max) - int(si.Min) + 1
	page := int(si.Page)
	if page <= 0 || rng <= page {
		return rc, false
	}

	client, err := h.GetClientRect()
	if err != nil {
		return rc, false
	}
	trackH := int(client.Bottom)
	minH := ui.DpiY(sbMinThumb)
	thumbH := trackH * page / rng
	if thumbH < minH {
		thumbH = minH
	}
	if thumbH > trackH {
		thumbH = trackH
	}

	y := 0
	if span := rng - page; span > 0 {
		y = (trackH - thumbH) * (int(si.Pos) - int(si.Min)) / span
	}
	if y > trackH-thumbH {
		y = trackH - thumbH
	}

	w := ui.DpiX(sbThumbW)
	right := visibleWidth(h) - ui.DpiX(sbThumbPad)
	return win.RECT{
		Left:   int32(right - w),
		Top:    int32(y),
		Right:  int32(right),
		Bottom: int32(y + thumbH),
	}, true
}

// paintScrollbar repaints the column the overlay lives in and, if there is
// anything to scroll, the thumb.
//
// fill is what is behind the strip: the glass background for a list, the panel
// surface for an editor. On a solid panel the thumb ends up opaque along with
// the rest of it, because wireSurface includes the strip in the rectangle whose
// alpha it repaints; on a list it stays glass like the rows it floats over.
func (t *theme) paintScrollbar(h win.HWND, fill win.HBRUSH) {
	hdc, err := h.GetDC()
	if err != nil {
		return
	}

	strip := stripRect(h)
	hdc.FillRect(&win.RECT{
		Left: strip.Left, Top: strip.Top, Right: strip.Right, Bottom: strip.Bottom,
	}, fill)

	rc, ok := thumbRect(h)
	if ok {
		oldBrush, _ := hdc.SelectObjectBrush(t.scroll)
		oldPen, _ := hdc.SelectObjectPen(t.scrPen)
		// The ellipse is the thumb's full width, so its radius is half that and
		// the ends come out as semicircles.
		round := int32(ui.DpiX(sbThumbW))
		hdc.RoundRect(rc, win.SIZE{Cx: round, Cy: round})
		hdc.SelectObjectBrush(oldBrush)
		hdc.SelectObjectPen(oldPen)
	}
	h.ReleaseDC(hdc)
}

// stripRect is the full-height column the overlay lives in, in client
// coordinates.
//
// It is repainted whole on every paint rather than just where the thumb is.
// Scrolling a control moves its pixels with ScrollWindow and invalidates only
// the band that was uncovered, so as far as Windows is concerned the thumb's
// old position is still valid and would be left behind. Erasing the column is
// what removes it, and it costs nothing: contentWidth keeps rows and text out
// of there, so nothing else has anything to draw in it.
func stripRect(h win.HWND) winapi.Rect {
	client, err := h.GetClientRect()
	if err != nil {
		return winapi.Rect{}
	}
	right := visibleWidth(h)
	return winapi.Rect{
		Left:   int32(right - ui.DpiX(sbThumbPad*2+sbThumbW)),
		Top:    0,
		Right:  int32(right),
		Bottom: client.Bottom,
	}
}

// wireScrollDrag makes the thumb draggable. The painting is not here: it hangs
// off the single WM_PAINT hook in wireSurface, which has to sequence it against
// the alpha repaint.
//
// after, which may be nil, runs on every one of these that reaches the control
// -- it is wireSurface's alpha and selection repaint, and it is passed in
// rather than registered alongside because windigo keeps only the last handler
// registered for a message.
func (p *Palette) wireScrollDrag(c subclassable, after func(win.HWND)) {
	c.OnSubclass().Wm(co.WM_LBUTTONDOWN, func(m ui.Wm) uintptr {
		h := c.Hwnd()
		x, y := mousePos(m.LParam)
		rc, ok := thumbRect(h)
		// Outside the strip, or a strip with no thumb in it, is an ordinary
		// click on the content and has to reach the control.
		if !ok || x < int(rc.Left)-ui.DpiX(sbThumbPad) || x > int(rc.Right)+ui.DpiX(sbThumbPad) {
			r := h.DefSubclassProc(co.WM_LBUTTONDOWN, m.WParam, m.LParam)
			if after != nil {
				after(h)
			}
			return r
		}
		grab := y - int(rc.Top)
		if y < int(rc.Top) || y > int(rc.Bottom) {
			// Clicked the track rather than the thumb: jump so the thumb centres
			// on the pointer, then drag from there.
			grab = int(rc.Bottom-rc.Top) / 2
		}
		p.sbDrag = scrollDrag{hwnd: h, grab: grab, thumb: int(rc.Bottom - rc.Top)}
		winapi.SetCapture(uintptr(h))
		p.scrollTo(h, y)
		return 0
	})

	c.OnSubclass().Wm(co.WM_MOUSEMOVE, func(m ui.Wm) uintptr {
		h := c.Hwnd()
		if p.sbDrag.hwnd != h {
			r := h.DefSubclassProc(co.WM_MOUSEMOVE, m.WParam, m.LParam)
			if after != nil {
				after(h)
			}
			return r
		}
		_, y := mousePos(m.LParam)
		p.scrollTo(h, y)
		return 0
	})

	c.OnSubclass().Wm(co.WM_LBUTTONUP, func(m ui.Wm) uintptr {
		h := c.Hwnd()
		if p.sbDrag.hwnd != h {
			r := h.DefSubclassProc(co.WM_LBUTTONUP, m.WParam, m.LParam)
			if after != nil {
				after(h)
			}
			return r
		}
		p.sbDrag = scrollDrag{}
		winapi.ReleaseCapture()
		h.SendMessage(co.WM_VSCROLL, win.WPARAM(sbEndScroll), 0)
		return 0
	})
}

// scrollTo moves the content so that the thumb's top lands where the drag says
// it should, and repaints the strip.
//
// The control is driven with the WM_VSCROLL a real scrollbar would have sent
// rather than with a scroll message of its own, because that is the one message
// a list view and an edit both understand the same way.
func (p *Palette) scrollTo(h win.HWND, mouseY int) {
	var si win.SCROLLINFO
	si.SetCbSize()
	si.Mask = co.SIF_ALL
	if err := h.GetScrollInfo(co.SBB_VERT, &si); err != nil {
		return
	}
	rng := int(si.Max) - int(si.Min) + 1
	page := int(si.Page)
	span := rng - page
	if span <= 0 {
		return
	}
	client, err := h.GetClientRect()
	if err != nil {
		return
	}
	travel := int(client.Bottom) - p.sbDrag.thumb
	if travel <= 0 {
		return
	}

	pos := (mouseY - p.sbDrag.grab) * span / travel
	if pos < 0 {
		pos = 0
	}
	if pos > span {
		pos = span
	}
	pos += int(si.Min)

	h.SendMessage(co.WM_VSCROLL, win.WPARAM(sbThumbTrack|(pos<<16)), 0)
	// The repaint that follows redraws the strip through the WM_PAINT hook, so
	// the thumb comes back at its new position with the right background.
	h.InvalidateRect(nil, false)
}

// mousePos unpacks the client coordinates a mouse message carries. They are
// signed 16-bit halves of the LPARAM: a pointer dragged off the top of the
// control reports a negative y, and reading it as unsigned turns a small
// upward drag into a jump to the bottom of the list.
func mousePos(lp win.LPARAM) (x, y int) {
	return int(int16(uint32(lp) & 0xffff)), int(int16(uint32(lp) >> 16))
}
