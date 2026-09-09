//go:build windows

package ui

import (
	"context"
	"sync"

	"github.com/rodrigocfd/windigo/co"
	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/api"
	"github.com/hulaun/quick-tools/internal/transform"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// Private window messages. WM_APP and above belong to the application.
const (
	wmRunDone        = co.WM(0x8000 + 2) // a transform finished on a worker
	wmSourcesChanged = co.WM(0x8000 + 3) // the scripts, snippets or places changed
	wmSendDone       = co.WM(0x8000 + 5) // an HTTP request finished on a worker
	wmRoundCorners   = co.WM(0x8000 + 6) // repaint once the first paint is done

	// The macro tab. Recording and replaying both run away from the UI thread --
	// one inside a keyboard hook, the other on a worker -- and every decision
	// they lead to comes back here, because installing and removing a hook has
	// to happen on the thread that pumps messages.
	wmMacroStop   = co.WM(0x8000 + 6) // the stop key was pressed, or recording timed out
	wmMacroDone   = co.WM(0x8000 + 7) // a replay finished; WPARAM is its undo depth
	wmMacroUndo   = co.WM(0x8000 + 8) // the armed Ctrl+Z was caught
	wmMacroDisarm = co.WM(0x8000 + 9) // stop watching for it
)

// What a run is for.
//
// A preview is thrown away as soon as a newer one starts; an apply ends the
// interaction; a chain step feeds its result back in as the input to the next
// transform and leaves the palette open.
const (
	runPreview = iota
	runApply
	runChain
)

// runResult is the outcome of one transform run.
type runResult struct {
	name string // the transform's display name, for the chain label
	out  string
	err  error
	kind int
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
func (r *runner) start(hwnd win.HWND, t *transform.Transform, input string, kind int) uint64 {
	r.mu.Lock()
	r.seq++
	seq := r.seq
	if kind == runPreview {
		r.latest = seq
	}
	r.mu.Unlock()

	go func() {
		out, err := t.Run(input)

		r.mu.Lock()
		r.results[seq] = runResult{name: t.Name, out: out, err: err, kind: kind}
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

// sendResult is the outcome of one HTTP request.
type sendResult struct {
	resp api.Response
	err  error

	// hook is the request's post-response block, carried through so the UI
	// thread can run it. It runs there rather than on the worker because it
	// writes to the environment, which the window reads -- and it is a
	// two-second budget of string work, not a network wait.
	hook string
}

// sender performs HTTP requests off the UI thread.
//
// One at a time, unlike the transform runner: a request has a visible cost and
// a visible answer, and firing a second while the first is in flight would
// leave two responses racing for one pane. Starting a new send cancels the one
// before it, which is also what Esc does.
type sender struct {
	mu      sync.Mutex
	seq     uint64
	results map[uint64]sendResult

	// stop cancels the request currently in flight, if any. Holding the func
	// rather than the context is what lets Esc reach across from the UI thread.
	stop func()
}

func newSender() *sender {
	return &sender{results: make(map[uint64]sendResult)}
}

// start launches a request, cancelling whatever was already running.
func (s *sender) start(hwnd win.HWND, req api.Request) uint64 {
	s.mu.Lock()
	if s.stop != nil {
		s.stop()
	}
	s.seq++
	seq := s.seq
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.mu.Unlock()

	go func() {
		resp, err := api.Send(ctx, req)
		cancel() // release the context's resources whatever happened

		s.mu.Lock()
		// A result whose send has been superseded is dropped here rather than in
		// the window: the pane belongs to the newest request, and an older reply
		// arriving late must not overwrite it.
		current := seq == s.seq
		if current {
			s.results[seq] = sendResult{resp: resp, err: err, hook: req.Hook}
			s.stop = nil
		}
		s.mu.Unlock()

		if current {
			hwnd.PostMessage(wmSendDone, win.WPARAM(seq), 0)
		}
	}()

	return seq
}

// cancel stops the request in flight and reports whether there was one.
func (s *sender) cancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop == nil {
		return false
	}
	s.stop()
	s.stop = nil
	// Bump the sequence so the cancelled request's result is dropped when it
	// lands, rather than arriving as an error a moment after Esc.
	s.seq++
	return true
}

// inFlight reports whether a request is currently running.
func (s *sender) inFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop != nil
}

func (s *sender) take(seq uint64) (sendResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, ok := s.results[seq]
	delete(s.results, seq)
	return res, ok
}

// runEvents wires the worker results back into the window.
func (p *Palette) runEvents() {
	p.wnd.On().Wm(wmRunDone, func(m ui.Wm) uintptr {
		seq := uint64(m.WParam)
		res, ok := p.runner.take(seq)
		if !ok {
			return 0
		}

		switch res.kind {
		case runPreview:
			if p.runner.isStalePreview(seq) {
				return 0 // a newer preview has already started
			}
			if res.err != nil {
				p.preview.SetText(toCRLF("error: " + res.err.Error()))
			} else {
				p.preview.SetText(toCRLF(res.out))
			}
		case runChain:
			p.finishChain(res)
		default:
			p.finishApply(res)
		}
		return 0
	})

	p.wnd.On().Wm(wmSendDone, func(m ui.Wm) uintptr {
		if res, ok := p.sender.take(uint64(m.WParam)); ok {
			p.finishSend(res)
		}
		return 0
	})

	p.wnd.On().Wm(wmSourcesChanged, func(_ ui.Wm) uintptr {
		p.reloadScripts()
		p.reloadNotes()
		p.reloadMacros()
		p.reloadRequests()
		p.reloadEnv()
		return 0
	})

	// The rounded corners are not real until this runs -- see winapi.RedrawAll
	// for what is stale and why. It is posted rather than called, because a
	// redraw issued in the same turn of the message pump as the first paint is
	// folded into that paint and misses exactly the pixels it was meant to fix.
	// Arriving as a message is what puts it after.
	p.wnd.On().Wm(wmRoundCorners, func(_ ui.Wm) uintptr {
		winapi.RedrawAll(uintptr(p.wnd.Hwnd()))
		return 0
	})
}
