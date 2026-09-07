//go:build windows

package ui

import "testing"

// TestStep pins the one decision in list navigation: a single step wraps and a
// jump clamps. Getting it the other way round is not a crash, it is a palette
// that throws you to the far end of a long list when you asked to move five
// rows -- which is exactly the position the jump exists to keep.
func TestStep(t *testing.T) {
	cases := []struct {
		name          string
		cur, delta, n int
		want          int
	}{
		{"down one", 2, 1, 10, 3},
		{"up one", 2, -1, 10, 1},

		{"down one from the end wraps to the top", 9, 1, 10, 0},
		{"up one from the top wraps to the end", 0, -1, 10, 9},

		{"a jump moves the whole way when there is room", 2, 5, 10, 7},
		{"a jump up moves the whole way when there is room", 7, -5, 10, 2},

		{"a jump past the end stops at the end", 8, 5, 10, 9},
		{"a jump past the top stops at the top", 2, -5, 10, 0},

		{"a jump from the end stays there", 9, 5, 10, 9},
		{"a jump from the top stays there", 0, -5, 10, 0},

		{"a jump longer than the list is still in range", 0, 5, 3, 2},
		{"one row: every move is a no-op", 0, 5, 1, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := step(c.cur, c.delta, c.n); got != c.want {
				t.Errorf("step(%d, %d, %d) = %d, want %d", c.cur, c.delta, c.n, got, c.want)
			}
		})
	}
}

// TestArrowStepIsOneWithoutCtrl checks the default without pressing anything.
// The Ctrl branch reads the real keyboard, so it cannot be pinned here -- that
// half is verified by hand.
func TestArrowStepIsOneWithoutCtrl(t *testing.T) {
	if ctrlDown() {
		t.Skip("Ctrl is physically held")
	}
	if got := arrowStep(); got != 1 {
		t.Errorf("arrowStep() = %d, want 1", got)
	}
}
