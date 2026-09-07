//go:build windows

package winapi

import (
	"unsafe"
)

var (
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procBitBlt             = gdi32.NewProc("BitBlt")

	procGetUpdateRect = user32.NewProc("GetUpdateRect")
	procHideCaret     = user32.NewProc("HideCaret")
	procShowCaret     = user32.NewProc("ShowCaret")
	procGetDC         = user32.NewProc("GetDC")
	procReleaseDC     = user32.NewProc("ReleaseDC")
)

// Rect is a Win32 RECT. Declared here rather than borrowed from windigo so that
// this file stays a plain syscall layer like the rest of the package.
type Rect struct {
	Left, Top, Right, Bottom int32
}

// bitmapInfoHeader is BITMAPINFOHEADER. A negative height asks for a top-down
// bitmap, so row 0 is the top one and the pointer arithmetic below is the
// obvious way round.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

const (
	diRgbColors = 0
	srcCopy     = 0x00CC0020
)

// GetUpdateRect is the part of a window that is about to be repainted, in
// client coordinates. It must be called before the paint, which validates it.
func GetUpdateRect(hwnd uintptr) (Rect, bool) {
	var rc Rect
	r, _, _ := procGetUpdateRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)), 0)
	return rc, r != 0
}

// Selection says how a text selection was painted and how it should look
// instead. Zero it (or pass nil) to leave the colours alone and only set the
// alpha.
//
// A plain edit control paints its selection in the system highlight colour and
// there is no message that says otherwise -- the colours a window hands back
// from WM_CTLCOLOREDIT are the unselected ones. So the selection is recoloured
// after the fact, in the pass that is already walking every pixel to set the
// alpha, by mapping the blend the control painted onto the blend we want.
//
// Bg and Fg are the two ends of that blend: every pixel inside a selection is
// some mixture of the two, one mixture per channel because the text is
// antialiased with ClearType. Undoing that per channel is what keeps the glyph
// edges clean -- a whole-pixel test would leave a blue fringe around every
// letter, since ClearType puts a different amount of each channel down.
type Selection struct {
	FromBg, FromFg uint32 // what the control painted, 0xRRGGBB
	ToBg, ToFg     uint32 // what it should have painted
}

// channels splits 0xRRGGBB into the order the pixels are stored in: blue,
// green, red.
func channels(c uint32) [3]int32 {
	return [3]int32{int32(c & 0xff), int32((c >> 8) & 0xff), int32((c >> 16) & 0xff)}
}

// remapRow rewrites one scanline's selection pixels.
//
// The span is found rather than calculated. Asking the control where the
// selection is means walking its lines and its wrapping and turning character
// offsets into pixels; the pixels themselves already say, because the
// background of a selection is the highlight colour exactly and nothing else in
// this window is that colour. So the run between the first and last exact match
// is the selection, and it is grown outwards over the antialiased edge of the
// glyph at each end -- those are blends, not exact matches, and leaving them
// behind is what would leave two blue slivers at the ends of every selection.
func remapRow(row []uint32, from, to [2][3]int32) {
	bg := uint32(from[0][2])<<16 | uint32(from[0][1])<<8 | uint32(from[0][0])

	first, last := -1, -1
	for x, px := range row {
		if px&0x00ff_ffff == bg {
			if first < 0 {
				first = x
			}
			last = x
		}
	}
	if first < 0 {
		return
	}
	// bluer reports a pixel that could be part of the blend but is not the plain
	// background: the two ends of the run, where a glyph is half over the edge.
	// Every other colour in this window is a grey, and a grey has no cast.
	bluer := func(px uint32) bool {
		b, r := int32(px&0xff), int32((px>>16)&0xff)
		return (b-r)*(from[0][0]-from[0][2]) > 0
	}
	for first > 0 && bluer(row[first-1]) {
		first--
	}
	for last < len(row)-1 && bluer(row[last+1]) {
		last++
	}

	for x := first; x <= last; x++ {
		px := row[x]
		var out uint32
		ok := true
		for ch := 0; ch < 3 && ok; ch++ {
			v := int32((px >> (8 * ch)) & 0xff)
			span := from[1][ch] - from[0][ch]
			if span == 0 {
				out |= uint32(to[0][ch]) << (8 * ch)
				continue
			}
			t := (v - from[0][ch]) * 256 / span
			// Not on the blend at all, by more than rounding. Nothing the
			// control painted is here, so leave the pixel exactly as it is
			// rather than invent a colour for it.
			if t < -16 || t > 272 {
				ok = false
				break
			}
			if t < 0 {
				t = 0
			} else if t > 256 {
				t = 256
			}
			out |= uint32(to[0][ch]+(to[1][ch]-to[0][ch])*t/256) << (8 * ch)
		}
		if ok {
			row[x] = out | 0xff00_0000
		}
	}
}

// MakeOpaque marks a rectangle of a window's client area as solid, so that the
// desktop window manager stops showing the backdrop through it.
//
// The window's frame is extended over its whole client area (see the ui
// package's applyChrome), which is what puts the acrylic material behind
// everything -- and what makes the alpha byte of every pixel significant. GDI
// does not have an alpha channel: every colour it paints leaves that byte zero,
// which the compositor reads as "nothing was painted here". So GDI painting on
// this window comes out translucent whatever colour it is, and there is no
// brush, mode or flag that changes it.
//
// What does change it is copying a 32-bit bitmap, because that copy is bytewise
// and carries whatever alpha it holds. This copies the finished pixels out into
// one, sets the alpha byte across the whole thing, and copies them back. The
// colours are untouched; only the fourth byte moves. That is why it runs
// *after* the control has painted and not instead of it -- a control drawing its
// own text is exactly the thing that would put the alpha back to zero.
//
// Nothing here is cached between calls. A DIB the size of the update rectangle
// is a few hundred kilobytes and lives for the length of one paint; keeping one
// alive per control, for five controls that are hidden most of the time, would
// trade that for a permanent cost.
func MakeOpaque(hwnd uintptr, rc Rect, sel *Selection) {
	w, h := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	if w <= 0 || h <= 0 {
		return
	}

	// The caret is drawn by inverting the pixels under it, so it has to be off
	// the screen while they are copied out and put back. Leave it there and the
	// copy takes a picture of one blink of it: the pixels underneath are then
	// wrong, and -- worse, once the colours below are being rewritten -- the next
	// blink inverts a colour the caret was never drawn over and leaves a stray
	// mark behind. Both calls are no-ops unless this window owns the caret.
	procHideCaret.Call(hwnd)
	defer procShowCaret.Call(hwnd)

	hdc, _, _ := procGetDC.Call(hwnd)
	if hdc == 0 {
		return
	}
	defer procReleaseDC.Call(hwnd, hdc)

	mem, _, _ := procCreateCompatibleDC.Call(hdc)
	if mem == 0 {
		return
	}
	defer procDeleteDC.Call(mem)

	bi := bitmapInfoHeader{
		Size:     uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:    int32(w),
		Height:   int32(-h),
		Planes:   1,
		BitCount: 32,
	}
	var bits unsafe.Pointer
	bmp, _, _ := procCreateDIBSection.Call(mem,
		uintptr(unsafe.Pointer(&bi)), diRgbColors,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(mem, bmp)
	defer procSelectObject.Call(mem, old)

	procBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h),
		hdc, uintptr(rc.Left), uintptr(rc.Top), srcCopy)

	// The bits came from CreateDIBSection, so they live outside the Go heap and
	// the collector cannot move them -- the same reasoning that makes the
	// clipboard's one unsafe.Pointer conversion sound.
	px := unsafe.Slice((*uint32)(bits), w*h)
	for i := range px {
		px[i] |= 0xff00_0000
	}
	if sel != nil && sel.FromBg != sel.FromFg {
		from := [2][3]int32{channels(sel.FromBg), channels(sel.FromFg)}
		to := [2][3]int32{channels(sel.ToBg), channels(sel.ToFg)}
		for y := 0; y < h; y++ {
			remapRow(px[y*w:(y+1)*w], from, to)
		}
	}

	procBitBlt.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(w), uintptr(h),
		mem, 0, 0, srcCopy)
}
