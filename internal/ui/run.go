//go:build windows

package ui

import (
	"sync"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/transform"
)

// Private window messages. WM_APP and above belong to the application.
const (
	wmRunDone        = co.WM(0x8000 + 2) // a transform finished on a worker
	wmScriptsChanged = co.WM(0x8000 + 3) // the scripts folder changed on disk
)

// runResult is the outcome of one transform run.
type runResult struct {
	out   string
	err   error
	apply bool // the result should be put on the clipboard, not just previewed
}

// runner executes transforms off the UI thread.
//
// Built-in transforms are microseconds of string work and could safely run
// inline, but a user script cannot: it is arbitrary JavaScript, and running it
// on the UI thread would freeze the window for as long as it takes -- up to the
// two-second timeout for something with an accidental infinite loop. So every
// transform goes through here, script or not, and the window stays responsive.
type runner struct {
	mu      sync.Mutex
	seq     uint64
	results map[uint64]runResult

	// latest is the most recently started preview. Results from earlier previews
	// are discarded: with a keystroke starting a new run each time, an older and
	// slower one must not overwrite a newer answer.
	latest uint64
}

func newRunner() *runner {
	return &runner{results: make(map[uint64]runResult)}
}

// start launches a transform and returns the sequence number identifying it.
func (r *runner) start(hwnd win.HWND, t *transform.Transform, input string, apply bool) uint64 {
	r.mu.Lock()
	r.seq++
	seq := r.seq
	if !apply {
		r.latest = seq
	}
	r.mu.Unlock()

	go func() {
		out, err := t.Run(input)

		r.mu.Lock()
		r.results[seq] = runResult{out: out, err: err, apply: apply}
		r.mu.Unlock()

		// Hand the result back to the UI thread. Touching a control from this
		// goroutine would be a race against the window procedure.
		hwnd.PostMessage(wmRunDone, win.WPARAM(seq), 0)
	}()

	return seq
}

// take removes and returns a finished result.
func (r *runner) take(seq uint64) (runResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.results[seq]
	delete(r.results, seq)
	return res, ok
}

// isStalePreview reports whether a preview result has been superseded.
func (r *runner) isStalePreview(seq uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return seq != r.latest
}

// runEvents wires the worker results back into the window.
func (p *Palette) runEvents() {
	p.wnd.On().Wm(wmRunDone, func(m ui.Wm) uintptr {
		seq := uint64(m.WParam)
		res, ok := p.runner.take(seq)
		if !ok {
			return 0
		}

		if !res.apply {
			if p.runner.isStalePreview(seq) {
				return 0 // a newer preview has already started
			}
			if res.err != nil {
				p.preview.SetText(toCRLF("error: " + res.err.Error()))
			} else {
				p.preview.SetText(toCRLF(res.out))
			}
			return 0
		}

		p.finishApply(res)
		return 0
	})

	p.wnd.On().Wm(wmScriptsChanged, func(_ ui.Wm) uintptr {
		p.reloadScripts()
		return 0
	})
}
