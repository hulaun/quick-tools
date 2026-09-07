//go:build windows

package ui

import (
	"strings"

	"github.com/hulaun/quick-tools/internal/transform"
)

// Chaining transforms.
//
// One transform is often not the whole job: a Java object becomes JSON, and
// then that JSON becomes an insert statement. Tab runs the highlighted
// transform and feeds its result back in as the input to the next one, leaving
// the palette open with the search box cleared and focused. Enter is still the
// end of the interaction -- it runs the highlighted transform against whatever
// the chain has built up and puts that on the clipboard.
//
// Tab rather than Space, which is what this was before. Space is a printable
// character, so binding it cost the search box the ability to type one without
// Shift held -- an odd tax on a text field, to save a key that was otherwise
// doing nothing. Tab also reads the same as it does in the other two tabs:
// carry on from what is highlighted.
//
// Nothing is written to the clipboard until Enter, so an abandoned chain
// changes nothing: Esc leaves the clipboard exactly as it was found.

// chainStep is one applied transform, and the input it was applied to. Keeping
// the input is what makes a step undoable -- there is no inverse transform to
// call, so the only way back is to remember what was there before.
type chainStep struct {
	name   string
	before string
}

// chainStep runs the highlighted transform as a step rather than as the final
// answer. The work happens on the worker like every other run; finishChain
// picks it up.
func (p *Palette) chainStep() {
	if p.mode != modeTransforms {
		return
	}
	it, ok := p.selected()
	if !ok {
		return
	}
	t, ok := it.Data.(*transform.Transform)
	if !ok {
		return
	}
	p.runner.start(p.wnd.Hwnd(), t, p.input, runChain)
}

// finishChain accepts a step's result as the new working input.
func (p *Palette) finishChain(res runResult) {
	if res.err != nil {
		// Leave the chain where it was. A step that failed has produced nothing
		// worth carrying forward, and the error is the only useful thing to show.
		p.preview.SetText(toCRLF("error: " + res.err.Error()))
		return
	}

	p.chain = append(p.chain, chainStep{name: res.name, before: p.input})
	p.input = res.out
	p.updateChainLabel()

	// Clear the query so the next transform is picked from the full list. The
	// preview then re-runs against the new input, so the pane shows what the
	// next step would produce, not what the last one did.
	p.search.SetText("")
	p.refilter()
	p.search.Hwnd().SetFocus()
}

// popChain undoes the last step. Backspace on an empty search box is where it
// lives, so correcting a wrong turn is the same key as correcting a typo. Tab
// adds a step and Backspace takes one away, which is as close to a pair as two
// keys that are not each other's opposite can get.
func (p *Palette) popChain() bool {
	if len(p.chain) == 0 {
		return false
	}
	last := p.chain[len(p.chain)-1]
	p.chain = p.chain[:len(p.chain)-1]
	p.input = last.before
	p.updateChainLabel()
	p.refilter()
	return true
}

// resetChain drops the chain and is called every time the palette opens: each
// invocation starts from what is on the clipboard now.
func (p *Palette) resetChain() {
	p.chain = nil
	// updateStripLabel, not updateChainLabel: the strip beside the tabs is
	// shared, and this runs on every show() -- including one that lands on the
	// API tab, where writing an empty chain into it would wipe the environment
	// name the tab had put there.
	p.updateStripLabel()
}

// updateChainLabel writes the steps into the strip beside the tabs.
//
// The label is the only sign that the input being previewed is no longer the
// clipboard, so it is worth the space it takes: without it a chained palette
// looks like a palette showing the wrong preview.
func (p *Palette) updateChainLabel() {
	if p.chainLabel == nil {
		return
	}
	p.chainLabel.Hwnd().SetWindowText(chainText(p.chain, chainLabelChars))
	p.chainLabel.Hwnd().InvalidateRect(nil, true)
}

// chainLabelChars is roughly how many characters fit in the strip. Steps are
// dropped from the front when they do not, because the recent ones are the
// ones being reasoned about.
const chainLabelChars = 46

func chainText(steps []chainStep, budget int) string {
	if len(steps) == 0 {
		return ""
	}

	names := make([]string, 0, len(steps))
	for _, s := range steps {
		names = append(names, s.name)
	}

	const sep = " > "
	for first := 0; first < len(names); first++ {
		text := strings.Join(names[first:], sep) + " >"
		if first > 0 {
			text = "... " + text
		}
		if len(text) <= budget || first == len(names)-1 {
			return text
		}
	}
	return ""
}
