//go:build windows

package ui

import (
	_ "embed"
	"fmt"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// iconICO is the tray icon, generated as a 32x32 32-bit ICO and compiled into
// the binary. Embedding avoids shipping a loose file next to the executable,
// and lets the icon exist before there is any .syso resource (that arrives with
// packaging in M4).
//
//go:embed icon.ico
var iconICO []byte

// Tray message and menu command ids.
const (
	// wmTrayIcon is the private message the shell sends us for mouse activity on
	// the notification icon. Anything from WM_APP up is ours to define.
	wmTrayIcon = co.WM(0x8000 + 1) // WM_APP + 1

	trayIconID = 1

	cmdOpen      uint16 = 100
	cmdAutoPaste uint16 = 101
	cmdAutostart uint16 = 102
	cmdQuit      uint16 = 103
)

// tray owns the notification-area icon.
type tray struct {
	icon  win.HICON
	added bool
}

// icoResourceOffset skips the ICONDIR and single ICONDIRENTRY at the head of a
// one-image .ico file. CreateIconFromResourceEx wants the DIB that follows, not
// the file header.
const icoResourceOffset = 6 + 16

func newTray() (*tray, error) {
	if len(iconICO) <= icoResourceOffset {
		return nil, fmt.Errorf("embedded icon is too small to be a valid .ico")
	}
	// 0x00030000 is the icon format version every current .ico uses.
	icon, err := win.CreateIconFromResourceEx(
		iconICO[icoResourceOffset:], 0x00030000,
		win.SIZE{Cx: int32(win.GetSystemMetrics(co.SM_CXSMICON)), Cy: int32(win.GetSystemMetrics(co.SM_CYSMICON))},
		co.LR_DEFAULTCOLOR)
	if err != nil {
		return nil, fmt.Errorf("building tray icon: %w", err)
	}
	return &tray{icon: icon}, nil
}

// add puts the icon in the notification area.
func (t *tray) add(hwnd win.HWND, tip string) error {
	nid := win.NOTIFYICONDATA{
		HWnd:             hwnd,
		UID:              trayIconID,
		UFlags:           co.NIF_ICON | co.NIF_MESSAGE | co.NIF_TIP,
		UCallbackMessage: wmTrayIcon,
		HIcon:            t.icon,
	}
	nid.SetCbSize()
	nid.SetSzTip(tip)

	if err := win.Shell_NotifyIcon(co.NIM_ADD, &nid); err != nil {
		return err
	}
	t.added = true
	return nil
}

// remove takes the icon away. Skipping this leaves a ghost icon in the tray
// until the user hovers over it, which looks like the app failed to exit.
func (t *tray) remove(hwnd win.HWND) {
	if t.added {
		nid := win.NOTIFYICONDATA{HWnd: hwnd, UID: trayIconID}
		nid.SetCbSize()
		win.Shell_NotifyIcon(co.NIM_DELETE, &nid)
		t.added = false
	}
	if t.icon != 0 {
		t.icon.DestroyIcon()
		t.icon = 0
	}
}

// showMenu pops up the tray context menu at the cursor.
func (p *Palette) showTrayMenu() {
	menu, err := win.CreatePopupMenu()
	if err != nil {
		return
	}
	defer menu.DestroyMenu()

	winapi.AppendMenu(uintptr(menu), winapi.MFString, cmdOpen, "Open palette\t"+p.cfg.Hotkey)
	winapi.AppendMenu(uintptr(menu), winapi.MFSeparator, 0, "")

	autoPasteFlags := uint32(winapi.MFString | winapi.MFUnchecked)
	if p.cfg.AutoPaste {
		autoPasteFlags = winapi.MFString | winapi.MFChecked
	}
	winapi.AppendMenu(uintptr(menu), autoPasteFlags, cmdAutoPaste, "Paste automatically")

	autostartFlags := uint32(winapi.MFString | winapi.MFUnchecked)
	if winapi.AutostartEnabled() {
		autostartFlags = winapi.MFString | winapi.MFChecked
	}
	winapi.AppendMenu(uintptr(menu), autostartFlags, cmdAutostart, "Start with Windows")
	winapi.AppendMenu(uintptr(menu), winapi.MFSeparator, 0, "")
	winapi.AppendMenu(uintptr(menu), winapi.MFString, cmdQuit, "Quit quick-tools")

	pos, err := win.GetCursorPos()
	if err != nil {
		return
	}

	// The window must be foreground first, or the menu will not dismiss when the
	// user clicks elsewhere -- it just hangs around. This is a documented quirk
	// of showing a menu from a tray icon.
	p.wnd.Hwnd().SetForegroundWindow()
	menu.TrackPopupMenu(co.TPM_RIGHTBUTTON, int(pos.X), int(pos.Y), p.wnd.Hwnd())
	p.wnd.Hwnd().PostMessage(co.WM_NULL, 0, 0)
}

// onTrayCommand handles a menu selection.
func (p *Palette) onTrayCommand(cmd uint16) {
	switch cmd {
	case cmdOpen:
		p.show()
	case cmdAutoPaste:
		p.cfg.AutoPaste = !p.cfg.AutoPaste
		// Persist immediately: the toggle exists to be lived with across sessions,
		// and a setting that silently reverts on restart is worse than none.
		if err := config.Save(p.cfg); err != nil {
			p.fatal("Could not save settings", err)
		}
	case cmdAutostart:
		var err error
		if winapi.AutostartEnabled() {
			err = winapi.DisableAutostart()
		} else {
			err = winapi.EnableAutostart()
		}
		if err != nil {
			p.fatal("Could not change the startup setting", err)
		}
	case cmdQuit:
		p.wnd.Hwnd().PostMessage(co.WM_CLOSE, 0, 0)
	}
}

// trayEvents wires the notification icon into the window's message handling.
func (p *Palette) trayEvents() {
	p.wnd.On().Wm(wmTrayIcon, func(m ui.Wm) uintptr {
		switch co.WM(m.LParam.LoWord()) {
		case co.WM_LBUTTONUP:
			p.show()
		case co.WM_RBUTTONUP:
			p.showTrayMenu()
		}
		return 0
	})

	p.wnd.On().WmCommand(cmdOpen, co.CMD_MENU, func() { p.onTrayCommand(cmdOpen) })
	p.wnd.On().WmCommand(cmdAutoPaste, co.CMD_MENU, func() { p.onTrayCommand(cmdAutoPaste) })
	p.wnd.On().WmCommand(cmdAutostart, co.CMD_MENU, func() { p.onTrayCommand(cmdAutostart) })
	p.wnd.On().WmCommand(cmdQuit, co.CMD_MENU, func() { p.onTrayCommand(cmdQuit) })
}
