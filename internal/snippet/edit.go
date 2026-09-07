package snippet

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DefaultExt is the extension given to a note created without one.
const DefaultExt = ".txt"

// Save writes text back to a snippet.
//
// Line endings are normalised to LF on the way in. The editor is a Win32 Edit
// control, which only understands CRLF and hands back CRLF for every line
// whatever the file contained; writing that straight out would silently
// rewrite an LF file the first time it was opened and saved.
func (s *Store) Save(id, text string) error {
	path, err := s.resolve(id)
	if err != nil {
		return err
	}
	body := strings.ReplaceAll(text, "\r\n", "\n")
	return os.WriteFile(path, []byte(body), 0o644)
}

// CreateNote makes an empty note in folder (relative, slash-separated, "" for
// the root) and returns it.
//
// An existing file is never overwritten -- a numeric suffix is added until the
// name is free. Creating a note is a keystroke away, so silently clobbering a
// stored password on a name collision would be far too easy.
func (s *Store) CreateNote(folder, name string) (Snippet, error) {
	name = SanitizeName(name)
	if name == "" {
		name = "note"
	}
	if filepath.Ext(name) == "" {
		name += DefaultExt
	}

	dir, err := s.resolve(folder)
	if err != nil {
		return Snippet{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Snippet{}, err
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	final := name
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, final)); os.IsNotExist(err) {
			break
		}
		final = fmt.Sprintf("%s-%d%s", base, i, ext)
	}

	full := filepath.Join(dir, final)
	if err := os.WriteFile(full, nil, 0o644); err != nil {
		return Snippet{}, err
	}

	return Snippet{
		ID:     joinRel(folder, final),
		Name:   strings.TrimSuffix(final, ext),
		Folder: folder,
		Path:   full,
	}, nil
}

// CreateFolder makes a folder under parent and returns its relative id.
func (s *Store) CreateFolder(parent, name string) (string, error) {
	name = SanitizeName(name)
	if name == "" {
		name = "folder"
	}
	dir, err := s.resolve(joinRel(parent, name))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return joinRel(parent, name), nil
}

// Rename gives a note or a folder a new name, in place, and returns its new id.
//
// A note keeps its extension unless the new name supplies one, so renaming
// "db-conn.txt" to "prod-db" gives "prod-db.txt" -- the extension is not part
// of what the palette shows, so being asked to retype it would be a trap.
//
// An existing file is never replaced. Unlike a create, which quietly picks the
// next free name, a rename that collides is refused: the user named a specific
// thing, and silently landing on "db-2" would be a worse answer than being told.
func (s *Store) Rename(id, name string) (string, error) {
	old, err := s.resolve(id)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(old)
	if err != nil {
		return "", err
	}

	name = SanitizeName(name)
	if name == "" {
		return "", fmt.Errorf("a name cannot be empty")
	}
	if !info.IsDir() && filepath.Ext(name) == "" {
		name += filepath.Ext(old)
	}

	dir := filepath.Dir(old)
	target := filepath.Join(dir, name)
	if !strings.EqualFold(target, old) {
		if _, err := os.Stat(target); err == nil {
			return "", fmt.Errorf("%q already exists", name)
		}
	}
	if err := os.Rename(old, target); err != nil {
		return "", err
	}

	parent := path.Dir(filepath.ToSlash(id))
	if parent == "." {
		parent = ""
	}
	return joinRel(parent, name), nil
}

// Delete removes a note, or a folder and everything inside it.
//
// It is a permanent delete: os.Remove is a straight DeleteFile, so nothing goes
// to the Recycle Bin and there is nothing to restore. That is deliberate --
// these are configs and credentials, and leaving copies of them in the Bin is
// the opposite of what a notes store for passwords should do. The caller is
// expected to have asked first.
func (s *Store) Delete(id string) error {
	full, err := s.resolve(id)
	if err != nil {
		return err
	}
	if full == s.dir {
		return fmt.Errorf("the snippets folder itself cannot be deleted")
	}
	return os.RemoveAll(full)
}

// SanitizeName strips what Windows will not accept in a filename.
//
// The name comes from whatever is typed in the search box, so it is entirely
// ordinary for it to contain a colon or a slash. Rejecting it with an error
// would be worse than quietly making it usable.
func SanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return '-'
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, name)
	// A trailing dot or space is legal to create and then impossible to delete
	// in Explorer.
	return strings.TrimRight(name, ". ")
}

// resolve turns a relative, slash-separated id into a path inside the root.
//
// It refuses anything that would escape the root. These ids come from the UI
// rather than from a network, but a note named ".." should fail loudly here
// rather than write somewhere unexpected.
func (s *Store) resolve(id string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(id))
	if clean == "." {
		return s.dir, nil
	}
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("%q is outside the snippets folder", id)
	}
	return filepath.Join(s.dir, clean), nil
}

// joinRel joins two relative id parts, tolerating an empty parent.
func joinRel(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}
