//go:build windows

package winapi

import (
	"syscall"
	"unsafe"
)

var procAppendMenuW = user32.NewProc("AppendMenuW")

// Menu item flags.
const (
	MFString    = 0x0000
	MFSeparator = 0x0800
	MFChecked   = 0x0008
	MFUnchecked = 0x0000
)

// AppendMenu adds an item to a menu.
//
// windigo exposes only InsertMenuItem, which needs a fully populated
// MENUITEMINFO for what is nearly always a label and a command id. This wraps
// the older, simpler call instead.
func AppendMenu(hmenu uintptr, flags uint32, cmdID uint16, label string) error {
	var textPtr uintptr
	if flags&MFSeparator == 0 {
		p, err := syscall.UTF16PtrFromString(label)
		if err != nil {
			return err
		}
		textPtr = uintptr(unsafe.Pointer(p))
	}
	r, _, err := procAppendMenuW.Call(hmenu, uintptr(flags), uintptr(cmdID), textPtr)
	if r == 0 {
		return err
	}
	return nil
}
