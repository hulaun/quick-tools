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
	"github.com/hulaun/quick-tools/internal/macro"
	"github.com/hulaun/quick-tools/internal/place"
	"github.com/hulaun/quick-tools/internal/script"
	"github.com/hulaun/quick-tools/internal/snippet"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/ui"
	"github.com/hulaun/quick-tools/internal/winapi"
)

func main() {
	testScripts := flag.Bool("test-scripts", false,
		"run the .test.json fixtures beside each script, then exit")
	only := flag.String("only", "",
		"with -test-scripts: run only the scripts and cases whose name contains this")
	autostart := flag.String("autostart", "",
		"on|off|status: register this exe to run at login, then exit")
	flag.Parse()

	if *autostart != "" {
		winapi.AttachConsole()
		os.Exit(runAutostart(*autostart))
	}

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
		// A release build is a GUI binary with no console, so output would go
		// nowhere without this.
		winapi.AttachConsole()
		os.Exit(runFixtures(scriptsDir, *only))
	}

	snippetsDir, err := config.ResolveDir(cfg.SnippetsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the snippets folder:", err)
		os.Exit(1)
	}

	placesFile, err := config.ResolvePath(cfg.PlacesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the places file:", err)
		os.Exit(1)
	}

	macrosFile, err := config.ResolvePath(cfg.MacrosFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the macros file:", err)
		os.Exit(1)
	}

	requestsDir, err := config.ResolveDir(cfg.RequestsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the requests folder:", err)
		os.Exit(1)
	}

	envFile, err := config.ResolvePath(cfg.EnvFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot locate the environments file:", err)
		os.Exit(1)
	}

	// Only one copy may run: a second would fail to register the hotkey and sit
	// there useless. Launching it again is nearly always someone trying to open
	// the palette, so hand the request to the running copy and exit quietly.
	instance, alreadyRunning, err := winapi.AcquireSingleInstance()
	if alreadyRunning {
		winapi.SignalExistingInstance()
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not check for a running instance:", err)
		os.Exit(1)
	}
	defer instance.Release()

	// Windows GUI is single-threaded, and RegisterHotKey delivers WM_HOTKEY to
	// the registering thread. Both need this.
	runtime.LockOSThread()

	// Before any window is created: ask for real pixels, so Windows does not
	// render at 96 DPI and scale the result up into a blur.
	winapi.SetDPIAware()

	reg := transform.NewRegistry()
	transform.RegisterBuiltins(reg)

	// The requests tree is walked by the same store as the notes tree: a request
	// is a text file in a folder, which is exactly what that store reads.
	palette, err := ui.New(cfg, reg, script.NewLoader(scriptsDir),
		snippet.NewStore(snippetsDir), place.NewStore(placesFile),
		macro.NewStore(macrosFile), snippet.NewStore(requestsDir), envFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not build the palette window:", err)
		os.Exit(1)
	}
	code := palette.Run()
	instance.Release()
	os.Exit(code)
}

// runAutostart is the command-line half of the tray's "Start with Windows"
// toggle, so a build script can register the app without a human clicking a
// menu. It reports the resulting state either way, because the interesting
// failure is not an error -- it is the entry silently pointing at a copy of the
// exe somewhere else (see winapi.AutostartEnabled).
func runAutostart(mode string) int {
	var err error
	switch mode {
	case "on":
		err = winapi.EnableAutostart()
	case "off":
		err = winapi.DisableAutostart()
	case "status":
	default:
		fmt.Fprintf(os.Stderr, "-autostart: want on, off or status, got %q\n", mode)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "autostart:", err)
		return 1
	}

	exe, _ := os.Executable()
	if winapi.AutostartEnabled() {
		fmt.Printf("autostart: on (%s)\n", exe)
	} else {
		fmt.Printf("autostart: off (this exe is %s)\n", exe)
	}
	return 0
}

// runFixtures is the red/green loop for writing a script: edit the .js, run
// this, watch the failures turn into passes. It needs no window and no
// clipboard, so it also works from a plain terminal or a watch loop.
func runFixtures(dir, only string) int {
	fmt.Printf("running fixtures in %s\n\n", dir)
	if failed := script.RunAllFixtures(dir, os.Stdout, only); failed > 0 {
		return 1
	}
	return 0
}
