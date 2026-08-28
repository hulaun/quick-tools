// Package config loads and saves the user's settings.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	// ScriptsDir and SnippetsDir default to folders beside the executable, so
	// the whole thing stays portable -- copy the folder, keep your transforms.
	ScriptsDir  string `json:"scriptsDir"`
	SnippetsDir string `json:"snippetsDir"`
}

// Default returns the settings used when no file exists yet.
func Default() Config {
	return Config{
		Hotkey:      "Ctrl+Alt+Space",
		AutoPaste:   false,
		ScriptsDir:  "scripts",
		SnippetsDir: "snippets",
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

// ResolveDir turns a possibly-relative directory from the config into an
// absolute path, anchored next to the executable rather than the current
// working directory -- which is wherever Explorer happened to launch us from.
func ResolveDir(dir string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), dir), nil
}
