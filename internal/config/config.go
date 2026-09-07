// Package config loads and saves the user's settings.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config is the on-disk settings file.
type Config struct {
	// Hotkey is the combination that opens the palette, e.g. "Ctrl+Alt+Space".
	Hotkey string `json:"hotkey"`

	// AutoPaste sends Ctrl+V to the previously focused window after a transform
	// runs. It defaults to off: Enter puts the result on the clipboard and stops
	// there, leaving the user to paste when and where they want. Turn it on in
	// config.json to have the paste happen automatically.
	AutoPaste bool `json:"autoPaste"`

	// Every path below is relative to Root by default -- the folder holding the
	// exe, or its parent when the exe is in bin/ -- so the whole thing stays
	// portable: copy the folder and your transforms and notes come with it.
	//
	// Everything the user accumulates lives under storage/; scripts/ does not,
	// because it is half checked-in (the stubs, their fixtures and the README)
	// and half yours, which makes it source rather than data.
	ScriptsDir  string `json:"scriptsDir"`
	SnippetsDir string `json:"snippetsDir"`

	// PlacesFile is the list of named locations behind the Places tab. It is one
	// file rather than a folder of them, unlike snippets: a place is a single
	// line, and a file each would be more filing than the thing being filed.
	PlacesFile string `json:"placesFile"`

	// MacrosFile is the recorded keystroke sequences behind the Macros tab. One
	// file rather than a folder of them, for the same reason as places: a macro
	// is a short list of steps, and a file each would be more filing than the
	// thing being filed.
	MacrosFile string `json:"macrosFile"`

	// MacroUndo lets the palette turn the first Ctrl+Z after a multi-change
	// macro into as many presses as that macro needs, so one press puts the line
	// back. It is on by default and only ever does anything in the twenty seconds
	// after a replay; set it false to have Ctrl+Z always mean exactly one undo.
	MacroUndo bool `json:"macroUndo"`

	// RequestsDir is the folder of .http files behind the API tab, and EnvFile
	// is the environments they take their {{vars}} from. A folder for one and a
	// single file for the other, for the same reason places is one file: a
	// request is a document worth its own file, an environment is a handful of
	// key-value pairs.
	RequestsDir string `json:"requestsDir"`
	EnvFile     string `json:"envFile"`

	// Openers maps an opener id -- "code", "notepad++", "terminal" -- to the
	// executable that serves it. Anything set here wins over what was detected,
	// and an empty value removes that opener from the menu. Left unset, all
	// three are found automatically where they are installed.
	Openers map[string]string `json:"openers,omitempty"`
}

// Default returns the settings used when no file exists yet.
func Default() Config {
	return Config{
		Hotkey:      "Ctrl+Alt+Q",
		AutoPaste:   false,
		ScriptsDir:  "scripts",
		SnippetsDir: "storage/snippets",
		PlacesFile:  "storage/places.json",
		MacrosFile:  "storage/macros.json",
		MacroUndo:   true,
		RequestsDir: "storage/requests",
		EnvFile:     "storage/env.json",
	}
}

// Dir is where the config file lives: %APPDATA%\quick-tools.
func Dir() (string, error) {
	appData, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appData, "quick-tools"), nil
}

// Path is the full path to config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config, falling back to defaults.
//
// A missing file is not an error -- it is a first run. A malformed file is
// reported, because silently reverting to defaults would look like the user's
// settings were thrown away at random.
func Load() (Config, error) {
	cfg := Default()
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	return cfg, nil
}

// Save writes the config, creating the directory if needed.
func Save(cfg Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// binDirName is the folder the executable lives in, and the one place the
// anchoring rule below has to know about by name.
const binDirName = "bin"

// Root is the folder that relative config paths are measured from: the
// executable's own folder, except that a "bin" is stepped out of.
//
// Anchoring to the exe rather than the working directory is the important part
// -- the working directory is wherever Explorer or the Run key happened to
// launch us from, and would make the app read a different storage folder on
// Tuesday than it did on Monday.
//
// Stepping out of bin/ is what lets the binary live in bin/ while storage/,
// scripts/ and the rest sit beside it rather than inside it:
//
//	quick-tools/
//	  bin/quicktools.exe
//	  storage/
//	  scripts/
//
// That is the layout in development and the layout a release ships in, which is
// the whole point: a dev build and a release build read the same files. The
// rule is one folder name rather than a search, so it is predictable -- there
// is no walking up the tree hoping to find something that looks right.
func Root() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return rootFor(filepath.Dir(exe)), nil
}

// rootFor is Root's rule on its own, so it can be tested without pretending to
// be an executable somewhere.
func rootFor(exeDir string) string {
	if strings.EqualFold(filepath.Base(exeDir), binDirName) {
		return filepath.Dir(exeDir)
	}
	return exeDir
}

// ResolveDir turns a possibly-relative directory from the config into an
// absolute path, anchored at Root.
func ResolveDir(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, dir), nil
}

// ResolvePath is ResolveDir for a file. Same rule, and the same reason: a
// relative path means under Root, not beside wherever Explorer happened to
// launch the app from.
func ResolvePath(file string) (string, error) { return ResolveDir(file) }
