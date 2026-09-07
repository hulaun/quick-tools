package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// abs builds a genuinely absolute path, taking the volume from the working
// directory so the same code works wherever the tests run.
//
// Two Windows traps live here, and both produce a path that looks absolute and
// is not: filepath.Join("C:", "app") gives "C:app", which is relative to the
// current directory *on drive C*, and a leading separator alone gives
// "\app", which is relative to the current *volume*. Either one would have
// these tests quietly exercising a different case than the one they name --
// which is exactly what happened while writing them.
func abs(segments ...string) string {
	root := string(filepath.Separator)
	if wd, err := os.Getwd(); err == nil {
		root = filepath.VolumeName(wd) + root
	}
	return filepath.Join(append([]string{root}, segments...)...)
}

// TestDefaultsAreUnderStorage pins where a fresh install puts things. The
// grouping is the point: everything the user accumulates is in one folder that
// can be backed up or synced as a unit, and scripts/ is deliberately not in it.
func TestDefaultsAreUnderStorage(t *testing.T) {
	cfg := Default()

	for _, c := range []struct{ name, got, want string }{
		{"snippets", cfg.SnippetsDir, "storage/snippets"},
		{"places", cfg.PlacesFile, "storage/places.json"},
		{"macros", cfg.MacrosFile, "storage/macros.json"},
		{"requests", cfg.RequestsDir, "storage/requests"},
		{"env", cfg.EnvFile, "storage/env.json"},
		{"scripts", cfg.ScriptsDir, "scripts"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

// TestLoadKeepsDefaultsForAbsentKeys is what lets a new setting appear without
// every existing config.json having to be edited: Load unmarshals over
// Default(), so a key the file does not mention keeps its default.
func TestLoadKeepsDefaultsForAbsentKeys(t *testing.T) {
	cfg := Default()
	// The same operation Load performs, on a file written before storage/
	// existed. Anything it does not name must survive.
	if err := json.Unmarshal([]byte(`{"hotkey": "Ctrl+Alt+K"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Hotkey != "Ctrl+Alt+K" {
		t.Errorf("hotkey = %q, want the file's value", cfg.Hotkey)
	}
	if cfg.RequestsDir != "storage/requests" {
		t.Errorf("requestsDir = %q, want the default to survive", cfg.RequestsDir)
	}
	// A boolean default is the case this rule is easiest to get wrong on: Go's
	// zero value for macroUndo is false, and it is only true because Load
	// unmarshals over Default() rather than over an empty struct.
	if !cfg.MacroUndo {
		t.Error("macroUndo = false, want the default of true to survive an older config.json")
	}
}

// TestRootStepsOutOfBin is the rule that lets the exe live in bin/ while
// storage/ sits beside it rather than inside it. Getting this wrong is not a
// crash -- it is an app that silently reads an empty bin/storage/ and looks
// like every note has been lost.
func TestRootStepsOutOfBin(t *testing.T) {
	cases := []struct {
		name   string
		exeDir string
		want   string
	}{
		{"in bin", abs("app", "bin"), abs("app")},
		{"in Bin, whatever the case", abs("app", "Bin"), abs("app")},
		{"beside storage already", abs("app"), abs("app")},
		{"a folder that merely ends in bin", abs("app", "notbin"), abs("app", "notbin")},
		{"only the last segment counts", abs("bin", "app"), abs("bin", "app")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rootFor(c.exeDir); got != c.want {
				t.Errorf("rootFor(%q) = %q, want %q", c.exeDir, got, c.want)
			}
		})
	}
}

// TestResolveDirLeavesAbsolutePathsAlone: a config naming an absolute folder
// means that folder, wherever the exe happens to be.
func TestResolveDirLeavesAbsolutePathsAlone(t *testing.T) {
	elsewhere := abs("elsewhere", "notes")
	got, err := ResolveDir(elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if got != elsewhere {
		t.Errorf("ResolveDir(%q) = %q, want it unchanged", elsewhere, got)
	}
}

// TestResolveDirIsAnchored checks that a relative path does not depend on the
// working directory, which is the whole reason the function exists.
func TestResolveDirIsAnchored(t *testing.T) {
	got, err := ResolveDir("storage/requests")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("ResolveDir = %q, want an absolute path", got)
	}
	if !strings.HasSuffix(filepath.ToSlash(got), "storage/requests") {
		t.Errorf("ResolveDir = %q, want it to end in storage/requests", got)
	}
	if strings.Contains(filepath.ToSlash(got), "/bin/storage") {
		t.Errorf("ResolveDir = %q, want bin/ stepped out of", got)
	}
}
