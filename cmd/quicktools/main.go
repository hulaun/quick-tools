//go:build windows

// Command quicktools is the resident command palette.
//
// It starts once, registers a global hotkey, and then sits idle with its window
// already built but hidden. Pressing the hotkey shows that window, which is why
// it appears instantly rather than paying startup cost per invocation.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/hulaun/quick-tools/internal/config"
	"github.com/hulaun/quick-tools/internal/script"
	"github.com/hulaun/quick-tools/internal/snippet"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/ui"
	"github.com/hulaun/quick-tools/internal/winapi"
)

func main() {
	testScripts := flag.Bool("test-scripts", false,
		"run the .test.json fixtures beside each script, then exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		// A broken config should not stop the app from starting -- it falls back to
		// defaults and says so, rather than refusing to run.
		fmt.Fprintf(os.Stderr, "config: %v (using defaults)\n", err)
	}

	scriptsDir, err := config.ResolveDir(cfg.ScriptsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the scripts folder:", err)
		os.Exit(1)
	}

	if *testScripts {
		os.Exit(runFixtures(scriptsDir))
	}

	snippetsDir, err := config.ResolveDir(cfg.SnippetsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the snippets folder:", err)
		os.Exit(1)
	}

	// Windows GUI is single-threaded, and RegisterHotKey delivers WM_HOTKEY to
	// the registering thread. Both need this.
	runtime.LockOSThread()

	// Before any window is created: ask for real pixels, so Windows does not
	// render at 96 DPI and scale the result up into a blur.
	winapi.SetDPIAware()

	reg := transform.NewRegistry()
	transform.RegisterBuiltins(reg)

	palette, err := ui.New(cfg, reg, script.NewLoader(scriptsDir), snippet.NewStore(snippetsDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not build the palette window:", err)
		os.Exit(1)
	}
	os.Exit(palette.Run())
}

// runFixtures is the red/green loop for writing a script: edit the .js, run
// this, watch the failures turn into passes. It needs no window and no
// clipboard, so it also works from a plain terminal or a watch loop.
func runFixtures(dir string) int {
	fmt.Printf("running fixtures in %s\n\n", dir)
	if failed := script.RunAllFixtures(dir, os.Stdout); failed > 0 {
		return 1
	}
	return 0
}
