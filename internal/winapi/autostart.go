//go:build windows

package winapi

import (
	"fmt"
	"os"
	"strings"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/win"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

	// autostartValue is the registry value name. It is also what the user sees
	// listed under Startup apps in Task Manager, so it is a readable name rather
	// than a path or a guid.
	autostartValue = "quick-tools"
)

// autostartCommand is the exact string written to the registry.
//
// The path is quoted because Windows parses an unquoted Run entry at the first
// space, so an executable under "C:\Program Files\..." would otherwise launch
// "C:\Program" with the rest as arguments.
func autostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `"`, nil
}

// AutostartEnabled reports whether the app is registered to run at login, and
// whether that registration points at *this* executable.
//
// The second part matters: after moving or rebuilding the exe elsewhere, the
// old entry still exists and still launches the old copy. Reporting that as
// "enabled" would be misleading.
func AutostartEnabled() bool {
	want, err := autostartCommand()
	if err != nil {
		return false
	}

	hKey, err := win.HKEY_CURRENT_USER.RegOpenKeyEx(runKeyPath, co.REG_OPTION_NONE, co.KEY_READ)
	if err != nil {
		return false
	}
	defer hKey.RegCloseKey()

	val, err := hKey.RegGetValue("", autostartValue, co.RRF_RT_REG_SZ)
	if err != nil {
		return false
	}
	got, ok := val.Sz()
	if !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(got), want)
}

// EnableAutostart registers the app to start at login.
func EnableAutostart() error {
	cmd, err := autostartCommand()
	if err != nil {
		return err
	}

	hKey, _, err := win.HKEY_CURRENT_USER.RegCreateKeyEx(
		runKeyPath, "", co.REG_OPTION_NONE, co.KEY_SET_VALUE, nil)
	if err != nil {
		return fmt.Errorf("opening the Run key: %w", err)
	}
	defer hKey.RegCloseKey()

	if err := hKey.RegSetValueEx(autostartValue, win.RegValSz(cmd)); err != nil {
		return fmt.Errorf("writing the Run entry: %w", err)
	}
	return nil
}

// DisableAutostart removes the registration.
func DisableAutostart() error {
	hKey, err := win.HKEY_CURRENT_USER.RegOpenKeyEx(runKeyPath, co.REG_OPTION_NONE, co.KEY_SET_VALUE)
	if err != nil {
		return err
	}
	defer hKey.RegCloseKey()

	// Deleting a value that is not there is success, not failure -- the desired
	// end state is "not registered" either way.
	if err := hKey.RegDeleteValue(autostartValue); err != nil && AutostartEnabled() {
		return err
	}
	return nil
}
