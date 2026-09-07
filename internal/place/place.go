// Package place holds the palette's third mode: named locations on disk.
//
// A place is a name and a path. What Enter does with it is worked out from what
// the path turns out to be -- a folder opens in Explorer, an executable runs,
// anything else goes to whichever app Windows has registered for it -- so the
// common case needs no configuration at all. Naming an opener in places.json
// only overrides that default.
package place

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Place is one entry in places.json.
type Place struct {
	Name string `json:"name"`
	Path string `json:"path"`

	// Group is an optional heading, shown before the name in the list and
	// searchable as context, exactly like a note's folder.
	Group string `json:"group,omitempty"`

	// Open names the opener Enter should use -- "explorer", "code", "notepad++",
	// "terminal" or "default". Empty means the default worked out from the path.
	Open string `json:"open,omitempty"`

	// Elevate asks for the "runas" verb, which is what makes an editor able to
	// save to a protected path. hosts is the reason this exists: it opens fine
	// without elevation and then fails silently on save, which looks like the
	// editor being broken rather than a permission being missing.
	Elevate bool `json:"elevate,omitempty"`

	// Index is the entry's position in places.json, filled in by Load and never
	// written back. It is what a rename or a delete addresses, because the list
	// on screen is sorted for reading and its order is not the file's.
	Index int `json:"-"`
}

// Opener ids. They are the values allowed in a place's "open" field and the
// keys in the config's opener table.
const (
	OpenDefault   = "default"  // whatever Windows has registered for the file
	OpenExplorer  = "explorer" // a folder, or the folder containing a file
	OpenCode      = "code"
	OpenNotepadPP = "notepad++"
	OpenTerminal  = "terminal"
	OpenCopy      = "copy" // not an opener: put the path on the clipboard
)

// Normalize turns a path as a human would type it into one Win32 accepts.
//
// The point is that none of these should have to be got right by hand, because
// they all arrive by copying from somewhere: forward slashes from a URL, a
// config file or a Java properties file; doubled separators from string
// literals that were escaped once too often; %VARS% from the environment; and
// /d/Users/... from Git Bash, which is where half of this project's paths get
// copied out of in the first place.
//
// It is deliberately textual -- it never touches the disk -- so it cannot hang
// on a path that is not there, and it is the same answer whether or not the
// target exists.
func Normalize(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"`)
	if s == "" {
		return ""
	}

	s = expand(s)
	s = strings.ReplaceAll(s, "/", `\`)

	// A UNC path is the one case where a doubled separator is meaningful, so
	// whether the string started with one has to be remembered before the runs
	// of separators are collapsed, and put back afterwards.
	unc := strings.HasPrefix(s, `\\`)

	drive := ""
	rooted := false
	switch {
	case len(s) >= 2 && isLetter(s[0]) && s[1] == ':':
		drive = strings.ToUpper(s[:1]) + ":"
		s = s[2:]
		rooted = strings.HasPrefix(s, `\`)
	case !unc && isMSYS(s):
		// Git Bash prints /d/Users/... for D:\Users\... -- and this is a project
		// whose own build scripts run in Git Bash, so those are the paths that get
		// copied.
		drive = strings.ToUpper(s[1:2]) + ":"
		s = s[2:]
		rooted = true
	case unc:
		s = s[2:]
	default:
		rooted = strings.HasPrefix(s, `\`)
	}

	segs := clean(strings.Split(s, `\`))

	switch {
	case unc:
		return `\\` + strings.Join(segs, `\`)
	case drive != "" || rooted:
		// A bare drive is the drive's root, not the current directory on it:
		// "C:" and "C:\" mean different things to Windows and only one of them is
		// what someone writing a places file meant.
		return drive + `\` + strings.Join(segs, `\`)
	default:
		return strings.Join(segs, `\`)
	}
}

// clean drops empty and "." segments and resolves "..".
//
// Hand-rolled rather than handed to filepath.Clean so that the answer does not
// depend on the OS the tests happen to run under, and so that the UNC and
// drive prefixes above are the only things deciding the shape of the result.
func clean(segs []string) []string {
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		switch seg {
		case "", ".":
			// A run of separators, or a no-op segment.
		case "..":
			if n := len(out); n > 0 && out[n-1] != ".." {
				out = out[:n-1]
			} else {
				out = append(out, "..")
			}
		default:
			out = append(out, seg)
		}
	}
	return out
}

// expand substitutes %VARS% and a leading ~.
//
// An unset variable is left exactly as written rather than replaced with
// nothing: "%WORKSPACE%\build" silently becoming "\build" would send Explorer
// somewhere real and wrong, where the unexpanded name says what is missing.
func expand(s string) string {
	if strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) || s == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			s = home + s[1:]
		}
	}

	var b strings.Builder
	for {
		start := strings.IndexByte(s, '%')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start+1:], '%')
		if end < 0 {
			break
		}
		end += start + 1

		name := s[start+1 : end]
		val, ok := os.LookupEnv(name)
		if !ok || name == "" {
			// Keep the literal, including the percent signs, and carry on past it.
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:start])
		b.WriteString(val)
		s = s[end+1:]
	}
	b.WriteString(s)
	return b.String()
}

func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isMSYS reports the Git Bash form: a single-letter first segment standing in
// for a drive, as in \c\Users (already slash-converted from /c/Users).
func isMSYS(s string) bool {
	if len(s) < 2 || s[0] != '\\' || !isLetter(s[1]) {
		return false
	}
	return len(s) == 2 || s[2] == '\\'
}

// Kind is what a path turned out to be on disk.
type Kind int

const (
	KindMissing Kind = iota
	KindDir
	KindFile
	KindExe // a file Windows will execute rather than open
)

// runnable are the extensions that mean "run this" rather than "open this in
// something". Anything else -- .pdf, .txt, .json -- goes to the registered app.
var runnable = map[string]bool{
	".exe": true, ".bat": true, ".cmd": true, ".com": true, ".msi": true, ".ps1": true,
}

// Stat classifies a path. It touches the disk, so callers run it off the UI
// thread: a path on a disconnected network drive can block for seconds.
func Stat(path string) Kind {
	if path == "" {
		return KindMissing
	}
	fi, err := os.Stat(path)
	if err != nil {
		return KindMissing
	}
	if fi.IsDir() {
		return KindDir
	}
	if runnable[strings.ToLower(filepath.Ext(path))] {
		return KindExe
	}
	return KindFile
}

// Load reads places.json.
//
// A missing file is not an error: it is a mode nobody has filled in yet, and
// the palette says so in place of the list.
func Load(path string) ([]Place, error) {
	raws, err := readRaw(path)
	if err != nil {
		return nil, err
	}

	out := make([]Place, 0, len(raws))
	for i, raw := range raws {
		var pl Place
		if err := json.Unmarshal(raw, &pl); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		// The index is the position in the file, counted before anything is
		// skipped: it is what a later rename or delete addresses, so it has to
		// survive an entry the palette chooses not to show.
		pl.Index = i

		if strings.TrimSpace(pl.Path) == "" {
			continue
		}
		if pl.Name == "" {
			// A place with no name is still usable: the last segment of the path is
			// what someone would have called it anyway.
			pl.Name = BaseName(Normalize(pl.Path))
		}
		out = append(out, pl)
	}
	return out, nil
}

// BaseName is the last segment of a resolved path, which is the name a place
// gets when it is created from a path alone.
func BaseName(path string) string {
	path = strings.TrimRight(path, `\`)
	if i := strings.LastIndexByte(path, '\\'); i >= 0 {
		if name := path[i+1:]; name != "" {
			return name
		}
	}
	if path == "" {
		return "place"
	}
	return path
}

// IsRooted reports whether a resolved path names an absolute location: a drive,
// or a UNC share, or the root of the current drive.
//
// It is what decides whether some text the user had to hand -- the clipboard,
// or the search box -- is a path worth turning into a place. "vpn" is a search
// query; "C:\vpn" is somewhere to go.
func IsRooted(path string) bool {
	switch {
	case len(path) >= 2 && isLetter(path[0]) && path[1] == ':':
		return true
	case strings.HasPrefix(path, `\`):
		return true
	default:
		return false
	}
}

// Openers maps an opener id to the executable that serves it. Empty means the
// app is not installed, and the palette leaves that option out rather than
// offering something that will fail.
type Openers map[string]string

// DetectOpeners finds the editors and the terminal, letting config override
// any of them.
//
// Detection matters more than it looks: an install in the default location is
// the easy case, but VS Code in particular is routinely unpacked somewhere
// arbitrary, so the last resort is to find `code` on PATH and walk up from it.
func DetectOpeners(override map[string]string) Openers {
	found := Openers{}

	if exe := firstExisting(
		os.Getenv("LOCALAPPDATA")+`\Programs\Microsoft VS Code\Code.exe`,
		os.Getenv("ProgramFiles")+`\Microsoft VS Code\Code.exe`,
		codeFromPath(),
	); exe != "" {
		found[OpenCode] = exe
	}

	if exe := firstExisting(
		os.Getenv("ProgramFiles")+`\Notepad++\notepad++.exe`,
		os.Getenv("ProgramFiles(x86)")+`\Notepad++\notepad++.exe`,
	); exe != "" {
		found[OpenNotepadPP] = exe
	}

	if exe, err := exec.LookPath("wt.exe"); err == nil {
		found[OpenTerminal] = exe
	} else {
		// Every Windows has cmd, so the terminal option is never missing -- it is
		// just a plainer one.
		found[OpenTerminal] = os.Getenv("COMSPEC")
	}

	for id, exe := range override {
		if strings.TrimSpace(exe) == "" {
			delete(found, id)
			continue
		}
		found[id] = Normalize(exe)
	}
	return found
}

// codeFromPath resolves VS Code through its launcher script.
//
// `code` on PATH is code.cmd in the install's bin/ folder, and launching a .cmd
// pops a console window for as long as the editor takes to start. The real
// executable is one level up.
func codeFromPath() string {
	cmd, err := exec.LookPath("code")
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(cmd)), "Code.exe")
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
