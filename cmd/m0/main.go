//go:build windows

// Command m0 is the de-risk spike for quick-tools.
//
// It has no UI at all. It proves, in one place, every Win32 interaction the
// real app depends on:
//
//	press Ctrl+Alt+Space  ->  read clipboard
//	                      ->  transform it (here: uppercase)
//	                      ->  write it back
//	                      ->  restore focus to where you were
//	                      ->  send Ctrl+V
//
// If that works in Notepad, VS Code, a browser address bar and an RDP session,
// the rest of the project is ordinary application code.
//
// Run with -selftest to exercise just the clipboard round trip, which needs no
// keypress and no foreground window.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hulaun/quick-tools/internal/winapi"
)

const hotkeyID = 1

func main() {
	selftest := flag.Bool("selftest", false, "check the clipboard round trip and exit")
	flag.Parse()

	if *selftest {
		os.Exit(runSelfTest())
	}

	// Must happen before anything registers a hotkey or pumps messages.
	winapi.LockThread()

	fmt.Println("quick-tools M0 spike")
	fmt.Println("  Ctrl+Alt+Space   uppercase the clipboard, then paste it")
	fmt.Println("  Ctrl+C           quit")
	fmt.Println()
	fmt.Println("Try it in Notepad, VS Code, a browser address bar, and over RDP.")
	fmt.Println()

	if err := winapi.HotkeyLoop(hotkeyID, winapi.ModControl|winapi.ModAlt, winapi.VKSpace, onHotkey); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func onHotkey() {
	start := time.Now()

	// Grab the target window first: anything we do afterwards could change which
	// window is in front.
	target := winapi.ForegroundWindow()

	input, err := winapi.GetClipboardText()
	if err != nil {
		fmt.Println("  clipboard:", err)
		return
	}

	output := strings.ToUpper(input)

	if err := winapi.SetClipboardText(output); err != nil {
		fmt.Println("  set clipboard:", err)
		return
	}

	restored := winapi.RestoreForeground(target)
	winapi.SendPaste()

	fmt.Printf("  %q -> %q  (focus restored: %v, %v)\n",
		truncate(input), truncate(output), restored, time.Since(start).Round(time.Millisecond))
}

// runSelfTest checks clipboard read/write without needing a hotkey or a
// foreground window, so it can run unattended. Whatever the user had on the
// clipboard is put back afterwards.
func runSelfTest() int {
	original, origErr := winapi.GetClipboardText()

	cases := []string{
		"hello",
		"multi\r\nline\ttext",
		"unicode: wörld 日本語 emoji",
		strings.Repeat("long ", 5000),
		"",
	}

	failed := 0
	for _, want := range cases {
		if err := winapi.SetClipboardText(want); err != nil {
			fmt.Printf("FAIL  set %q: %v\n", truncate(want), err)
			failed++
			continue
		}
		got, err := winapi.GetClipboardText()
		// An empty string round-trips as "no text available", which is Windows
		// behaviour rather than a defect.
		if want == "" && err == winapi.ErrNoText {
			fmt.Println("ok    empty string (reads back as ErrNoText, as expected)")
			continue
		}
		switch {
		case err != nil:
			fmt.Printf("FAIL  get %q: %v\n", truncate(want), err)
			failed++
		case got != want:
			fmt.Printf("FAIL  round trip: got %q, want %q\n", truncate(got), truncate(want))
			failed++
		default:
			fmt.Printf("ok    %q (%d chars)\n", truncate(want), len(want))
		}
	}

	if origErr == nil {
		if err := winapi.SetClipboardText(original); err != nil {
			fmt.Println("warning: could not restore your original clipboard:", err)
		} else {
			fmt.Println("\nyour original clipboard contents have been restored")
		}
	}

	if failed > 0 {
		fmt.Printf("\n%d check(s) failed\n", failed)
		return 1
	}
	fmt.Println("all clipboard checks passed")
	return 0
}

// truncate shortens a value for the log line. It does not escape anything --
// every caller prints it with %q, which handles that.
func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
