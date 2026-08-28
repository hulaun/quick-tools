package transform

import (
	"sort"
	"strconv"
	"strings"
)

// splitLines splits on newlines, tolerating CRLF, and drops a single trailing
// empty line so a copied block with a final newline does not gain a blank item.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

func TrimLines(s string) string {
	lines := splitLines(s)
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

func DedupeLines(s string) string {
	seen := make(map[string]bool)
	var out []string
	for _, l := range splitLines(s) {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func SortLines(s string) string {
	lines := splitLines(s)
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func ReverseLines(s string) string {
	lines := splitLines(s)
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}

func RemoveBlankLines(s string) string {
	var out []string
	for _, l := range splitLines(s) {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func JoinComma(s string) string { return strings.Join(splitLines(s), ", ") }

func NumberLines(s string) string {
	lines := splitLines(s)
	width := len(strconv.Itoa(len(lines)))
	for i, l := range lines {
		lines[i] = strings.Repeat(" ", width-len(strconv.Itoa(i+1))) + strconv.Itoa(i+1) + "  " + l
	}
	return strings.Join(lines, "\n")
}

// ToSQLInList turns a column of copied ids into an IN (...) clause. Values that
// already look numeric are left unquoted.
func ToSQLInList(s string) string {
	var parts []string
	for _, l := range splitLines(s) {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if _, err := strconv.ParseFloat(l, 64); err == nil {
			parts = append(parts, l)
			continue
		}
		parts = append(parts, "'"+strings.ReplaceAll(l, "'", "''")+"'")
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func registerLines(r *Registry) {
	add := func(id, name string, tags []string, f func(string) string) {
		r.Add(Transform{ID: id, Name: name, Group: "Lines", Tags: tags, Run: pure(f)})
	}
	add("lines.trim", "Trim each line", []string{"strip"}, TrimLines)
	add("lines.dedupe", "Remove duplicate lines", []string{"uniq"}, DedupeLines)
	add("lines.sort", "Sort lines", nil, SortLines)
	add("lines.reverse", "Reverse line order", nil, ReverseLines)
	add("lines.noblank", "Remove blank lines", []string{"compact"}, RemoveBlankLines)
	add("lines.joincomma", "Join lines with commas", []string{"csv"}, JoinComma)
	add("lines.number", "Number lines", nil, NumberLines)
	add("lines.sqlin", "Lines -> SQL IN (...) list", []string{"sql", "in"}, ToSQLInList)
}
