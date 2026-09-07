package script

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RunAllFixtures compiles every script in dir, runs its fixtures, and writes a
// report. It returns the number of failing cases.
//
// This is the loop for writing a parser: edit the .js, run this, watch the red
// turn green. It needs no window and no clipboard, so it can be run from a
// terminal or wired into a watch loop.
//
// only narrows it to the cases whose script filename or case name contains that
// substring, matched without regard to case. Empty runs everything. A script
// with nothing matching is not printed at all -- with a hundred cases across
// seven files, a filter that still listed every file would not have filtered
// anything worth filtering.
func RunAllFixtures(dir string, out *os.File, only string) int {
	only = strings.ToLower(only)
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(out, "cannot read %s: %v\n", dir, err)
		return 1
	}

	var paths []string
	for _, de := range entries {
		if !de.IsDir() && strings.EqualFold(filepath.Ext(de.Name()), ".js") {
			paths = append(paths, filepath.Join(dir, de.Name()))
		}
	}
	sort.Strings(paths)

	if len(paths) == 0 {
		fmt.Fprintf(out, "no scripts in %s\n", dir)
		return 0
	}

	totalPass, totalFail, skipped := 0, 0, 0
	matched := false

	for _, path := range paths {
		name := filepath.Base(path)

		cases, err := LoadFixtures(path)
		if err != nil {
			fmt.Fprintf(out, "%s\n  FAIL  fixtures: %v\n\n", name, err)
			totalFail++
			continue
		}
		if len(cases) == 0 {
			skipped++
			continue
		}
		if cases = selectCases(cases, name, only); len(cases) == 0 {
			continue
		}
		matched = true

		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(out, "%s\n  FAIL  %v\n\n", name, err)
			totalFail++
			continue
		}
		s, err := Compile(idFor(path), path, string(src))
		if err != nil {
			// Every case counts as failed, so a script that will not compile cannot
			// be mistaken for one with nothing to run.
			fmt.Fprintf(out, "%s\n  FAIL  %v\n\n", name, err)
			totalFail += len(cases)
			continue
		}

		var b strings.Builder
		pass, fail := Report(&b, RunFixtures(s, cases), only != "")
		totalPass += pass
		totalFail += fail

		fmt.Fprintf(out, "%s  (%d/%d)\n%s\n", name, pass, pass+fail, b.String())
	}

	// Nothing matching is a typo in the filter, not a green run. Saying "0
	// passed, 0 failed" and exiting zero would read as success.
	if only != "" && !matched {
		fmt.Fprintf(out, "nothing matches %q\n", only)
		return 1
	}

	fmt.Fprintf(out, "%d passed, %d failed", totalPass, totalFail)
	if skipped > 0 {
		fmt.Fprintf(out, ", %d script(s) with no fixtures", skipped)
	}
	fmt.Fprintln(out)

	return totalFail
}

// selectCases applies the -only filter. A match on the script's filename keeps
// every case in it, so "-only java" is "run that script"; otherwise the cases
// are matched one at a time, so "-only static" is "run that one case, wherever
// it lives".
func selectCases(cases []Case, scriptName, only string) []Case {
	if only == "" || strings.Contains(strings.ToLower(scriptName), only) {
		return cases
	}
	var keep []Case
	for _, c := range cases {
		if strings.Contains(strings.ToLower(c.Name), only) {
			keep = append(keep, c)
		}
	}
	return keep
}
