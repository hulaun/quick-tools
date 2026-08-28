package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Loader keeps the scripts folder and the palette in step.
//
// Changed is called from a polling goroutine while Load runs on the UI thread,
// so the bookkeeping is guarded.
type Loader struct {
	mu  sync.Mutex
	dir string

	// stamps records the size and modification time of each file seen, which is
	// how a change is detected without a filesystem watcher.
	stamps map[string]stamp

	scripts map[string]*Script
	errs    map[string]error
}

type stamp struct {
	mod  time.Time
	size int64
}

// Entry is one loadable script, or one that failed to load.
//
// Failures are surfaced rather than dropped: a script with a syntax error still
// appears in the palette, and selecting it shows the error in the preview pane.
// Silently vanishing is the worst possible feedback for someone mid-edit.
type Entry struct {
	ID    string
	Name  string
	Group string
	Tags  []string
	Run   func(string) (string, error)
}

func NewLoader(dir string) *Loader {
	return &Loader{
		dir:     dir,
		stamps:  make(map[string]stamp),
		scripts: make(map[string]*Script),
		errs:    make(map[string]error),
	}
}

// Dir is the folder being watched.
func (l *Loader) Dir() string { return l.dir }

// Changed reports whether any .js file has appeared, vanished or been modified
// since the last Load. It is cheap enough to poll.
func (l *Loader) Changed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	files, err := l.list()
	if err != nil {
		// A missing folder is a legitimate state, and matches "no scripts". It
		// only counts as a change if we previously had some.
		return len(l.stamps) > 0
	}
	if len(files) != len(l.stamps) {
		return true
	}
	for path, st := range files {
		prev, ok := l.stamps[path]
		if !ok || prev != st {
			return true
		}
	}
	return false
}

// Load reads every script in the folder and returns the resulting entries,
// sorted by name for a stable palette order.
func (l *Loader) Load() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()

	files, err := l.list()
	if err != nil {
		l.stamps = map[string]stamp{}
		l.scripts = map[string]*Script{}
		l.errs = map[string]error{}
		return nil
	}

	newStamps := make(map[string]stamp, len(files))
	newScripts := make(map[string]*Script, len(files))
	newErrs := make(map[string]error)

	for path, st := range files {
		id := idFor(path)
		newStamps[path] = st

		// Reuse the compiled script when the file has not changed. Recompiling
		// every scan would throw away the runtime on each keystroke-triggered
		// poll for no reason.
		if prev, ok := l.stamps[path]; ok && prev == st {
			if s, ok := l.scripts[path]; ok {
				newScripts[path] = s
				continue
			}
			if e, ok := l.errs[path]; ok {
				newErrs[path] = e
				continue
			}
		}

		src, err := os.ReadFile(path)
		if err != nil {
			newErrs[path] = fmt.Errorf("could not read: %w", err)
			continue
		}
		s, err := Compile(id, path, string(src))
		if err != nil {
			newErrs[path] = err
			continue
		}
		newScripts[path] = s
	}

	l.stamps, l.scripts, l.errs = newStamps, newScripts, newErrs

	entries := make([]Entry, 0, len(files))
	for path, s := range l.scripts {
		script := s
		entries = append(entries, Entry{
			ID:    script.ID,
			Name:  script.Name,
			Group: script.Group,
			Tags:  script.Tags,
			Run:   script.Run,
		})
		_ = path
	}
	for path, e := range l.errs {
		err := e
		base := idFor(path)
		entries = append(entries, Entry{
			ID:    base,
			Name:  filepath.Base(path) + " (error)",
			Group: "Scripts",
			Tags:  []string{"broken", "error"},
			Run: func(string) (string, error) {
				return "", err
			},
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// list stamps every .js file in the folder.
func (l *Loader) list() (map[string]stamp, error) {
	des, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]stamp)
	for _, de := range des {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), ".js") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		out[filepath.Join(l.dir, de.Name())] = stamp{mod: info.ModTime(), size: info.Size()}
	}
	return out, nil
}

// idFor builds a stable transform id from a path, e.g. "script.java-to-json".
func idFor(path string) string {
	base := filepath.Base(path)
	return "script." + strings.TrimSuffix(base, filepath.Ext(base))
}
