//go:build windows

package ui

import (
	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/winapi"
)

// What each control does after it has finished painting itself.
//
// There are three such things and none of them can be asked for through a
// style: a static's text has to be redrawn to inset it, the overlay scrollbar
// has to go on top of the content, and the rounded corners have to be painted
// out because clipping them does not work (see maskCorners). All three hang off
// one WM_PAINT hook, in that order.
//
// This file was glass.go until 2026-10-27 and was mostly about transparency.
// The window sat on an acrylic blur, which made the alpha byte of every pixel
// significant -- and GDI never writes that byte, so anything meant to be solid
// had its pixels copied out, the byte set, and copied back, after every single
// paint. That is all gone with the blur; what is left is ordinary painting.
// Gotcha 59 is the account of why it went.

// surface describes what one control needs doing after it paints.
type surface struct {
	ctrl    subclassable
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

// wireSurface hangs a control's after-paint work off one WM_PAINT hook.
//
// The order is not negotiable: the scrollbar goes over the content, and the
// corner mask goes over everything, including the scrollbar's strip where the
// two meet at the bottom of a pane.
//
// Like every subclass this has to be asked for before the control exists, which
// is why it is called from events() rather than from applyLook with the rest of
// the appearance work. It must not capture the handle either -- a control has
// none until WM_CREATE, so a captured one is zero forever.
func (p *Palette) wireSurface(s surface) {
	c := s.ctrl

	c.OnSubclass().Wm(co.WM_PAINT, func(m ui.Wm) uintptr {
		h := c.Hwnd()
		r := h.DefSubclassProc(co.WM_PAINT, m.WParam, m.LParam)

		if s.label != 0 {
			// Over the top of what the control just drew, because a static gives
			// no way to stop it drawing. The text is painted twice, once flush
			// and once inset; the fill in paintLabel is what hides the first.
			p.paintLabel(h, s.label)
		}
		if s.scrolls {
			p.theme.paintScrollbar(h, s.fill())
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

	// The thumb's drag, which is mouse messages rather than painting and so is
	// not part of the hook above.
	if s.scrolls {
		p.wireScrollDrag(c)
	}
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
// So the corners are filled with the window's own background afterwards, and it
// runs last for the same reason anything painted last does: nothing else may
// come over the top of it.
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
