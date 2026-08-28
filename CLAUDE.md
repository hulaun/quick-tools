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

The user is building this partly to learn. Two pieces are **reserved for them**:

- `scripts/java-to-json.js` — Java `toString()` output → JSON
- `scripts/js-object-to-json.js` — JavaScript object literal → JSON

**Do not implement these.** Build everything *around* them so they are ready to
write against: a wired-up stub, the grammar spec, failing test fixtures, and a
red/green runner. If asked to "finish the project", finish everything except
these two and say so.

The same instinct generalises: when a task has a genuinely interesting
algorithmic core (parser, scheduler, diff, solver), ask before implementing it.
Build all the mechanical parts fully — those were never the point.

---

## Current status

| Milestone | State |
|---|---|
| **M0** — de-risk spike | Built. Clipboard round trip verified automatically. **Interactive hotkey test still pending user confirmation.** |
| **M1** — palette UI + built-in transforms | Done. Dark themed palette, fuzzy search, live preview. |
| **M2** — goja scripting + handoff | Not started. |
| **M3** — snippets | Not started. |
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

### Known open questions

- No tray icon and no quit path yet: stop it with
  `taskkill /IM quicktools.exe /F`. This is the next piece of work.
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
go build -o bin/m0.exe ./cmd/m0    # the spike
./bin/m0.exe -selftest             # unattended clipboard check
./bin/m0.exe                       # interactive: Ctrl+Alt+Space
```

Final builds use `-ldflags "-H windowsgui -s -w"` so there is no console window.

---

## Layout

```
cmd/m0/            de-risk spike (no UI) — keep working as a diagnostic tool
cmd/quicktools/    the real app (not yet written)
internal/winapi/   Win32: hotkey, clipboard, focus, sendinput
internal/transform/ built-in transforms + registry
internal/ui/       palette window, tray          (empty)
internal/script/   goja engine + hot-reload      (empty)
internal/snippet/  snippet tree                  (empty)
internal/config/   %APPDATA%\quick-tools\config.json  (empty)
scripts/           user .js transforms
snippets/          user snippet tree
```

### Planned dependencies (none added yet)

| Need | Package |
|---|---|
| Win32 window and controls | `github.com/rodrigocfd/windigo` |
| JS engine for user scripts | `github.com/dop251/goja` |
| Fuzzy matching | `github.com/sahilm/fuzzy` |

`lxn/walk` — the obvious Windows GUI choice — **was archived in April 2026**.
Do not adopt it. `windigo` is the pure-Go, cgo-free replacement.

Deliberately avoided: `golang.design/x/hotkey` (we own a message loop already),
`atotto/clipboard` (unmaintained), any TOML/YAML library (config is JSON).

---

## Conventions

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

**7. Themes and custom draw are separate fights.** `SetWindowTheme` with
`DarkMode_Explorer` is what makes scrollbars dark; it is unrelated to the
selection problem above (removing it does not fix the highlight).

**8. Screenshots need a DPI-aware capture.** A DPI-unaware PowerShell reports
logical coordinates but `CopyFromScreen` captures physical pixels, so the window
appears at 1.5x its reported position and looks mispositioned when it is not.
Call `SetProcessDPIAware()` in the capture script first.

**9. Known `go vet` finding.** `internal/winapi/clipboard.go` has one
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
