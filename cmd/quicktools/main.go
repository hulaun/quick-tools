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
)

func main() {
	// Windows GUI is single-threaded, and RegisterHotKey delivers WM_HOTKEY to
	// the registering thread. Both need this.
	runtime.LockOSThread()

	cfg, err := config.Load()
	if err != nil {
		// A broken config should not stop the app from starting -- it falls back to
		// defaults and says so, rather than refusing to run.
		fmt.Fprintf(os.Stderr, "config: %v (using defaults)\n", err)
	}

	reg := transform.NewRegistry()
	transform.RegisterBuiltins(reg)

	palette := ui.New(cfg, reg)
	os.Exit(palette.Run())
}
