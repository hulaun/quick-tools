package place

import (
	"os"
	"sync"
	"time"
)

// Store reads places.json and reports when it has been edited, so the palette
// picks up a new entry the same way it picks up a new script -- by saving the
// file, without restarting the app.
type Store struct {
	path string

	mu   sync.Mutex
	seen time.Time
	size int64
}

func NewStore(path string) *Store { return &Store{path: path} }

// Path is where the store reads from, for the message shown when it is empty.
func (s *Store) Path() string { return s.path }

// Load returns the current contents, and any error reading them. The error is
// returned rather than logged because a malformed places.json is worth showing
// in the pane: the alternative is an empty list and no reason for it.
func (s *Store) Load() ([]Place, error) { return Load(s.path) }

// Changed reports whether the file has been written since the last call.
//
// Modification time and size together, the same test the script loader uses. A
// second's polling is well inside what feels immediate for a file someone just
// saved, and it costs one stat.
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
