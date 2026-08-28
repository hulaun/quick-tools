# quick-tools

A resident Win32 command palette for Windows, written in Go.

Copy text → press a hotkey → pick a transform → the result is on the clipboard
(and optionally pasted for you). Plus a folder-organised snippet store to
replace Win+V for configs and logins.

---

## Why this exists

The user repeatedly does three things by hand:

1. **Reshaping copied text** — a Java object into JSON, `\` paths into `/`,
   switching between camel/upper/lower case.
2. **Storing configs and logins in Win+V** — fast, but a flat list with no
   folders or tabs, so it cannot be maintained.
3. Wanting a notepad smaller and faster than Notepad.

**The central design decision:** the "opens in under 300ms" requirement is met
by *residency*, not by language choice. The app starts once at login, sits in
the tray with its palette window already created but hidden, and the hotkey just
calls `ShowWindow()`. Perceived open time is one frame. Cold start is paid once,
at boot. Do not redesign around per-invocation startup cost — that fight is
already won.

---

## Division of work — IMPORTANT

The user is building this partly to learn. **Six scripts are reserved for
them**, laddered by difficulty:

1. `scripts/properties-to-json.js` — line parsing, dotted-key nesting
2. `scripts/query-string-to-json.js` — decoding, repeated keys → arrays
3. `scripts/js-object-to-json.js` — recursive descent over a bracketed grammar
4. `scripts/curl-to-json.js` — a real tokeniser, scan separated from interpret
5. `scripts/java-to-json.js` — depth tracking, ambiguity policy
6. `scripts/stacktrace-to-json.js` — recursion with no brackets to guide it

None duplicates a built-in, so nothing is broken while they are unwritten —
each is a capability the tool does not have yet. That was deliberate: handing
back a working built-in as a stub would have cost the user a feature.

**Do not implement these.** The scaffolding is finished as of M2:
- stubs with the contract wired up and hints, but no parser
- `scripts/README.md` — the grammar, type rules, and the ambiguity policy
- `scripts/*.test.json` — 15 and 13 cases, currently red
- `scripts/csv-line-to-json.js` — a complete worked parser to copy the shape of
- `quicktools.exe -test-scripts` — the red/green loop

If asked to "finish the project", finish everything except these two and say so.
Do not "helpfully" fill in a stub.

The same instinct generalises: when a task has a genuinely interesting
algorithmic core (parser, scheduler, diff, solver), ask before implementing it.
Build all the mechanical parts fully — those were never the point.

---

## Current status

| Milestone | State |
|---|---|
| **M0** — de-risk spike | Done and confirmed working by the user in a real app. |
| **M1** — palette UI + built-in transforms | Done. Dark themed palette, fuzzy search, live preview. |
| **M2** — goja scripting + handoff | Done. Engine, hot-reload, fixture runner, stubs and spec all in place. |
| **M3** — snippets | Done. Folder tree, searchable in the same palette, Enter copies. |
| **M4** — packaging | Not started. |

### What works today

- `internal/winapi` — hotkey registration, clipboard get/set, focus restore,
  synthesised Ctrl+V. All pure `syscall`, no cgo.
- `internal/transform` — 40 built-ins across Case, Path, JSON, Encoding, Lines,
  Misc. `go test ./...` is green.
- `cmd/m0` — the de-risk spike. `-selftest` checks the clipboard round trip
  unattended (and restores whatever the user had on the clipboard).
- `internal/ui` — the palette: dark theme, Segoe UI, DWM rounded corners,
  accent highlight, live preview. `internal/fuzzy` ranks entries.
- `cmd/quicktools` — the real app.
- Tray icon with a context menu: open, toggle auto-paste (persisted), quit.
  The icon is generated as a 32x32 ICO and `go:embed`ed in `internal/ui`.
- `internal/script` — goja engine with a 2s interrupt, hot-reloading loader,
  and the `.test.json` fixture runner behind `quicktools.exe -test-scripts`.
- `internal/snippet` — walks `snippets/`, one entry per text file, folder as
  the group. Registered as Transforms whose Run ignores input and returns the
  file, so the fuzzy index, preview, paste path and auto-paste all work
  unchanged.
- Transforms run on a worker goroutine and post results back via `wmRunDone`;
  stale preview results are discarded. A slow script cannot freeze the window.

### Known open questions

- Windows 11 hides newly registered tray icons in the overflow ("^") by
  default. The icon is there and works; the user drags it out to pin it. Not a
  bug and nothing to fix in code.
- A flash still remains at the exact moment the list scrollbar appears or
  disappears. Much reduced by WS_EX_COMPOSITED but not gone. The user has
  accepted it for now. The remaining option, if it ever matters, is to keep the
  scrollbar permanently visible so the transition never happens.
- The search box cue banner (placeholder text) is set but does not appear.
  Cosmetic, unexplained, not yet chased.
- Elevated target windows will reject synthesised input (Windows UIPI). This is
  an OS rule, not a bug — do not try to defeat it.
- Nothing has been pushed to the remote yet. The local repo has commits; ask
  before pushing.

---

## Build and test

Go lives at `C:\Program Files\Go`. In Git Bash:

```bash
export PATH="$PATH:/c/Program Files/Go/bin"

go test ./...                      # all tests
go vet ./...                       # see "known vet finding" below
# The exe is built at the repo root on purpose, so scripts/ and snippets/ sit
# beside it -- the same layout a release ships in. Building into bin/ makes the
# app read a different copy of scripts/ than the one being edited.
go build -o quicktools.exe ./cmd/quicktools
./quicktools.exe                   # then press Ctrl+Alt+Space
./quicktools.exe -test-scripts     # run the script fixtures

go build -o bin/m0.exe ./cmd/m0    # the spike
./bin/m0.exe -selftest             # unattended clipboard check
```

Final builds use `-ldflags "-H windowsgui -s -w"` so there is no console window.

---

## Layout

```
cmd/m0/             de-risk spike (no UI) — keep working as a diagnostic tool
cmd/quicktools/     the real app
internal/winapi/    Win32: hotkey, clipboard, focus, sendinput, DPI, theming
internal/transform/ built-in transforms + registry
internal/ui/        palette.go (window) + theme.go (colours, fonts, chrome)
internal/fuzzy/     ranked search over the palette entries
internal/config/    %APPDATA%\quick-tools\config.json
internal/script/    goja engine + hot-reload      (empty, M2)
internal/snippet/   snippet tree                  (empty, M3)
scripts/            user .js transforms
snippets/           user snippet tree
```

### Dependencies

| Need | Package | State |
|---|---|---|
| Win32 window and controls | `github.com/rodrigocfd/windigo` | in use |
| Fuzzy matching | `github.com/sahilm/fuzzy` | in use |
| JS engine for user scripts | `github.com/dop251/goja` | not added yet (M2) |

`lxn/walk` — the obvious Windows GUI choice — **was archived in April 2026**.
Do not adopt it. `windigo` is the pure-Go, cgo-free replacement.

Deliberately avoided: `golang.design/x/hotkey` (we own a message loop already),
`atotto/clipboard` (unmaintained), any TOML/YAML library (config is JSON).

---

## Conventions

- **Layout and colour live in one place each**: the constants block at the top
  of `internal/ui/palette.go`, and the colour vars at the top of
  `internal/ui/theme.go`. Nothing else hardcodes a position or a colour. The
  user edits these directly — keep it that way.
- **The `Transform` struct is the single abstraction.** Built-ins and JS scripts
  both become a `Transform`, so the palette, fuzzy index and preview pane never
  know which is which. Keep it that way — it is what makes a dropped-in script a
  first-class citizen.
- `Registry.Add` replaces by ID rather than appending. That is what makes script
  hot-reload work; do not "fix" it into an append.
- No transform may panic on empty input — the palette previews the highlighted
  entry against whatever is on the clipboard, including nothing. There is a test
  enforcing this.
- JSON transforms must not decode into `map[string]any` without `UseNumber()`.
  Doing so silently corrupts 64-bit ids into float64. There is a test pinning it.
- Windows-only files carry `//go:build windows`.

---

## Gotchas that have already bitten us

**1. Do not write Go files containing backslashes via a shell heredoc.**
The heredoc collapsed `\\` into `\`, which silently turned `EscapeBackslashes`
and `UnescapeBackslashes` into no-ops returning their input unchanged. Use the
Write tool for any file with backslash literals, and grep the tree afterwards.

**2. `runtime.LockOSThread` is mandatory.** `RegisterHotKey` delivers `WM_HOTKEY`
to the *thread* that registered it, and Go migrates goroutines across threads.
This is the usual cause of a hotkey that "works sometimes". `winapi.LockThread`
must be called before `HotkeyLoop`, which refuses to run otherwise.

**3. `SetForegroundWindow` silently fails.** Windows blocks focus stealing. The
working pattern — already implemented in `winapi.RestoreForeground` — attaches
our input queue to the target thread with `AttachThreadInput` for the duration.

**4. Held modifiers corrupt the paste.** The user is still physically holding
Ctrl+Alt from the hotkey; adding Ctrl+V on top makes the target see Ctrl+Alt+V.
`winapi.SendPaste` lifts every held modifier first, then presses a clean Ctrl+V.

**5. `OpenClipboard` fails when another process holds it.** Routine, not
exceptional. `winapi.openClipboard` retries ten times with a 10ms backoff.

**6. A list view ignores custom-draw colours on a selected row.** It always
paints the selected row in system colours -- flat grey with black text on a dark
window. The palette therefore never lets the control select anything: `selIdx`
tracks the highlight and `NmCustomDraw` paints it. Do not reintroduce
`.Select(true)` or `LVS_SHOWSELALWAYS`; it will silently undo the accent colour.

**7. A list view also repaints a *clicked* row in the system accent colour.**
Not selecting from our own code was not enough -- clicking selects the row, and
a selected row ignores custom draw. `LvnItemChanging` now vetoes every
selection change, so the control never has anything selected and the only
highlight is the one we paint. Verified by sampling the pixel colour, not by
eye: the control's accent (#0078D4) and our blue look nearly identical on
screen.

**8. windigo gives every control `WS_EX_CLIENTEDGE` by default.** That sunken
3D edge is the most dated detail on a Win32 window. Clearing `WndStyle` does
not remove it -- the *extended* style must be overridden too, with
`WndExStyle(co.WS_EX_LEFT)`.

**9. Child controls get no rounded corners from DWM.** The corner preference
applies to top-level windows only, so panels are clipped to shape with
`SetWindowRgn` and a round-rect region (`winapi.CreateRoundRectRgn`). Note
`SetWindowRgn` takes ownership of the region -- deleting it afterwards is a
double free.

**10. Appearance setup must not run in `WM_CREATE`.** The child controls do not
reliably have window handles while the parent is still being created, and
styling a zero handle fails silently -- which looks exactly like the styling
code being wrong. It runs from a `sync.Once` on first show instead.

**11. `OptsListView.Column(title, width)` does no DPI scaling.** Unlike
`Position` and `Size`, the width goes straight to the control as raw pixels, so
a value meant as logical units comes out too narrow on a scaled display -- 60%
of the panel at 150%, with every label truncated. `refilter` now calls
`Col(0).SetWidthToFill()` after populating, which sizes to the real client
width and adapts as the scrollbar appears and disappears.

**12. Rebuilding the list flickers unless painting is suppressed.** Every
delete and insert repaints, and so does the scrollbar appearing or disappearing
as the row count changes -- which is why the flash only showed once the list was
long enough to need a scrollbar. `refilter` wraps the rebuild in
`SetRedraw(false)`/`SetRedraw(true)`, and the list carries
`LVS_EX_DOUBLEBUFFER` so the erase-then-paint happens off-screen. That was not
enough on its own: `LVS_EX_DOUBLEBUFFER` covers painting *inside* the list, but
when the scrollbar appears or disappears the control's non-client area changes
and the parent repaints the strip beneath it, and that hand-off between two
windows is what flashed. The parent therefore also carries `WS_EX_COMPOSITED`,
which renders the whole hierarchy into one buffer. Note the
invalidate in `setSelection` deliberately keeps `erase: true`; with double
buffering it costs nothing, and `false` would leave stale rows behind when a
filter shrinks the list.

**13. Themes and custom draw are separate fights.** `SetWindowTheme` with
`DarkMode_Explorer` is what makes scrollbars dark; it is unrelated to the
selection problem above (removing it does not fix the highlight).

**14. Screenshots need a DPI-aware capture.** A DPI-unaware PowerShell reports
logical coordinates but `CopyFromScreen` captures physical pixels, so the window
appears at 1.5x its reported position and looks mispositioned when it is not.
Call `SetProcessDPIAware()` in the capture script first.

**15. A Go const block does not repeat the last expression.** Writing
`cmdOpen uint16 = 100` then bare `cmdAutoPaste` and `cmdQuit` gives all three
the value 100 -- unlike an `iota` block, a plain expression is repeated
verbatim. It compiled and only failed on the duplicate-case check.

**16. goja does not parse ES module syntax.** Scripts use plain globals
(`var name`, `var tags`) and a `function transform(input)`, not `export
default`. An earlier version of this file documented the `export` form; it does
not work.

**17. `-test-scripts` prints to stdout, so it needs a console.** Once M4 builds
with `-H windowsgui` there is no console attached and the output goes nowhere.
That flag will need `AttachConsole(ATTACH_PARENT_PROCESS)` first, or the runner
moves to its own small command.

**18. `IsDialogMessage` swallows Enter.** windigo's main loop calls it by
default (`processDlgMsgs: true`) for dialog-style Tab navigation. It turns
VK_RETURN into a dialog IDOK command, so the key never reaches the search box
subclass and pressing Enter silently does nothing -- while arrows and typing
work fine, which makes it look like an Enter-specific bug in our code. The
window sets `ProcessDlgMsgs(false)`; this window does not use Tab navigation.

This shipped broken from M1 to M3 because the earlier testing only exercised
preview, arrow keys and mouse clicks -- never the Enter key. When verifying a
UI, test the path that commits the action, not just the one that displays it.

**19. Known `go vet` finding.** `internal/winapi/clipboard.go` has one
`possible misuse of unsafe.Pointer` in `lockGlobal`. It is sound and documented:
the memory came from `GlobalAlloc`, so it lives outside the Go heap and the GC
cannot move it. All such conversions are deliberately funnelled through that one
function. Do not scatter new ones.

---

## Next steps

1. Tray icon and a quit path — the app currently can only be killed.
2. M2: goja engine with a per-run interrupt budget (~2s), `scripts/` hot-reload,
   a `--test-scripts` fixture runner, then hand the two parsers to the user with
   stubs, spec, and failing fixtures.

The full approved plan lives at
`C:\Users\ACER\.claude\plans\i-want-you-to-swirling-possum.md`.
