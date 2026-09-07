//go:build windows

package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rodrigocfd/windigo/ui"
	"github.com/rodrigocfd/windigo/win"

	"github.com/hulaun/quick-tools/internal/fuzzy"
	"github.com/hulaun/quick-tools/internal/macro"
	"github.com/hulaun/quick-tools/internal/winapi"
)

// macroStore is the subset of macro.Store the palette needs.
type macroStore interface {
	Load() ([]macro.Macro, error)
	Changed() bool
	Path() string
	Add(macro.Macro) (int, error)
	SetName(index int, name string) error
	SetSteps(index int, steps []macro.Step) error
	Delete(index int) error
}

// recordTimeout stops a recording nobody stopped.
//
// A keyboard hook is in front of every key on the machine while it is
// installed, and a resident app that leaves one there because the user was
// called away mid-recording is not something to ship. Two minutes is far longer
// than any edit worth recording and short enough that the forgotten case is
// bounded.
const recordTimeout = 2 * time.Minute

// undoArmWindow is how long after a replay the palette keeps watching for
// Ctrl+Z. Long enough to look at what happened and decide, short enough that it
// is obviously connected to the macro that just ran.
const undoArmWindow = 20 * time.Second

// recording is a macro being captured.
//
// Every field is read and written on the UI thread only: the keyboard hook is
// dispatched by the thread that installed it, and that is the same thread the
// window procedure runs on. That is what makes this lock-free, and it is the
// reason InstallKeyHook says it must be called from there.
type recording struct {
	hook *winapi.KeyHook
	rec  macro.Recorder

	// index is the position in macros.json being recorded into, and isNew says
	// this recording created it -- which decides whether an empty result is
	// discarded and whether the name box opens afterwards.
	index int
	isNew bool

	// The combination that ends the recording, taken from the palette's own
	// hotkey. Reusing it means there is no second key to learn, and the hook
	// swallows it so the palette does not also try to open.
	stopMods uint32
	stopVK   uint16

	// stopping is set the moment the stop key goes down, so the events between
	// then and the window procedure catching up are not recorded.
	stopping bool

	timer *time.Timer
}

// undoArm is the one-shot watch for Ctrl+Z after a replay.
//
// It exists only for macros that make more than one change. A macro whose edit
// is a single undo -- which is most of them, including the one this feature was
// asked for -- needs nothing from us: the user's own Ctrl+Z already does the
// right thing, and installing a hook to watch it happen would be theatre.
type undoArm struct {
	hook  *winapi.KeyHook
	mods  macro.Modifiers
	depth int
	gap   time.Duration
	timer *time.Timer
}

// macroRow is one row in the macros list.
type macroRow struct {
	macro.Macro
}

// reloadMacros rebuilds the macros index from macros.json.
//
// Must run on the UI thread: it replaces the index the window procedure reads.
func (p *Palette) reloadMacros() {
	macros, err := p.macros.Load()
	p.macrosErr = err

	rows := make([]macroRow, 0, len(macros))
	for _, m := range macros {
		rows = append(rows, macroRow{Macro: m})
	}

	// Group first, then name, so a file written in recording order reads as a
	// filed list -- the same rule as places.
	sort.SliceStable(rows, func(i, j int) bool {
		gi, gj := strings.ToLower(rows[i].Group), strings.ToLower(rows[j].Group)
		if gi != gj {
			return gi < gj
		}
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})

	items := make([]fuzzy.Item, 0, len(rows))
	for _, r := range rows {
		// The steps are context, not a name: searching "ctrl" should not outrank a
		// macro actually called that, but a macro whose only distinguishing feature
		// is that it presses Home should still be findable.
		items = append(items, fuzzy.Item{
			ID:    macroID(r),
			Name:  r.Name,
			Group: r.Group,
			Tags:  []string{macro.Format(r.Steps)},
			Data:  r,
		})
	}
	p.indexes[modeMacros] = fuzzy.New(items)

	// Only when this mode is the one on screen -- refiltering a list that is not
	// showing drops its highlight for no reason the user can see.
	if p.shown && p.mode == modeMacros {
		p.refilterKeeping(p.currentID())
	}
}

// macroID identifies a row across a reload. The file position is part of it
// because two macros may legitimately be called the same thing.
func macroID(r macroRow) string {
	return strconv.Itoa(r.Index) + "|" + r.Name
}

// macroLabel renders a macros row: "SQL: Quote the third word".
func macroLabel(it fuzzy.Item) string {
	if it.Group == "" {
		return it.Name
	}
	return it.Group + ": " + it.Name
}

func (p *Palette) selectedMacro() (macroRow, bool) {
	it, ok := p.selected()
	if !ok {
		return macroRow{}, false
	}
	r, ok := it.Data.(macroRow)
	return r, ok
}

// selectByMacroIndex highlights the row for a given position in macros.json.
// The list is sorted for reading, so the file's order is not the screen's.
func (p *Palette) selectByMacroIndex(idx int) {
	for i, it := range p.visible {
		if r, ok := it.Data.(macroRow); ok && r.Index == idx {
			p.setSelection(i)
			return
		}
	}
}

// showMacro fills the editor pane with what the highlighted macro will do.
//
// The steps are shown in full rather than summarised, because a macro is about
// to synthesise keystrokes into a window that is not ours and the only
// reasonable answer to "what is this going to do" is the list itself.
func (p *Palette) showMacro() {
	r, ok := p.selectedMacro()
	if !ok {
		p.setEditorText(p.macroEmptyMessage())
		return
	}

	var b strings.Builder
	if len(r.Steps) == 0 {
		b.WriteString("nothing recorded yet.\r\n\r\nCtrl+R records into this macro.")
	} else {
		b.WriteString(macro.Format(r.Steps))
		b.WriteString("\n\n")
		b.WriteString(describeUndo(r.Macro, p.cfg.MacroUndo))
	}
	p.setEditorText(b.String())
}

// describeUndo says what Ctrl+Z will do after this macro runs, because the
// answer is not always "one press" and the difference is worth knowing before
// running it rather than after.
//
// arm is the macroUndo setting. It is passed in rather than read here so that
// the pane cannot promise something the code will not do: below a depth of two
// the watch is never armed, and with the setting off it is never armed at all.
func describeUndo(m macro.Macro, arm bool) string {
	switch n := m.UndoDepth(); {
	case n == 0:
		return "-- changes nothing, so there is nothing to undo"
	case n == 1:
		return "-- one Ctrl+Z undoes it"
	case arm:
		return fmt.Sprintf("-- %d changes; the first Ctrl+Z after it runs undoes all %d", n, n)
	default:
		return fmt.Sprintf("-- %d changes, so %d Ctrl+Z presses undo it", n, n)
	}
}

func (p *Palette) macroEmptyMessage() string {
	if p.macrosErr != nil {
		return "could not read " + p.macros.Path() + ": " + p.macrosErr.Error()
	}
	return "no macros yet.\r\n\r\nPut the caret where the edit starts, press Alt+D, " +
		"do the edit once, then press " + p.cfg.Hotkey + " to stop recording."
}

// applyMacro is Enter in macros mode: hide, hand focus back, and replay.
func (p *Palette) applyMacro() {
	r, ok := p.selectedMacro()
	if !ok {
		return
	}
	if len(r.Steps) == 0 {
		p.setEditorText("nothing recorded yet -- Ctrl+R records into this macro.")
		return
	}

	steps := make([]winapi.KeyStep, len(r.Steps))
	for i, s := range r.Steps {
		steps[i] = winapi.KeyStep{Text: s.Text, Mods: s.Mods, VK: s.VK}
	}
	gap := time.Duration(r.Gap()) * time.Millisecond
	depth := r.UndoDepth()

	// Take the handle before hiding: everything after this runs on a worker.
	h := p.wnd.Hwnd()
	p.hide(true)

	// Off the UI thread. A twenty-step macro at the default gap is most of a
	// fifth of a second, and blocking the message loop for that long would stop
	// the window answering the paint that hiding it just caused.
	go func() {
		winapi.SendKeys(steps, gap)
		// The gap travels with the result: a macro slowed down because the target
		// drops fast input needs its undo presses slowed down for the same reason.
		h.PostMessage(wmMacroDone, win.WPARAM(depth), win.LPARAM(r.Gap()))
	}()
}

// createMacro is Alt+D in macros mode: make an entry and start recording into
// it straight away.
//
// Recording immediately rather than making an empty macro and waiting is the
// whole flow: you press Alt+D because you are about to do the thing once.
// Naming happens afterwards, when there is something to name.
func (p *Palette) createMacro() {
	name := strings.TrimSpace(p.search.Text())
	if name == "" {
		name = "New macro"
	}

	idx, err := p.macros.Add(macro.Macro{Name: name})
	if err != nil {
		p.fatal("Could not create the macro", err)
		return
	}

	p.search.SetText("")
	p.reloadMacros()
	p.startRecording(idx, true)
}

// recordIntoSelected is Ctrl+R: record over the highlighted macro, keeping its
// name, group and any tuning that has been done to it.
func (p *Palette) recordIntoSelected() {
	r, ok := p.selectedMacro()
	if !ok {
		return
	}
	p.startRecording(r.Index, false)
}

// startRecording hides the palette, hands focus back, and puts a keyboard hook
// in front of every key until the stop combination comes round.
func (p *Palette) startRecording(index int, isNew bool) {
	if p.rec != nil {
		return
	}
	p.disarmUndo()

	mods, vk, err := winapi.ParseHotkey(p.cfg.Hotkey)
	if err != nil {
		// Should be impossible -- the same string registered successfully at
		// startup -- but a recording with no way to stop it is not something to
		// start on the strength of that.
		p.fatal("Cannot record without a stop key", err)
		return
	}

	rec := &recording{index: index, isNew: isNew, stopMods: mods, stopVK: uint16(vk)}

	// Focus goes back to whatever the user was editing before the keys start
	// being watched, so the first thing recorded is not the palette closing.
	p.hide(true)

	hook, err := winapi.InstallKeyHook(func(e winapi.KeyEvent) bool {
		return p.onRecordedKey(rec, e)
	})
	if err != nil {
		p.fatal("Could not start recording", err)
		if isNew {
			p.macros.Delete(index)
			p.reloadMacros()
		}
		return
	}
	rec.hook = hook
	p.rec = rec

	h := p.wnd.Hwnd()
	rec.timer = time.AfterFunc(recordTimeout, func() {
		h.PostMessage(wmMacroStop, 0, 0)
	})

	p.tray.setTip(h, "quick-tools -- recording ("+p.cfg.Hotkey+" to stop)")
	p.tray.notify(h, "Recording a macro",
		"Do the edit once, then press "+p.cfg.Hotkey+" to stop.")
}

// onRecordedKey is the hook callback, on the UI thread. It returns true to
// swallow the key.
func (p *Palette) onRecordedKey(rec *recording, e winapi.KeyEvent) bool {
	if rec.stopping {
		// The stop key's own release, and nothing else. Swallowing the modifier
		// releases as well would leave the application the user was typing into
		// believing Ctrl and Alt are still held.
		return !e.Down && e.VK == rec.stopVK
	}

	// Held() is the state before this event, which for a chord is exactly right:
	// the modifiers went down first.
	if e.Down && e.VK == rec.stopVK && rec.rec.Held().Mask() == rec.stopMods {
		rec.stopping = true
		p.wnd.Hwnd().PostMessage(wmMacroStop, 0, 0)
		// Swallowed, which is also what stops the registered hotkey firing: a
		// low-level hook runs before hotkey dispatch, so the palette does not
		// additionally try to open on the key that just ended the recording.
		return true
	}

	rec.rec.Add(macro.Event{VK: e.VK, Down: e.Down, Text: e.Text})
	return false
}

// cancelRecording takes the hook down and keeps whatever was captured out of
// the file.
//
// It is what shutdown uses. stopRecording cannot be: it saves, reloads and
// brings the window back, and doing any of that from WM_DESTROY would be acting
// on a window that is being torn down. Everything that matters at that point is
// the hook -- one left installed is a callback in front of every key on the
// machine, pointing into a process on its way out.
func (p *Palette) cancelRecording() {
	rec := p.rec
	if rec == nil {
		return
	}
	p.rec = nil
	if rec.timer != nil {
		rec.timer.Stop()
	}
	rec.hook.Remove()
}

// stopRecording ends a recording and writes what was captured.
func (p *Palette) stopRecording() {
	rec := p.rec
	if rec == nil {
		return
	}
	p.rec = nil

	if rec.timer != nil {
		rec.timer.Stop()
	}
	rec.hook.Remove()

	h := p.wnd.Hwnd()
	p.tray.setTip(h, "quick-tools -- "+p.cfg.Hotkey)

	steps := rec.rec.Steps()

	// Nothing recorded. A macro that was created for this recording goes away
	// again rather than being left as an entry that does nothing; an existing one
	// keeps the steps it had, because overwriting them with nothing is never what
	// a missed recording meant.
	if len(steps) == 0 {
		if rec.isNew {
			if err := p.macros.Delete(rec.index); err != nil {
				p.fatal("Could not discard the empty macro", err)
			}
		}
		p.reloadMacros()
		p.showMacrosTab(-1, false)
		return
	}

	if err := p.macros.SetSteps(rec.index, steps); err != nil {
		p.fatal("Could not save the recording", err)
		return
	}
	p.reloadMacros()
	p.showMacrosTab(rec.index, rec.isNew)

	if rec.rec.Full() {
		p.setEditorText(macro.Format(steps) + "\n\n-- stopped at " +
			strconv.Itoa(macro.MaxSteps) + " steps, which is the cap")
	}
}

// showMacrosTab brings the palette back after a recording, on the macros tab
// with the recorded entry highlighted.
func (p *Palette) showMacrosTab(index int, rename bool) {
	p.show()
	p.setMode(modeMacros)
	if index >= 0 {
		p.selectByMacroIndex(index)
	}
	if rename {
		p.beginRename()
	}
}

// deleteMacro is Del in macros mode.
//
// A macro is an entry in a file and nothing else -- there is no target it
// points at and no document it belongs to -- so unlike a note there is nothing
// to be careful about beyond the entry itself. It still asks, because the
// recording it took to make is not recoverable.
func (p *Palette) deleteMacro() {
	r, ok := p.selectedMacro()
	if !ok {
		return
	}
	if !p.confirm("Delete macro",
		"Delete \""+r.Name+"\" from macros.json?\n\n"+
			strconv.Itoa(len(r.Steps))+" recorded steps. This cannot be undone.") {
		return
	}
	if err := p.macros.Delete(r.Index); err != nil {
		p.fatal("Could not delete the macro", err)
		return
	}

	at := p.selIdx
	p.reloadMacros()
	p.refilter()
	if at >= len(p.visible) {
		at = len(p.visible) - 1
	}
	if at >= 0 {
		p.setSelection(at)
	}
}

// commitMacroRename writes a macro's new name into macros.json.
func (p *Palette) commitMacroRename(st renameState, name string) {
	if name == "" || name == st.was {
		p.selectByMacroIndex(st.entryIdx)
		p.search.Hwnd().SetFocus()
		return
	}
	if err := p.macros.SetName(st.entryIdx, name); err != nil {
		p.fatal("Could not rename the macro", err)
		p.selectByMacroIndex(st.entryIdx)
		return
	}
	p.reloadMacros()
	p.refilter()
	p.selectByMacroIndex(st.entryIdx)
	p.search.Hwnd().SetFocus()
}

// The undo watch.
//
// After a replay that made more than one change, the next Ctrl+Z is caught and
// turned into as many as the macro needs -- so one press puts the line back,
// which is what anyone means by undoing a macro. It is one shot and it expires:
// any other key, twenty seconds, or opening the palette all call it off, and
// the key is passed through untouched once it has.
func (p *Palette) armUndo(depth int, gap time.Duration) {
	p.disarmUndo()
	if depth <= 1 || !p.cfg.MacroUndo || p.rec != nil {
		return
	}

	arm := &undoArm{depth: depth, gap: gap}
	hook, err := winapi.InstallKeyHook(func(e winapi.KeyEvent) bool {
		return p.onArmedKey(arm, e)
	})
	if err != nil {
		// Not worth interrupting for. The macro ran; the user simply has to press
		// Ctrl+Z the full number of times, which is what they would do without
		// this feature at all.
		return
	}
	arm.hook = hook
	p.undo = arm

	h := p.wnd.Hwnd()
	arm.timer = time.AfterFunc(undoArmWindow, func() {
		h.PostMessage(wmMacroDisarm, 0, 0)
	})
}

func (p *Palette) onArmedKey(arm *undoArm, e winapi.KeyEvent) bool {
	if macro.IsModifier(e.VK) {
		arm.mods = arm.mods.Track(e.VK, e.Down)
		return false
	}
	if !e.Down {
		return false
	}

	if e.VK == winapi.VKZ && arm.mods.Mask() == macro.ModCtrl {
		p.wnd.Hwnd().PostMessage(wmMacroUndo, 0, 0)
		// Swallowed and replaced. The replacement is sent from the window
		// procedure rather than from here: a hook callback holds up every key on
		// the machine for as long as it runs, and this one would be running for
		// depth times the gap.
		return true
	}

	// Anything else means the moment has passed. The key is passed through
	// untouched -- this watch never changes what a key does except the one it
	// was armed for.
	p.wnd.Hwnd().PostMessage(wmMacroDisarm, 0, 0)
	return false
}

// disarmUndo takes the watch down. Safe to call when there is none, which
// matters: it is called from the timer, from the hook, from the next replay and
// from the palette opening.
func (p *Palette) disarmUndo() {
	arm := p.undo
	if arm == nil {
		return
	}
	p.undo = nil
	if arm.timer != nil {
		arm.timer.Stop()
	}
	arm.hook.Remove()
}

// macroEvents wires the recording and replay messages into the window.
func (p *Palette) macroEvents() {
	p.wnd.On().Wm(wmMacroStop, func(_ ui.Wm) uintptr {
		p.stopRecording()
		return 0
	})

	// A replay has finished. Arming happens here rather than on the worker
	// because installing a hook binds it to the thread that pumps messages.
	p.wnd.On().Wm(wmMacroDone, func(m ui.Wm) uintptr {
		p.armUndo(int(m.WParam), time.Duration(m.LParam)*time.Millisecond)
		return 0
	})

	p.wnd.On().Wm(wmMacroUndo, func(_ ui.Wm) uintptr {
		arm := p.undo
		p.disarmUndo()
		if arm == nil {
			return 0
		}
		depth, gap := arm.depth, arm.gap
		go winapi.SendUndo(depth, gap)
		return 0
	})

	p.wnd.On().Wm(wmMacroDisarm, func(_ ui.Wm) uintptr {
		p.disarmUndo()
		return 0
	})
}
