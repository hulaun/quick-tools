package snippet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkfile(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func ids(sns []Snippet) string {
	out := make([]string, len(sns))
	for i, s := range sns {
		out[i] = s.ID
	}
	return strings.Join(out, ",")
}

func TestWalksTree(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "work/db-conn.txt", "host=localhost")
	mkfile(t, dir, "work/vpn.conf", "vpn")
	mkfile(t, dir, "personal/ssh-key.txt", "key")
	mkfile(t, dir, "loose.txt", "top level")

	got := NewStore(dir).Load()
	// Sorted by folder then name, so the palette mirrors the tree. The empty
	// folder (root) sorts first.
	if want := "loose.txt,personal/ssh-key.txt,work/db-conn.txt,work/vpn.conf"; ids(got) != want {
		t.Errorf("ids = %q, want %q", ids(got), want)
	}
}

func TestFieldsAreSplitCorrectly(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "work/db-conn.txt", "x")
	mkfile(t, dir, "loose.md", "x")

	got := NewStore(dir).Load()
	byID := map[string]Snippet{}
	for _, s := range got {
		byID[s.ID] = s
	}

	if s := byID["work/db-conn.txt"]; s.Name != "db-conn" || s.Folder != "work" {
		t.Errorf("nested snippet = name %q folder %q", s.Name, s.Folder)
	}
	if s := byID["loose.md"]; s.Name != "loose" || s.Folder != "" {
		t.Errorf("root snippet = name %q folder %q", s.Name, s.Folder)
	}
}

func TestText(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.txt", "line one\nline two\n\n")

	got := NewStore(dir).Load()
	if len(got) != 1 {
		t.Fatalf("got %d snippets", len(got))
	}
	// Trailing newlines are stripped: a file almost always ends with one, and
	// pasting it would add a stray line break to whatever you paste into.
	text, err := got[0].Text()
	if err != nil || text != "line one\nline two" {
		t.Errorf("Text = %q, %v", text, err)
	}
}

func TestSkipsHiddenAndOversized(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "keep.txt", "yes")
	mkfile(t, dir, ".hidden.txt", "no")
	mkfile(t, dir, ".git/config", "no")
	mkfile(t, dir, "big.txt", strings.Repeat("x", MaxSize+1))

	got := NewStore(dir).Load()
	if ids(got) != "keep.txt" {
		t.Errorf("ids = %q, want just keep.txt", ids(got))
	}
}

func TestMissingFolderIsNotAnError(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "nope"))
	if got := s.Load(); len(got) != 0 {
		t.Errorf("got %d snippets, want none", len(got))
	}
	if s.Changed() {
		t.Error("a folder that never existed should not report a change")
	}
}

func TestChangeDetection(t *testing.T) {
	dir := t.TempDir()
	p := mkfile(t, dir, "a.txt", "one")

	s := NewStore(dir)
	s.Load()
	if s.Changed() {
		t.Fatal("no change expected straight after a load")
	}

	if err := os.WriteFile(p, []byte("one but longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !s.Changed() {
		t.Fatal("an edited snippet was not detected")
	}
	s.Load()

	mkfile(t, dir, "sub/b.txt", "two")
	if !s.Changed() {
		t.Fatal("a new snippet in a new folder was not detected")
	}
	s.Load()

	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if !s.Changed() {
		t.Fatal("a deleted snippet was not detected")
	}
}

func TestLooksBinary(t *testing.T) {
	if LooksBinary("plain text\nwith lines") {
		t.Error("plain text flagged as binary")
	}
	if LooksBinary("unicode: wörld 日本語") {
		t.Error("unicode text flagged as binary")
	}
	if !LooksBinary("has a \x00 nul") {
		t.Error("NUL byte not detected")
	}
	if !LooksBinary(string([]byte{0xff, 0xfe, 0x41})) {
		t.Error("invalid utf-8 not detected")
	}
}
