//go:build windows

package winapi

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

// ErrNoText is returned when the clipboard holds no Unicode text.
var ErrNoText = errors.New("winapi: clipboard holds no text")

// lockGlobal pins a global memory handle and returns a pointer to its contents.
//
// This is the one place in the package that turns a uintptr into an
// unsafe.Pointer, and `go vet` flags it -- correctly, in general. It is sound
// here for a specific reason: the block came from GlobalAlloc, so it lives
// outside the Go heap and the garbage collector will neither move nor free it.
// GlobalLock additionally pins it until the matching GlobalUnlock. Keeping the
// conversion in a single function means there is exactly one place to audit
// rather than one per call site.
func lockGlobal(h uintptr) (unsafe.Pointer, error) {
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		return nil, err
	}
	return unsafe.Pointer(p), nil //nolint:govet // see doc comment
}

// openClipboard retries, because any other process holding the clipboard makes
// OpenClipboard fail outright. That is routine, not exceptional -- Explorer,
// browsers and editors all grab it briefly. Never assume the first call wins.
func openClipboard() error {
	var lastErr error
	for i := 0; i < 10; i++ {
		r, _, err := procOpenClipboard.Call(0)
		if r != 0 {
			return nil
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	return lastErr
}

// utf16PtrLen walks to the NUL terminator, since Win32 hands back a pointer to
// a string whose length it never states.
func utf16PtrLen(p unsafe.Pointer) int {
	for n := 0; ; n++ {
		if *(*uint16)(unsafe.Add(p, n*2)) == 0 {
			return n
		}
	}
}

// GetClipboardText reads Unicode text off the clipboard.
func GetClipboardText() (string, error) {
	if r, _, _ := procIsFormatAvailable.Call(cfUnicodeText); r == 0 {
		return "", ErrNoText
	}
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()

	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", err
	}
	p, err := lockGlobal(h)
	if err != nil {
		return "", err
	}
	defer procGlobalUnlock.Call(h)

	return syscall.UTF16ToString(unsafe.Slice((*uint16)(p), utf16PtrLen(p))), nil
}

// SetClipboardText replaces the clipboard contents with s.
func SetClipboardText(s string) error {
	utf16, err := syscall.UTF16FromString(s)
	if err != nil {
		return err
	}
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return err
	}

	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(utf16)*2))
	if h == 0 {
		return err
	}
	p, err := lockGlobal(h)
	if err != nil {
		procGlobalFree.Call(h)
		return err
	}
	copy(unsafe.Slice((*uint16)(p), len(utf16)), utf16)
	procGlobalUnlock.Call(h)

	// On success the clipboard takes ownership of the handle, so we must not
	// free it. On failure we still own it and must.
	if r, _, err := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return err
	}
	return nil
}
