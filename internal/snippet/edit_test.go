package snippet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveNormalisesLineEndings(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "one\ntwo\n")

	s := NewStore(dir)
	// What comes back out of a Win32 Edit is always CRLF, whatever went in.
	if err := s.Save("a.txt", "one\r\ntwo\r\nthree"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\ntwo\nthree" {
		t.Errorf("file = %q, want LF line endings", string(got))
	}
}

func TestSaveRefusesToEscapeTheRoot(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Save("../outside.txt", "no"); err == nil {
		t.Error("a path outside the root was accepted")
	}
}

func TestCreateNote(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	sn, err := s.CreateNote("work/vpn", "office login")
	if err != nil {
		t.Fatal(err)
	}
	if sn.ID != "work/vpn/office login.txt" {
		t.Errorf("ID = %q", sn.ID)
	}
	if sn.Name != "office login" || sn.Folder != "work/vpn" {
		t.Errorf("name %q folder %q", sn.Name, sn.Folder)
	}
	if _, err := os.Stat(sn.Path); err != nil {
		t.Errorf("file was not created: %v", err)
	}
}

func TestCreateNoteNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "db.txt", "the real password")

	s := NewStore(dir)
	sn, err := s.CreateNote("", "db")
	if err != nil {
		t.Fatal(err)
	}
	if sn.ID != "db-2.txt" {
		t.Errorf("ID = %q, want the name to have been made unique", sn.ID)
	}

	kept, err := os.ReadFile(filepath.Join(dir, "db.txt"))
	if err != nil || string(kept) != "the real password" {
		t.Errorf("the existing note was damaged: %q %v", string(kept), err)
	}
}

func TestCreateNoteKeepsAnExplicitExtension(t *testing.T) {
	dir := t.TempDir()
	sn, err := NewStore(dir).CreateNote("", "settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if sn.ID != "settings.json" || sn.Name != "settings" {
		t.Errorf("ID = %q name = %q", sn.ID, sn.Name)
	}
}

func TestCreateFolderAndFolders(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	id, err := s.CreateFolder("work", "vpn")
	if err != nil {
		t.Fatal(err)
	}
	if id != "work/vpn" {
		t.Errorf("id = %q", id)
	}

	// An empty folder still has to be listed: it is where the next note goes.
	if got := strings.Join(s.Folders(), ","); got != "work,work/vpn" {
		t.Errorf("folders = %q", got)
	}
}

func TestEmptyFolderCountsAsAChange(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "x")

	s := NewStore(dir)
	s.Load()
	if _, err := s.CreateFolder("", "new"); err != nil {
		t.Fatal(err)
	}
	if !s.Changed() {
		t.Error("a new empty folder was not detected")
	}
}

func TestDeleteNote(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "one")
	mkfile(t, dir, "b.txt", "two")

	s := NewStore(dir)
	if err := s.Delete("a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Error("the note is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Error("deleting one note took another with it")
	}
}

// A folder goes with everything in it. The palette says how many that is
// before asking, so this is the behaviour the confirmation describes.
func TestDeleteFolderTakesItsContents(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "work", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkfile(t, filepath.Join(dir, "work"), "a.txt", "one")
	mkfile(t, filepath.Join(dir, "work", "deep"), "b.txt", "two")
	mkfile(t, dir, "keep.txt", "three")

	s := NewStore(dir)
	if err := s.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "work")); !os.IsNotExist(err) {
		t.Error("the folder is still there")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Error("deleting a folder took a note outside it")
	}
}

func TestDeleteRefusesTheRoot(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "one")

	s := NewStore(dir)
	for _, id := range []string{"", ".", "../" + filepath.Base(dir)} {
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q) was accepted", id)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Error("the snippets folder was emptied")
	}
}

// Deleting something that is already gone is success: the desired end state is
// "not there", and a watcher reload can easily have got there first.
func TestDeleteMissingIsNotAnError(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Delete("nope.txt"); err != nil {
		t.Errorf("deleting a missing note returned %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"  db conn  ":   "db conn",
		"a/b":           "a-b",
		"host:port":     "host-port",
		`what\now?`:     "what-now-",
		"trailing dot.": "trailing dot",
		"":              "",
	}
	for in, want := range cases {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenameKeepsTheExtension(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "work/db.conf", "body")

	s := NewStore(dir)
	id, err := s.Rename("work/db.conf", "prod-db")
	if err != nil {
		t.Fatal(err)
	}
	if id != "work/prod-db.conf" {
		t.Errorf("id = %q, want the extension kept", id)
	}
	got, err := os.ReadFile(filepath.Join(dir, "work", "prod-db.conf"))
	if err != nil || string(got) != "body" {
		t.Errorf("contents did not survive the rename: %q %v", string(got), err)
	}
}

func TestRenameTakesAnExplicitExtension(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "x")
	id, err := NewStore(dir).Rename("a.txt", "a.json")
	if err != nil || id != "a.json" {
		t.Errorf("id = %q, %v", id, err)
	}
}

func TestRenameFolder(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "work/db.txt", "x")

	s := NewStore(dir)
	id, err := s.Rename("work", "office")
	if err != nil {
		t.Fatal(err)
	}
	if id != "office" {
		t.Errorf("id = %q", id)
	}
	// The notes inside come with it.
	if got := ids(s.Load()); got != "office/db.txt" {
		t.Errorf("after renaming the folder, notes = %q", got)
	}
}

func TestRenameRefusesToReplace(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "keep.txt", "the real password")
	mkfile(t, dir, "other.txt", "x")

	if _, err := NewStore(dir).Rename("other.txt", "keep"); err == nil {
		t.Fatal("renaming onto an existing note was allowed")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "keep.txt"))
	if string(got) != "the real password" {
		t.Errorf("the existing note was overwritten: %q", string(got))
	}
}

func TestRenameSanitizes(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "x")
	id, err := NewStore(dir).Rename("a.txt", "host:port")
	if err != nil || id != "host-port.txt" {
		t.Errorf("id = %q, %v", id, err)
	}
}
