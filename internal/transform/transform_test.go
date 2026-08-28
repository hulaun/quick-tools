package transform

import (
	"strings"
	"testing"
)

func TestPaths(t *testing.T) {
	cases := []struct {
		name string
		fn   func(string) string
		in   string
		want string
	}{
		{"ToForwardSlashes", ToForwardSlashes, `C:\Users\me\file.txt`, `C:/Users/me/file.txt`},
		{"ToBackSlashes", ToBackSlashes, `C:/Users/me/file.txt`, `C:\Users\me\file.txt`},
		{"EscapeBackslashes", EscapeBackslashes, `C:\Users\me`, `C:\\Users\\me`},
		{"UnescapeBackslashes", UnescapeBackslashes, `C:\\Users\\me`, `C:\Users\me`},
		{"ToFileURI", ToFileURI, `C:\dir\file.txt`, `file:///C:/dir/file.txt`},
		{"ToFileURI spaces", ToFileURI, `C:\my dir\f.txt`, `file:///C:/my%20dir/f.txt`},
		{"PathBase", PathBase, `C:\dir\file.txt`, `file.txt`},
		{"PathBase trailing", PathBase, `C:\dir\sub\`, `sub`},
		{"PathDir", PathDir, `C:\dir\file.txt`, `C:\dir`},
	}
	for _, c := range cases {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("%s(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestLines(t *testing.T) {
	const in = "b\na\nb\n\nc\n"
	cases := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"DedupeLines", DedupeLines, "b\na\n\nc"},
		{"SortLines", SortLines, "\na\nb\nb\nc"},
		{"RemoveBlankLines", RemoveBlankLines, "b\na\nb\nc"},
		{"ReverseLines", ReverseLines, "c\n\nb\na\nb"},
		{"JoinComma", JoinComma, "b, a, b, , c"},
	}
	for _, c := range cases {
		if got := c.fn(in); got != c.want {
			t.Errorf("%s = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSQLInList(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1\n2\n3", "(1, 2, 3)"},
		{"abc\ndef", "('abc', 'def')"},
		{"O'Brien", "('O''Brien')"}, // embedded quote must be doubled
		{"1\nabc", "(1, 'abc')"},    // mixed: numbers stay bare
		{" 7 \n\n 8 ", "(7, 8)"},    // blanks dropped, values trimmed
	}
	for _, c := range cases {
		if got := ToSQLInList(c.in); got != c.want {
			t.Errorf("ToSQLInList(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEncodingRoundTrips(t *testing.T) {
	const s = "hello, wörld & <friends>"
	if got, err := Base64Decode(Base64Encode(s)); err != nil || got != s {
		t.Errorf("base64 round trip = %q, %v", got, err)
	}
	if got, err := URLDecode(URLEncode(s)); err != nil || got != s {
		t.Errorf("url round trip = %q, %v", got, err)
	}
	if got, err := HexDecode(HexEncode(s)); err != nil || got != s {
		t.Errorf("hex round trip = %q, %v", got, err)
	}
	if got := HTMLDecode(HTMLEncode(s)); got != s {
		t.Errorf("html round trip = %q", got)
	}
}

// Base64 in the wild arrives url-safe, unpadded, or line-wrapped. All must decode.
func TestBase64Lenient(t *testing.T) {
	for _, in := range []string{"aGVsbG8=", "aGVsbG8", "aGVs\nbG8=", " aGVsbG8= "} {
		got, err := Base64Decode(in)
		if err != nil || got != "hello" {
			t.Errorf("Base64Decode(%q) = %q, %v; want \"hello\"", in, got, err)
		}
	}
}

func TestJSON(t *testing.T) {
	if got, err := JSONMinify(`{ "a" : 1 }`); err != nil || got != `{"a":1}` {
		t.Errorf("JSONMinify = %q, %v", got, err)
	}
	if got, err := JSONPretty(`{"a":1}`); err != nil || got != "{\n  \"a\": 1\n}" {
		t.Errorf("JSONPretty = %q, %v", got, err)
	}
	if _, err := JSONPretty("not json"); err == nil {
		t.Error("JSONPretty accepted invalid JSON")
	}
	// A large id must survive verbatim. Decoding into float64 would corrupt it,
	// which is exactly the bug this transform exists to avoid introducing.
	const big = `{"id":9007199254740993}`
	if got, err := JSONSortKeys(big); err != nil || got != "{\n  \"id\": 9007199254740993\n}" {
		t.Errorf("JSONSortKeys lost precision: %q, %v", got, err)
	}
}

func TestJSONUnescapeTolerantOfMissingQuotes(t *testing.T) {
	for _, in := range []string{`"a\nb"`, `a\nb`} {
		if got, err := JSONUnescape(in); err != nil || got != "a\nb" {
			t.Errorf("JSONUnescape(%q) = %q, %v", in, got, err)
		}
	}
}

func TestJWTDecode(t *testing.T) {
	// {"alg":"HS256"} . {"sub":"123"} . sig
	const tok = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.abc"
	got, err := JWTDecode(tok)
	if err != nil {
		t.Fatalf("JWTDecode: %v", err)
	}
	for _, want := range []string{"HS256", `"sub": "123"`, "not verified"} {
		if !strings.Contains(got, want) {
			t.Errorf("JWTDecode output missing %q:\n%s", want, got)
		}
	}
	if _, err := JWTDecode("nodots"); err == nil {
		t.Error("JWTDecode accepted a non-token")
	}
}

func TestRegistryReplaceKeepsOrder(t *testing.T) {
	r := NewRegistry()
	r.Add(Transform{ID: "a", Name: "A", Run: pure(func(string) string { return "1" })})
	r.Add(Transform{ID: "b", Name: "B", Run: pure(func(string) string { return "x" })})
	// Hot-reload replaces in place rather than appending a duplicate.
	r.Add(Transform{ID: "a", Name: "A", Run: pure(func(string) string { return "2" })})

	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	tr, _ := r.Get("a")
	if got, _ := tr.Run(""); got != "2" {
		t.Errorf("replaced transform ran the old body: %q", got)
	}
	r.Remove("a")
	if _, ok := r.Get("a"); ok || r.Len() != 1 {
		t.Error("Remove did not drop the transform")
	}
}

func TestBuiltinsAllRunnable(t *testing.T) {
	r := NewRegistry()
	RegisterBuiltins(r)
	if r.Len() < 30 {
		t.Errorf("only %d builtins registered", r.Len())
	}
	// No transform may panic on empty input: the palette previews the highlighted
	// entry against whatever happens to be on the clipboard, including nothing.
	for _, tr := range r.All() {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s panicked on empty input: %v", tr.ID, p)
				}
			}()
			tr.Run("")
		}()
	}
}
