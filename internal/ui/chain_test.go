//go:build windows

package ui

import "testing"

func steps(names ...string) []chainStep {
	out := make([]chainStep, 0, len(names))
	for _, n := range names {
		out = append(out, chainStep{name: n})
	}
	return out
}

func TestChainTextIsEmptyWithNoSteps(t *testing.T) {
	if got := chainText(nil, 46); got != "" {
		t.Errorf("chainText(nil) = %q, want empty", got)
	}
}

func TestChainTextReadsAsUnfinished(t *testing.T) {
	// The trailing arrow is the point: the chain is waiting for the next pick.
	if got := chainText(steps("Java to JSON"), 46); got != "Java to JSON >" {
		t.Errorf("chainText = %q", got)
	}
	if got := chainText(steps("Java to JSON", "Pretty JSON"), 46); got != "Java to JSON > Pretty JSON >" {
		t.Errorf("chainText = %q", got)
	}
}

func TestChainTextDropsOldestStepsFirst(t *testing.T) {
	got := chainText(steps("Java to JSON", "Pretty JSON", "JSON to insert"), 30)
	if got != "... Pretty JSON > JSON to insert >" && got != "... JSON to insert >" {
		t.Errorf("chainText = %q, want the recent steps kept", got)
	}
	if len(got) > 0 && got[0] != '.' {
		t.Errorf("chainText = %q, want it marked as truncated", got)
	}
}

func TestChainTextAlwaysKeepsTheLastStep(t *testing.T) {
	// Even a budget nothing fits in must still say what just happened.
	got := chainText(steps("a very long transform name indeed", "another long one"), 5)
	if got != "... another long one >" {
		t.Errorf("chainText = %q", got)
	}
}
