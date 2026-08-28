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
func RunAllFixtures(dir string, out *os.File) int {
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
		pass, fail := Report(&b, name, RunFixtures(s, cases))
		totalPass += pass
		totalFail += fail

		fmt.Fprintf(out, "%s  (%d/%d)\n%s\n", name, pass, pass+fail, b.String())
	}

	fmt.Fprintf(out, "%d passed, %d failed", totalPass, totalFail)
	if skipped > 0 {
		fmt.Fprintf(out, ", %d script(s) with no fixtures", skipped)
	}
	fmt.Fprintln(out)

	return totalFail
}
