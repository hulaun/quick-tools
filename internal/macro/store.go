package macro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Store reads macros.json and reports when it has been edited, so a macro
// corrected by hand shows up without restarting the app -- the same contract
// place.Store offers, for the same reason.
type Store struct {
	path string

	mu   sync.Mutex
	seen time.Time
	size int64
}

func NewStore(path string) *Store { return &Store{path: path} }

// Path is where the store reads from, for the message shown when it is empty.
func (s *Store) Path() string { return s.path }

// Load returns the macros in the file, in file order, with Index filled in.
func (s *Store) Load() ([]Macro, error) { return Load(s.path) }

// Load reads macros.json.
//
// A missing file is an empty list, not an error: nobody has recorded anything
// yet. A malformed one is an error, and the palette shows it -- an empty tab
// with no reason given looks like the feature is broken.
func Load(path string) ([]Macro, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}

	var raw []Macro
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	out := make([]Macro, 0, len(raw))
	for i, m := range raw {
		// The index is assigned before anything is skipped, so it stays the
		// position in the file rather than the position in the list. Getting this
		// wrong renames the entry above or below the one on screen -- the same trap
		// place.Load has a test pinning.
		m.Index = i
		if m.Name == "" {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Changed reports whether the file has been written since the last call.
// Modification time and size together, the same test every other source here
// uses.
func (s *Store) Changed() bool {
	fi, err := os.Stat(s.path)

	var mod time.Time
	var size int64
	if err == nil {
		mod, size = fi.ModTime(), fi.Size()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if mod.Equal(s.seen) && size == s.size {
		return false
	}
	s.seen, s.size = mod, size
	return true
}
