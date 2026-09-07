// Package snippet reads the stored-text folder that replaces Win+V.
//
// A snippet is just a text file. The folder tree is the organisation Win+V
// never offered:
//
//	snippets/
//	  work/
//	    db-conn.txt
//	    vpn.conf
//	  personal/
//	    ssh-key.txt
//
// Because they are ordinary files, they can be edited in any editor, backed up,
// synced, or put in git. There is no database and no proprietary format.
package snippet

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxSize caps what will be loaded as a snippet.
//
// The store walks whatever is in the folder, and a stray binary or a huge log
// would otherwise be read into memory and offered as pasteable text.
const MaxSize = 1 << 20 // 1 MiB

// Snippet is one stored piece of text.
type Snippet struct {
	// ID is the path relative to the root, slash-separated: "work/db-conn.txt".
	ID string

	// Name is the filename without its extension: "db-conn".
	Name string

	// Folder is the containing path, or "" at the root: "work".
	Folder string

	Path string
	Size int64
}

// Store walks the snippets folder.
type Store struct {
	mu   sync.Mutex
	dir  string
	seen map[string]stamp

	// seenDirs tracks folders as well as files, so that creating an empty folder
	// counts as a change. Without it a new folder would be invisible until the
	// first note was put in it, which looks exactly like the create having
	// failed.
	seenDirs map[string]bool
}

type stamp struct {
	mod  time.Time
	size int64
}

func NewStore(dir string) *Store {
	return &Store{dir: dir, seen: make(map[string]stamp), seenDirs: make(map[string]bool)}
}

// Dir is the folder being watched.
func (s *Store) Dir() string { return s.dir }

// Changed reports whether the tree has changed since the last Load.
func (s *Store) Changed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, dirs, err := s.walk()
	if err != nil {
		return len(s.seen) > 0 || len(s.seenDirs) > 0
	}
	if len(found) != len(s.seen) || len(dirs) != len(s.seenDirs) {
		return true
	}
	for path, st := range found {
		prev, ok := s.seen[path]
		if !ok || prev != st {
			return true
		}
	}
	for path := range dirs {
		if !s.seenDirs[path] {
			return true
		}
	}
	return false
}

// Folders returns every folder under the root, slash-separated and relative,
// sorted so that a parent comes before its children.
//
// Folders are listed separately from snippets because an empty one still has
// to be selectable: it is where the next note gets created.
func (s *Store) Folders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, dirs, err := s.walk()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(dirs))
	for path := range dirs {
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

// Load returns every snippet, sorted by folder then name so the palette order
// mirrors the folder tree.
func (s *Store) Load() []Snippet {
	s.mu.Lock()
	defer s.mu.Unlock()

	found, dirs, err := s.walk()
	if err != nil {
		s.seen = map[string]stamp{}
		s.seenDirs = map[string]bool{}
		return nil
	}
	s.seen = found
	s.seenDirs = dirs

	out := make([]Snippet, 0, len(found))
	for path, st := range found {
		rel, err := filepath.Rel(s.dir, path)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)

		base := filepath.Base(rel)
		folder := ""
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			folder = rel[:i]
		}

		out = append(out, Snippet{
			ID:     rel,
			Name:   strings.TrimSuffix(base, filepath.Ext(base)),
			Folder: folder,
			Path:   path,
			Size:   st.size,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Text reads a snippet's contents.
//
// It is read on demand rather than held in memory: these files hold configs and
// credentials, and there is no reason for all of them to sit in the process for
// its whole lifetime when only one is ever pasted at a time.
func (sn Snippet) Text() (string, error) {
	data, err := os.ReadFile(sn.Path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

// walk collects every readable text file under the root, and every folder.
func (s *Store) walk() (map[string]stamp, map[string]bool, error) {
	if _, err := os.Stat(s.dir); err != nil {
		return nil, nil, err
	}

	out := make(map[string]stamp)
	dirs := make(map[string]bool)
	err := filepath.WalkDir(s.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory should not abandon the whole tree.
			return nil
		}
		if d.IsDir() {
			// Skip the places tooling puts things nobody wants in a palette.
			if name := d.Name(); path != s.dir && (name == ".git" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			if path != s.dir {
				dirs[path] = true
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSize {
			return nil
		}
		out[path] = stamp{mod: info.ModTime(), size: info.Size()}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, dirs, nil
}

// LooksBinary reports whether content is not usable as pasteable text.
//
// Checked at paste time rather than at walk time: reading every file to sniff
// it would defeat the point of loading them lazily.
func LooksBinary(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	// A NUL byte is the reliable giveaway; no text file contains one.
	return strings.ContainsRune(s, 0)
}
