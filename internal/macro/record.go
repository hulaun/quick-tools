package macro

import "strings"

// Turning a keyboard hook's output into a macro.
//
// The raw stream is not what anyone means by "what I just did". Pressing
// Shift+Alt+Right is six events -- three keys down and three up, in whichever
// order the fingers happened to leave the keyboard -- and typing "foo" is three
// separate presses. Replaying that verbatim would work, but nobody could read
// it, correct it or reason about what it will undo.
//
// So the stream is folded into two kinds of step: a chord, which is one
// non-modifier key plus whatever was held at the moment it went down, and a run
// of literal text. Everything else -- the modifier presses themselves, every
// key release, and the stop key -- contributes nothing and is dropped.

// Event is one key transition as the hook saw it.
type Event struct {
	VK   uint16
	Down bool

	// Text is what this key would put in a document given the modifiers held at
	// the time, or "" if it would put nothing there. Working that out needs the
	// active keyboard layout, so the hook fills it in; deciding what to do with
	// it is this package's job.
	Text string
}

// MaxSteps caps a recording.
//
// A recorder left running -- the stop key missed, the user called away -- would
// otherwise grow without bound and produce a macro that is not a macro. The
// limit is high enough that no real edit reaches it and low enough that the
// runaway case stops being interesting.
const MaxSteps = 500

// Recorder folds a stream of key events into steps.
//
// It is a plain value with no locking. In the app it is driven from the
// keyboard hook, which Windows dispatches on the thread that installed it --
// the UI thread -- so every call arrives on the same goroutine as every other.
type Recorder struct {
	held  Modifiers
	steps []Step

	// full records that MaxSteps was reached, so the caller can say why the
	// recording stops growing rather than appearing to freeze.
	full bool
}

// Add feeds one event in.
func (r *Recorder) Add(e Event) {
	if IsModifier(e.VK) {
		r.held = r.held.Track(e.VK, e.Down)
		return
	}
	if !e.Down || r.full {
		// A key release changes nothing: the chord was decided when the key went
		// down, and replaying press-and-release is what SendKeys does anyway.
		return
	}

	// A key that produces a character with no command modifier held is typing,
	// and typing is recorded as the characters it produced rather than as the
	// keys that produced them. That is what makes a macro survive a different
	// keyboard layout: the layout is applied once, here, at record time.
	//
	// Shift is not a command modifier for this purpose -- it is how a capital
	// letter is made, and the character already accounts for it.
	if !r.held.Any(ModCtrl|ModAlt|ModWin) && printable(e.Text) {
		r.appendText(e.Text)
		return
	}
	r.append(Step{Mods: r.held.Mask(), VK: e.VK})
}

// appendText merges into the previous step when that step is also text, so a
// typed word is one step rather than one per letter. Anything in between --
// even a bare arrow key -- ends the run, which is the same rule an editor uses
// to decide what one Ctrl+Z takes back.
func (r *Recorder) appendText(text string) {
	if n := len(r.steps); n > 0 && r.steps[n-1].IsText() {
		r.steps[n-1].Text += text
		return
	}
	r.append(Step{Text: text})
}

func (r *Recorder) append(s Step) {
	if len(r.steps) >= MaxSteps {
		r.full = true
		return
	}
	r.steps = append(r.steps, s)
}

// Steps is what has been recorded so far.
func (r *Recorder) Steps() []Step { return r.steps }

// Full reports whether the cap was hit and events are being dropped.
func (r *Recorder) Full() bool { return r.full }

// Held is the modifier mask currently down, which the caller needs in order to
// recognise its own stop combination.
func (r *Recorder) Held() Modifiers { return r.held }

// printable reports whether text is something worth putting in a document.
//
// ToUnicode answers for keys that produce control characters too: Enter comes
// back as "\r", Escape as "\x1b", Ctrl+H as a backspace. Those are keys, not
// text, and recording them as text would replay Enter as a literal carriage
// return -- which is not the same thing in a great many applications.
func printable(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return !strings.ContainsRune(text, '�')
}
