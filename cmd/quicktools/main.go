//go:build windows

// Command quicktools is the resident command palette.
//
// It starts once, registers a global hotkey, and then sits idle with its window
// already built but hidden. Pressing the hotkey shows that window, which is why
// it appears instantly rather than paying startup cost per invocation.
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/ui"
	"github.com/hulaun/quick-tools/internal/winapi"
)

func main() {
	// Windows GUI is single-threaded, and RegisterHotKey delivers WM_HOTKEY to
	// the registering thread. Both need this.
	runtime.LockOSThread()

	// Before any window is created: ask for real pixels, so Windows does not
	// render at 96 DPI and scale the result up into a blur.
	winapi.SetDPIAware()

	cfg, err := config.Load()
	if err != nil {
		// A broken config should not stop the app from starting -- it falls back to
		// defaults and says so, rather than refusing to run.
		fmt.Fprintf(os.Stderr, "config: %v (using defaults)\n", err)
	}

	reg := transform.NewRegistry()
	transform.RegisterBuiltins(reg)

	palette, err := ui.New(cfg, reg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not build the palette window:", err)
		os.Exit(1)
	}
	os.Exit(palette.Run())
}
