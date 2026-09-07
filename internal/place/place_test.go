package place

import (
	"os"
	"testing"
)

// TestNormalize pins the path shapes a place can be written in. They are all
// forms that arrive by copying from somewhere -- a Git Bash prompt, a Java
// properties file, an over-escaped string literal -- rather than by being typed
// out, which is why they have to be accepted rather than corrected by hand.
func TestNormalize(t *testing.T) {
	t.Setenv("USERPROFILE", `C:\Users\test`)
	t.Setenv("WINDIR", `C:\Windows`)

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"already correct", `C:\Users\test\.m2`, `C:\Users\test\.m2`},
		{"forward slashes", "C:/Users/test/.m2", `C:\Users\test\.m2`},
		{"doubled separators", `C:\\Users\\test\\.m2`, `C:\Users\test\.m2`},
		{"doubled forward slashes", "C://Users//test//.m2", `C:\Users\test\.m2`},
		{"mixed separators", `C:/Users\test//.m2`, `C:\Users\test\.m2`},
		{"lowercase drive", `d:\work`, `D:\work`},
		{"git bash form", "/d/Users/Personal/Projects", `D:\Users\Personal\Projects`},
		{"git bash root", "/c", `C:\`},
		{"trailing separator", `C:\Users\test\`, `C:\Users\test`},
		{"bare drive", "C:", `C:\`},
		{"quoted", `"C:\Program Files\Go"`, `C:\Program Files\Go`},
		{"surrounding space", "  C:/temp  ", `C:\temp`},
		{"env var", `%USERPROFILE%\.m2`, `C:\Users\test\.m2`},
		{"env var mid-path", `%WINDIR%\System32\drivers\etc\hosts`, `C:\Windows\System32\drivers\etc\hosts`},
		{"unset env var kept", `%NOPE%\build`, `%NOPE%\build`},
		{"dot segment", `C:\Users\.\test`, `C:\Users\test`},
		{"parent segment", `C:\Users\test\..\other`, `C:\Users\other`},
		{"unc", `\\server\share\folder`, `\\server\share\folder`},
		{"unc forward slashes", "//server/share/folder", `\\server\share\folder`},
		{"empty", "", ""},
		{"spaces only", "   ", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Normalize(c.in); got != c.want {
				t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestNormalizeHome covers the leading ~, which cannot be pinned to a literal
// because the home directory is whoever is running the test.
func TestNormalizeHome(t *testing.T) {
	got := Normalize("~/.m2")
	if got == `~\.m2` || got == "" {
		t.Fatalf("Normalize(~/.m2) = %q, want the home directory expanded", got)
	}
	if want := `\.m2`; got[len(got)-len(want):] != want {
		t.Errorf("Normalize(~/.m2) = %q, want it to end in %q", got, want)
	}
}

// TestNormalizeIsPure checks the promise that resolution never touches the
// disk: a path that is not there must still come back in its Win32 form, so a
// disconnected network drive cannot stall the window.
func TestNormalizeIsPure(t *testing.T) {
	if got, want := Normalize("//nosuchserver/nosuchshare/x"), `\\nosuchserver\nosuchshare\x`; got != want {
		t.Errorf("Normalize = %q, want %q", got, want)
	}
	if got, want := Normalize("Z:/definitely/not/here"), `Z:\definitely\not\here`; got != want {
		t.Errorf("Normalize = %q, want %q", got, want)
	}
}

func TestStatMissing(t *testing.T) {
	if got := Stat(`Z:\definitely\not\here`); got != KindMissing {
		t.Errorf("Stat of a missing path = %v, want KindMissing", got)
	}
	if got := Stat(""); got != KindMissing {
		t.Errorf("Stat of an empty path = %v, want KindMissing", got)
	}
}

func TestStatKinds(t *testing.T) {
	dir := t.TempDir()
	if got := Stat(dir); got != KindDir {
		t.Errorf("Stat of a folder = %v, want KindDir", got)
	}

	// An extension-less file is the case that text alone cannot classify, and
	// the reason the palette stats rather than guessing: hosts is a file, .m2 is
	// a folder, and neither has anything in its name to say so.
	for _, c := range []struct {
		name string
		want Kind
	}{
		{"hosts", KindFile},
		{"notes.txt", KindFile},
		{"doc.pdf", KindFile},
		{"tool.exe", KindExe},
		{"go.CMD", KindExe},
	} {
		path := dir + `\` + c.name
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Stat(path); got != c.want {
			t.Errorf("Stat(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	places, err := Load(t.TempDir() + `\nope.json`)
	if err != nil {
		t.Fatalf("Load of a missing file returned %v, want nil", err)
	}
	if len(places) != 0 {
		t.Errorf("Load of a missing file returned %d places, want 0", len(places))
	}
}

func TestLoadNamesFromPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + `\places.json`
	body := `[{"path":"C:/Users/test/.m2"},{"name":"","path":"  "}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	places, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("got %d places, want 1 -- the one with a blank path should be dropped", len(places))
	}
	if places[0].Name != ".m2" {
		t.Errorf("name = %q, want %q taken from the last path segment", places[0].Name, ".m2")
	}
}
