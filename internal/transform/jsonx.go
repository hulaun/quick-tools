package transform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// JSONPretty re-indents JSON with two spaces.
//
// It decodes into json.RawMessage rather than any/map so that key order and
// number formatting survive the round trip -- decoding into a map would sort
// keys and turn large int64 ids into lossy float64s.
func JSONPretty(s string) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(strings.TrimSpace(s)), "", "  "); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}
	return buf.String(), nil
}

func JSONMinify(s string) (string, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(strings.TrimSpace(s))); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}
	return buf.String(), nil
}

// JSONEscape wraps the input as a JSON string literal, for pasting a blob into
// a config file or a test fixture.
func JSONEscape(s string) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// JSONUnescape is the inverse: a quoted string literal back to raw text. It
// tolerates a missing pair of outer quotes, which is the common case when you
// have selected the inside of a literal rather than the whole thing.
func JSONUnescape(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, `"`) {
		s = `"` + s + `"`
	}
	out, err := strconv.Unquote(s)
	if err != nil {
		return "", fmt.Errorf("not a valid quoted string: %w", err)
	}
	return out, nil
}

// JSONSortKeys pretty-prints with keys sorted, which is what makes two API
// responses actually diffable.
func JSONSortKeys(s string) (string, error) {
	var v any
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(s)))
	dec.UseNumber() // keep 64-bit ids exact
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("not valid JSON: %w", err)
	}
	// encoding/json marshals map[string]any with sorted keys already.
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func registerJSON(r *Registry) {
	add := func(id, name string, tags []string, f func(string) (string, error)) {
		r.Add(Transform{ID: id, Name: name, Group: "Json", Tags: tags, Run: f})
	}
	add("json.pretty", "Pretty print", []string{"format", "indent", "beautify"}, JSONPretty)
	add("json.minify", "Minify", []string{"compact"}, JSONMinify)
	add("json.escape", "Escape as json string", []string{"quote"}, JSONEscape)
	add("json.unescape", "Unescape json string", []string{"unquote"}, JSONUnescape)
	add("json.sortkeys", "Pretty print with sorted keys", []string{"sort", "diff"}, JSONSortKeys)
}
