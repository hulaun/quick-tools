package transform

import (
	"net/url"
	"strings"
)

// ToForwardSlashes is the one you reach for constantly: a Windows path pasted
// into anything that wants POSIX separators.
func ToForwardSlashes(s string) string { return strings.ReplaceAll(s, `\`, `/`) }

func ToBackSlashes(s string) string { return strings.ReplaceAll(s, `/`, `\`) }

// EscapeBackslashes doubles separators for pasting a Windows path into a Java,
// Go or JSON string literal.
func EscapeBackslashes(s string) string { return strings.ReplaceAll(s, `\`, `\\`) }

func UnescapeBackslashes(s string) string { return strings.ReplaceAll(s, `\\`, `\`) }

// ToFileURI turns C:\dir\file.txt into file:///C:/dir/file.txt.
func ToFileURI(s string) string {
	p := ToForwardSlashes(strings.TrimSpace(s))
	if strings.HasPrefix(strings.ToLower(p), "file:") {
		return p
	}
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		// Leave a drive letter alone; escaping its colon breaks the URI.
		if i == 0 && len(seg) == 2 && seg[1] == ':' {
			continue
		}
		segs[i] = url.PathEscape(seg)
	}
	p = strings.Join(segs, "/")
	if strings.HasPrefix(p, "/") {
		return "file://" + p
	}
	return "file:///" + p
}

// PathBase returns the last segment, tolerating either separator.
func PathBase(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), `/\`)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[i+1:]
	}
	return s
}

// PathDir returns everything but the last segment.
func PathDir(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), `/\`)
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[:i]
	}
	return ""
}

func registerPaths(r *Registry) {
	add := func(id, name string, tags []string, f func(string) string) {
		r.Add(Transform{ID: id, Name: name, Group: "Path", Tags: tags, Run: pure(f)})
	}
	add("path.forward", `Backslashes -> forward slashes`, []string{"unix", "posix", "/"}, ToForwardSlashes)
	add("path.back", `Forward slashes -> backslashes`, []string{"windows", `\`}, ToBackSlashes)
	add("path.escape", `Escape backslashes (\ -> \\)`, []string{"literal"}, EscapeBackslashes)
	add("path.unescape", `Unescape backslashes (\\ -> \)`, nil, UnescapeBackslashes)
	add("path.fileuri", "Path -> file:// URI", []string{"uri", "url"}, ToFileURI)
	add("path.base", "Basename", []string{"filename"}, PathBase)
	add("path.dir", "Directory name", []string{"dirname", "folder"}, PathDir)
}
