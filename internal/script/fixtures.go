package script

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Case is one entry in a script's .test.json fixture file.
//
// The file is a JSON array of these:
//
//	[
//	  { "name": "simple", "in": "Person(name=John)", "out": "{\n  \"name\": \"John\"\n}" },
//	  { "name": "rejects garbage", "in": "???", "error": true }
//	]
type Case struct {
	Name string `json:"name"`
	In   string `json:"in"`
	Out  string `json:"out"`

	// Error marks a case that is supposed to fail. A parser has to reject some
	// inputs, and "it must not crash but must not accept this either" deserves to
	// be pinned down the same way a success is.
	Error bool `json:"error"`
}

// Result is the outcome of running one Case.
type Result struct {
	Case Case
	Got  string
	Err  error
	Pass bool
}

// FixturePath is the fixture file belonging to a script.
func FixturePath(scriptPath string) string {
	base := strings.TrimSuffix(scriptPath, filepath.Ext(scriptPath))
	return base + ".test.json"
}

// LoadFixtures reads a script's cases. A missing file means no fixtures, which
// is not an error -- plenty of scripts will never have any.
func LoadFixtures(scriptPath string) ([]Case, error) {
	path := FixturePath(scriptPath)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	for i := range cases {
		if cases[i].Name == "" {
			cases[i].Name = fmt.Sprintf("case %d", i+1)
		}
	}
	return cases, nil
}

// RunFixtures runs every case against the script.
func RunFixtures(s *Script, cases []Case) []Result {
	results := make([]Result, 0, len(cases))
	for _, c := range cases {
		got, err := s.Run(c.In)
		r := Result{Case: c, Got: got, Err: err}
		switch {
		case c.Error:
			r.Pass = err != nil
		case err != nil:
			r.Pass = false
		default:
			r.Pass = got == c.Out
		}
		results = append(results, r)
	}
	return results
}

// Report renders results for a terminal: one line per case, with a diff for
// each failure.
func Report(w *strings.Builder, scriptName string, results []Result) (passed, failed int) {
	for _, r := range results {
		if r.Pass {
			passed++
			fmt.Fprintf(w, "  PASS  %s\n", r.Case.Name)
			continue
		}
		failed++
		fmt.Fprintf(w, "  FAIL  %s\n", r.Case.Name)
		fmt.Fprintf(w, "        input    %s\n", visible(r.Case.In))
		switch {
		case r.Case.Error:
			fmt.Fprintf(w, "        expected an error, got %s\n", visible(r.Got))
		case r.Err != nil:
			fmt.Fprintf(w, "        error    %v\n", r.Err)
		default:
			fmt.Fprintf(w, "        expected %s\n", visible(r.Case.Out))
			fmt.Fprintf(w, "        got      %s\n", visible(r.Got))
		}
	}
	return passed, failed
}

// visible makes whitespace differences legible -- the usual reason two strings
// look identical in a terminal but are not equal.
func visible(s string) string {
	r := strings.NewReplacer("\n", "\\n", "\t", "\\t", "\r", "\\r")
	out := r.Replace(s)
	if len(out) > 200 {
		out = out[:200] + "..."
	}
	return `"` + out + `"`
}
