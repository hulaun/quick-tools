//go:build windows

package winapi

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// A low-level keyboard hook, for recording a macro.
//
// This is the one thing in the app that sees keystrokes it was not sent.
// WH_KEYBOARD_LL puts our callback in front of every key on the machine, which
// is the only way to record what someone does in another application -- and is
// also why it is installed for exactly as long as it is needed and removed the
// moment it is not. Two costs come with it:
//
//   - Every keystroke on the system waits for us. Windows gives a hook a few
//     hundred milliseconds and silently unhooks one that overruns, so the
//     callback does nothing but append to a slice.
//   - The callback is dispatched by the thread that installed the hook, while
//     that thread pumps messages. That thread is the UI thread, which is what
//     makes the recorder safe to touch without a lock -- and what makes it a
//     rule that Install and Remove are called from there and nowhere else.

const (
	whKeyboardLL = 13

	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105

	llkhfExtended = 0x01
	llkhfInjected = 0x10
	llkhfUp       = 0x80
)

var (
	procSetWindowsHookEx  = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procToUnicodeEx       = user32.NewProc("ToUnicodeEx")
	procGetKeyboardLayout = user32.NewProc("GetKeyboardLayout")
	procGetKeyState       = user32.NewProc("GetKeyState")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

// kbdllhookstruct is the LPARAM a low-level keyboard hook receives.
type kbdllhookstruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

// KeyEvent is one key transition, with the character it would have produced
// already worked out.
type KeyEvent struct {
	VK   uint16
	Down bool

	// Text is what this key would put in a document given the modifiers held,
	// or "" for a key that produces nothing. It is resolved here rather than at
	// replay time because it depends on the keyboard layout of the window being
	// typed into, which is knowable now and not later.
	Text string
}

// KeyHook is an installed keyboard hook.
type KeyHook struct {
	handle uintptr
}

var (
	hookMu sync.Mutex

	// hookFn is the handler for the currently installed hook. Only one hook may
	// be installed at a time, which is enough: the app records or watches for an
	// undo, never both.
	hookFn func(KeyEvent) bool

	// hookHeld is the modifier state as the hook has seen it, which is what
	// ToUnicodeEx has to be told about. The thread's own keyboard state is no
	// use here: a low-level hook runs before the key reaches any thread, so
	// GetKeyboardState answers about the key before this one.
	hookHeld struct{ shift, ctrl, alt, win bool }

	// hookCallback is created once. syscall.NewCallback allocates a trampoline
	// that is never freed and the process has a hard limit on them, so creating
	// one per recording would eventually fail -- after a few thousand macros,
	// which is exactly the kind of bug that only shows up on someone else's
	// machine.
	hookCallback = syscall.NewCallback(hookProc)
)

// InstallKeyHook puts fn in front of every key on the machine until Remove is
// called. Returning true from fn swallows the key, so the application being
// typed into never sees it.
//
// Must be called from the thread running the message loop.
func InstallKeyHook(fn func(KeyEvent) bool) (*KeyHook, error) {
	hookMu.Lock()
	already := hookFn != nil
	hookMu.Unlock()
	if already {
		return nil, fmt.Errorf("winapi: a keyboard hook is already installed")
	}

	mod, _, _ := procGetModuleHandleW.Call(0)
	h, _, err := procSetWindowsHookEx.Call(whKeyboardLL, hookCallback, mod, 0)
	if h == 0 {
		return nil, fmt.Errorf("winapi: SetWindowsHookEx failed: %w", err)
	}

	hookMu.Lock()
	hookFn = fn
	hookHeld = struct{ shift, ctrl, alt, win bool }{}
	hookMu.Unlock()

	return &KeyHook{handle: h}, nil
}

// Remove takes the hook out. Calling it twice is harmless, which matters: the
// recording can end because the stop key was pressed, because it timed out, or
// because the app is shutting down, and those can race.
func (h *KeyHook) Remove() {
	if h == nil || h.handle == 0 {
		return
	}
	procUnhookWindowsHook.Call(h.handle)
	h.handle = 0

	hookMu.Lock()
	hookFn = nil
	hookMu.Unlock()
}

// hookProc is the Win32 callback. It is deliberately thin: work out what the
// key was, hand it to the handler, and get out of the way.
//
// The third parameter is declared as the struct pointer it actually is rather
// than as a uintptr. syscall.NewCallback accepts a pointer-typed argument, and
// taking it that way keeps the one uintptr-to-pointer conversion in this
// package where it already is -- in clipboard.go, over memory that came from
// GlobalAlloc and has a written reason to be there.
func hookProc(nCode uintptr, wParam uintptr, info *kbdllhookstruct) uintptr {
	lParam := uintptr(unsafe.Pointer(info))

	if int32(nCode) < 0 || info == nil {
		return callNextHook(nCode, wParam, lParam)
	}

	// Ignore anything we synthesised ourselves. Without this, replaying a macro
	// while a hook is installed feeds the replay straight back into the recorder
	// -- and the undo watcher would see its own Ctrl+Z presses and swallow them.
	if info.flags&llkhfInjected != 0 {
		return callNextHook(nCode, wParam, lParam)
	}

	var down bool
	switch wParam {
	case wmKeyDown, wmSysKeyDown:
		down = true
	case wmKeyUp, wmSysKeyUp:
		down = false
	default:
		return callNextHook(nCode, wParam, lParam)
	}

	vk := uint16(info.vkCode)

	hookMu.Lock()
	fn := hookFn
	trackModifier(vk, down)
	text := ""
	if down && fn != nil {
		text = keyText(vk, info.scanCode)
	}
	hookMu.Unlock()

	if fn == nil {
		return callNextHook(nCode, wParam, lParam)
	}
	if fn(KeyEvent{VK: vk, Down: down, Text: text}) {
		return 1 // swallowed: the key goes no further
	}
	return callNextHook(nCode, wParam, lParam)
}

func callNextHook(nCode, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// trackModifier keeps the hook's own picture of what is held. Called with
// hookMu already taken.
func trackModifier(vk uint16, down bool) {
	switch vk {
	case VKShift, 0xa0, 0xa1:
		hookHeld.shift = down
	case VKControl, 0xa2, 0xa3:
		hookHeld.ctrl = down
	case VKMenu, 0xa4, 0xa5:
		hookHeld.alt = down
	case VKLWin, VKRWin:
		hookHeld.win = down
	}
}

const vkCapital = 0x14

// keyText asks Windows what character this key would produce, under the
// modifiers currently held and the keyboard layout of the window being typed
// into.
//
// Doing it here, once, at record time is what makes a macro layout-independent:
// the recording stores the character that came out, and replaying it puts that
// same character in whatever the layout is later. Storing the key instead would
// mean a macro recorded on a US layout typing something else entirely on a
// Vietnamese one.
//
// Called with hookMu already taken.
func keyText(vk uint16, scan uint32) string {
	var state [256]byte
	if hookHeld.shift {
		state[VKShift], state[0xa0] = 0x80, 0x80
	}
	if hookHeld.ctrl {
		state[VKControl], state[0xa2] = 0x80, 0x80
	}
	if hookHeld.alt {
		state[VKMenu], state[0xa4] = 0x80, 0x80
	}
	// Caps Lock is a toggle, so it is the low bit rather than the high one, and
	// it has to be asked for rather than tracked -- it may well have been on
	// before the hook was installed.
	if caps, _, _ := procGetKeyState.Call(vkCapital); caps&1 != 0 {
		state[vkCapital] = 1
	}

	var buf [8]uint16
	// Flag bit 2 tells ToUnicodeEx not to disturb the kernel's keyboard state.
	// Without it, asking about a dead key here consumes it, and the character
	// the user was actually composing comes out wrong in the application they
	// are typing into -- a translation call with a side effect on someone else's
	// window.
	const dontChangeState = 0x04
	n, _, _ := procToUnicodeEx.Call(
		uintptr(vk), uintptr(scan),
		uintptr(unsafe.Pointer(&state[0])),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		dontChangeState, foregroundLayout(),
	)

	// Zero is "this key produces nothing", and -1 is a dead key part way through
	// composing one. Both are recorded as a key rather than as text; a dead key
	// sequence is the one thing this cannot capture faithfully, and half of one
	// is worse than none.
	if int32(n) <= 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// foregroundLayout is the keyboard layout of the window being typed into,
// which is not necessarily ours: layouts are per-thread on Windows.
func foregroundLayout() uintptr {
	hwnd, _, _ := procGetForegroundWin.Call()
	var thread uintptr
	if hwnd != 0 {
		thread, _, _ = procGetWindowThreadPI.Call(hwnd, 0)
	}
	layout, _, _ := procGetKeyboardLayout.Call(thread)
	return layout
}
