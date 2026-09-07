//go:build windows

package ui

import (
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/winapi"
)

// Which parts of the window are glass, and which are not.
//
// The whole window sits on an acrylic backdrop (see applyChrome), and what
// shows through is decided per pixel by a byte GDI never writes -- so by
// default *everything* painted here is translucent. That is right for the
// window background and for the two lists, and wrong for the search box and the
// panes text is read and typed in, where a moving blur behind the words is
// tiring to look at for as long as those are looked at.
//
// So the solid controls repaint their own alpha afterwards, through
// winapi.MakeOpaque. It has to run after the control has drawn, not instead of
// it: the control drawing its own text is precisely what puts the alpha back to
// zero. Everything glass simply does nothing and stays transparent.
//
// One consequence worth knowing: nothing can be made *partly* transparent this
// way. A pixel is either the acrylic material or the colour painted over it.
// "Slightly transparent" is the material's own tint doing the work, not an
// alpha value we choose.

// surface describes how one control relates to the backdrop.
type surface struct {
	ctrl    subclassable
	solid   bool  // repaint the alpha, so the backdrop stops at its edges
	scrolls bool  // give it the overlay scrollbar
	label   co.DT // draw its text ourselves, with this format; zero to leave it alone
	radius  int   // round its corners by painting them out; zero to leave them square
	fill    func() win.HBRUSH
}

// paintLabel draws a static's text with the same left inset the panes have.
//
// A static has no equivalent of an edit's margins or formatting rectangle: it
// puts its text against the frame and there is no message that says otherwise.
// Since these two are panels in their own right -- rounded, filled, sitting in
// the right-hand column beside panes that are inset -- the only way to line
// their text up with the rest was to draw it.
func (p *Palette) paintLabel(h win.HWND, format co.DT) {
	hdc, err := h.GetDC()
	if err != nil {
		return
	}
	defer h.ReleaseDC(hdc)

	rc, err := h.GetClientRect()
	if err != nil {
		return
	}
	hdc.FillRect(&rc, p.theme.surface)

	text, err := h.GetWindowText()
	if err != nil || text == "" {
		return
	}
	old, _ := hdc.SelectObjectFont(p.theme.font)
	hdc.SetBkMode(co.BKMODE_TRANSPARENT)
	hdc.SetTextColor(colTextDim)
	pad := int32(ui.DpiX(textPad))
	box := win.RECT{Left: pad, Top: rc.Top, Right: rc.Right - pad, Bottom: rc.Bottom}
	hdc.DrawText(text, &box, format)
	hdc.SelectObjectFont(old)
}

// wireSurface hangs everything that happens after a control paints off one
// WM_PAINT hook: the overlay scrollbar, then the alpha repaint.
//
// The order is not negotiable. The scrollbar is GDI like everything else, so
// painting it after the alpha repaint would punch a translucent strip through a
// solid panel; and the strip has to be included in the rectangle the repaint
// covers, since it is usually outside the update region that caused the paint.
//
// Like every subclass this has to be asked for before the control exists, which
// is why it is called from events() rather than from applyLook with the rest of
// the appearance work. It must not capture the handle either -- a control has
// none until WM_CREATE, so a captured one is zero forever.
func (p *Palette) wireSurface(s surface) {
	c := s.ctrl

	c.OnSubclass().Wm(co.WM_PAINT, func(m ui.Wm) uintptr {
		h := c.Hwnd()
		// Asked for before the paint, which validates it.
		dirty, ok := winapi.GetUpdateRect(uintptr(h))

		r := h.DefSubclassProc(co.WM_PAINT, m.WParam, m.LParam)

		if s.label != 0 {
			// Over the top of what the control just drew, because a static gives
			// no way to stop it drawing. The text is painted twice, once flush
			// and once inset; the fill in paintLabel is what hides the first.
			p.paintLabel(h, s.label)
			dirty, ok = winapi.Rect{}, false
		}
		if s.scrolls {
			p.theme.paintScrollbar(h, s.fill())
			dirty = union(dirty, ok, stripRect(h))
			ok = true
		}
		if s.solid {
			if !ok {
				// No update rectangle to work from, or the whole control was
				// repainted from scratch: cover all of it.
				cr, cerr := h.GetClientRect()
				if cerr != nil {
					return r
				}
				dirty = winapi.Rect{Right: cr.Right, Bottom: cr.Bottom}
			}
			winapi.MakeOpaque(uintptr(h), dirty, p.theme.sel)
		}
		if s.radius > 0 {
			w := 0
			if s.scrolls {
				w = visibleWidth(h)
			} else if cr, cerr := h.GetClientRect(); cerr == nil {
				w = int(cr.Right)
			}
			p.maskCorners(h, ui.DpiX(s.radius), w)
		}
		return r
	})

	// The mouse messages, for the two things a paint hook cannot see. A control
	// dragging out a selection repaints as the pointer moves without always
	// going through a paint message, and what it draws that way is neither
	// solid nor recoloured: the line flashes the backdrop through, for as long
	// as it takes the next paint to arrive. Re-asserting both afterwards is
	// what stops it, and it is cheap because refreshSurface does nothing at all
	// unless something is selected.
	//
	// The scrolling controls take the same messages for the overlay scrollbar,
	// and windigo keeps only the last handler registered for a message -- so
	// those three are handed to wireScrollDrag to call rather than registered
	// twice, and only the double-click is left to register here.
	var after func(win.HWND)
	if s.solid {
		after = p.refreshSurface
	}
	if s.scrolls {
		p.wireScrollDrag(c, after)
	} else if after != nil {
		p.wireSelectRefresh(c, after, co.WM_LBUTTONDOWN, co.WM_MOUSEMOVE, co.WM_LBUTTONUP)
	}
	if after != nil {
		// WM_KEYUP covers selecting with the keyboard, and is free: nothing else
		// in the window wants it, and the arrow keys that do the selecting are
		// handled on the way down.
		p.wireSelectRefresh(c, after, co.WM_LBUTTONDBLCLK, co.WM_KEYUP)
	}
}

// refreshSurface re-asserts what wireSurface's paint hook does -- the alpha that
// keeps a control solid, and the selection colours -- outside a paint message.
//
// It covers the whole client area rather than a rectangle worked out from the
// selection: the point of it is the painting we did not see, so there is no
// update rectangle to go on. The guard is the selection itself. With nothing
// selected there is nothing a control redraws behind our back, so the common
// case -- every keystroke, every mouse move over a pane -- costs one message.
func (p *Palette) refreshSurface(h win.HWND) {
	var start, end uint32
	h.SendMessage(co.EM_GETSEL,
		win.WPARAM(unsafe.Pointer(&start)), win.LPARAM(unsafe.Pointer(&end)))
	if start == end {
		return
	}
	rc, err := h.GetClientRect()
	if err != nil {
		return
	}
	winapi.MakeOpaque(uintptr(h), winapi.Rect{Right: rc.Right, Bottom: rc.Bottom}, p.theme.sel)
}

// wireSelectRefresh hangs refreshSurface off messages the control handles for
// itself, after it has handled them.
func (p *Palette) wireSelectRefresh(c subclassable, after func(win.HWND), msgs ...co.WM) {
	for _, msg := range msgs {
		id := msg
		c.OnSubclass().Wm(id, func(m ui.Wm) uintptr {
			h := c.Hwnd()
			r := h.DefSubclassProc(id, m.WParam, m.LParam)
			after(h)
			return r
		})
	}
}

// union grows a rectangle to cover another. valid says whether the first one
// means anything yet -- a paint with no update rectangle still has to cover the
// scrollbar strip.
func union(a winapi.Rect, valid bool, b winapi.Rect) winapi.Rect {
	if !valid {
		return b
	}
	if b.Left < a.Left {
		a.Left = b.Left
	}
	if b.Top < a.Top {
		a.Top = b.Top
	}
	if b.Right > a.Right {
		a.Right = b.Right
	}
	if b.Bottom > a.Bottom {
		a.Bottom = b.Bottom
	}
	return a
}

// maskCorners rounds a control's corners by painting them out, rather than by
// clipping the control to a rounded region.
//
// A region is the obvious way and it does not work here. `SetWindowRgn`
// installs it, `PtInRegion` confirms the shape, and it reliably hides the
// native scrollbar pushed off the right-hand edge -- but the corners come back
// square anyway, and no amount of forcing a repaint changes it. What does work
// is painting, which is how the highlighted row has been rounded all along: the
// one rounded thing in this window that was never in doubt is the one nothing
// was asked to clip.
//
// So the corners are filled with the window's own background afterwards. It has
// to run last, after any alpha repaint, because these pixels are meant to stay
// transparent and show the acrylic like the window around them -- MakeOpaque
// would otherwise have just turned them solid.
func (p *Palette) maskCorners(h win.HWND, radius, width int) {
	rc, err := h.GetClientRect()
	if err != nil || width <= 0 {
		return
	}

	full, err := win.CreateRectRgnIndirect(win.RECT{Right: int32(width), Bottom: rc.Bottom})
	if err != nil {
		return
	}
	defer full.DeleteObject()

	ellipse := radius * 2
	round := win.HRGN(winapi.CreateRoundRectRgn(0, 0, width, int(rc.Bottom), ellipse, ellipse))
	if round == 0 {
		return
	}
	defer round.DeleteObject()

	// The corners are what is left of the rectangle once the rounded shape is
	// taken out of it: four triangles, and nothing else to repaint.
	if _, err := full.CombineRgn(full, round, co.RGN_DIFF); err != nil {
		return
	}

	hdc, err := h.GetDC()
	if err != nil {
		return
	}
	defer h.ReleaseDC(hdc)
	hdc.FillRgn(full, p.theme.bg)
}
