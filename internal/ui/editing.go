//go:build windows

package ui

import (
	"syscall"
	"unicode"
	"unicode/utf16"
	"unsafe"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

// Word deletion for Edit controls.
//
// A Win32 Edit has no Ctrl+Backspace. The key reaches the control, the control
// does not know what to do with it, and it inserts the DEL character instead --
// which is why the search box grows a small box glyph rather than losing a
// word. Both the search box and the note editor therefore implement it here.

// charClass groups characters so that one press deletes one run of like
// characters, which is what every other editor does: a word, or a run of
// punctuation, plus any whitespace on the way.
const (
	classSpace = iota
	classWord
	classOther
)

func charClass(c uint16) int {
	r := rune(c)
	switch {
	case r == ' ' || r == '\t' || r == '\r' || r == '\n':
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	default:
		return classOther
	}
}

// deleteWord removes the word before the caret (forward == false) or after it
// (forward == true). A non-empty selection is deleted instead, which is what
// the key would do in any other text field.
func deleteWord(h win.HWND, forward bool) {
	start, end := editSelection(h)
	if start != end {
		replaceSelection(h, "")
		return
	}

	text, err := h.GetWindowText()
	if err != nil {
		return
	}
	// EM_GETSEL counts UTF-16 code units, not bytes and not runes, so the scan
	// has to happen in the same units the control is measuring in.
	u := utf16.Encode([]rune(text))
	if start < 0 || start > len(u) {
		return
	}

	if forward {
		i := start
		if i < len(u) {
			c := charClass(u[i])
			for i < len(u) && charClass(u[i]) == c {
				i++
			}
		}
		for i < len(u) && charClass(u[i]) == classSpace {
			i++
		}
		if i == start {
			return
		}
		setSelection(h, start, i)
	} else {
		i := start
		for i > 0 && charClass(u[i-1]) == classSpace {
			i--
		}
		if i > 0 {
			c := charClass(u[i-1])
			for i > 0 && charClass(u[i-1]) == c {
				i--
			}
		}
		if i == start {
			return
		}
		setSelection(h, i, start)
	}

	replaceSelection(h, "")
}

// editSelection reads the caret or selection range.
//
// The two positions are returned through pointers rather than packed into the
// return value: the packed form is two 16-bit words, which is fine for a search
// box and quietly wrong past 65535 characters in a note.
func editSelection(h win.HWND) (int, int) {
	start, end := new(uint32), new(uint32)
	h.SendMessage(co.EM_GETSEL,
		win.WPARAM(unsafe.Pointer(start)), win.LPARAM(unsafe.Pointer(end)))
	return int(*start), int(*end)
}

func setSelection(h win.HWND, start, end int) {
	h.SendMessage(co.EM_SETSEL, win.WPARAM(start), win.LPARAM(end))
}

// replaceSelection overwrites the selection, keeping it undoable with Ctrl+Z.
func replaceSelection(h win.HWND, text string) {
	ptr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	h.SendMessage(co.EM_REPLACESEL, 1, win.LPARAM(unsafe.Pointer(ptr)))
}

// selectAll is Ctrl+A. A multiline Edit handles it on its own; a single-line
// one does not, so the search box needs it spelled out.
func selectAll(h win.HWND) {
	setSelection(h, 0, -1)
}

// ctrlDown and shiftDown report the modifiers at the moment a key arrives.
//
// GetAsyncKeyState rather than GetKeyState because these are read from a
// subclass handling a key that has already been dispatched, where the async
// state is the one that matches what the user is physically holding.
func ctrlDown() bool  { return win.GetAsyncKeyState(co.VK_CONTROL)&0x8000 != 0 }
func shiftDown() bool { return win.GetAsyncKeyState(co.VK_SHIFT)&0x8000 != 0 }

// moveCaretLines moves the caret n lines in a multiline Edit, keeping the
// column where it can.
//
// A multiline Edit does bind Ctrl+Up and Ctrl+Down already -- to scrolling the
// *view* by one line, leaving the caret where it was. That is the wrong half
// of the job in an editable pane: scroll far enough and typing resumes
// somewhere you can no longer see. Moving the caret and letting EM_SCROLLCARET
// bring the view along keeps the two together.
func moveCaretLines(h win.HWND, n int) {
	lines := editSend(h, co.EM_GETLINECOUNT, 0)
	if lines <= 0 {
		return
	}

	// wParam -1 asks for the line holding the caret rather than a given index.
	cur := editSend(h, co.EM_LINEFROMCHAR, ^win.WPARAM(0))
	start := editSend(h, co.EM_LINEINDEX, win.WPARAM(cur))
	caret, _ := editSelection(h)
	col := caret - start

	target := cur + n
	if target < 0 {
		target = 0
	}
	if target >= lines {
		target = lines - 1
	}

	at := editSend(h, co.EM_LINEINDEX, win.WPARAM(target))
	if at < 0 {
		return
	}
	// EM_LINELENGTH takes a character index, not a line number, so the line's
	// own start is what it has to be asked about.
	if l := editSend(h, co.EM_LINELENGTH, win.WPARAM(at)); col > l {
		col = l
	}
	setSelection(h, at+col, at+col)
	h.SendMessage(co.EM_SCROLLCARET, 0, 0)
}

// editSend is SendMessage for the Edit line messages, which all answer with a
// signed count in the return value and never fail in a way worth branching on.
func editSend(h win.HWND, msg co.WM, wp win.WPARAM) int {
	ret, _ := h.SendMessage(msg, wp, 0)
	return int(int32(ret))
}

// editKeys handles the keys both text fields share: word deletion, select all,
// save, and the Alt+D creates. It reports whether the key was consumed.
func (p *Palette) editKeys(h win.HWND, vk co.VK) bool {
	switch vk {
	case co.VK_DOWN:
		if ctrlDown() {
			moveCaretLines(h, jumpRows)
			return true
		}
	case co.VK_UP:
		if ctrlDown() {
			moveCaretLines(h, -jumpRows)
			return true
		}
	case co.VK_BACK:
		if ctrlDown() {
			deleteWord(h, false)
			return true
		}
	case co.VK_DELETE:
		if ctrlDown() {
			deleteWord(h, true)
			return true
		}
	case co.VK('A'):
		if ctrlDown() {
			selectAll(h)
			return true
		}
	case co.VK('S'):
		if ctrlDown() {
			// Ctrl+S means "write what I am looking at", which is a different file
			// in each editable mode.
			if p.mode == modeAPI {
				p.saveRequest()
			} else {
				p.saveNote()
			}
			return true
		}
	}
	return false
}

// editChars swallows the control characters the keys above would otherwise
// leave behind.
//
// An Edit turns Ctrl+Backspace into DEL (0x7f) and Ctrl+S into 0x13 and inserts
// them, or beeps. Handling WM_KEYDOWN alone is not enough: the character
// message is generated separately and arrives afterwards.
func editChars(ch uint16) bool {
	switch ch {
	case 0x7f, 0x13, 0x01: // Ctrl+Backspace, Ctrl+S, Ctrl+A
		return true
	case 0x09: // Tab
		// Tab is a command in every mode -- chain a transform, move into the note,
		// move into the action list -- and never a character. Handling it in
		// WM_KEYDOWN is not enough: TranslateMessage has already queued the tab
		// character by then, so without this it also lands in the field as text.
		return true
	}
	return false
}
