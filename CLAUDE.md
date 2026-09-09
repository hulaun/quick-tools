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

1. `scripts/query-string-to-json.js` — decoding, repeated keys → arrays
2. `scripts/json-to-query-string.js` — the reverse: flattening a tree into keys
3. `scripts/json-to-insert-query.js` — writing SQL, quoting, camel → snake
4. `scripts/json-to-update-query.js` — the same, plus which key is the `WHERE`
5. `scripts/js-object-to-json.js` — recursive descent over a bracketed grammar
6. `scripts/json-to-js.js` — the reverse, and why `JSON.parse` cannot be used

`scripts/java-to-json.js` **is written** — by the user first, then rewritten on
2026-08-29 at their explicit request after they had done it themselves and
wanted to see another take. That sequence is the only thing that makes it an
exception. Do not treat it as a precedent for filling in the other six; the rule
below still holds, and it is overridden only by the user asking in as many
words. Its 22 fixtures are the worked example the folder no longer has
elsewhere, so point at it rather than writing a new one.

Three of them are pairs pointing in opposite directions (query string, SQL
values, JS literal), which is deliberate: the second of a pair is faster than
the first, and each direction's fixtures are largely the other's outputs.

None duplicates a built-in, so nothing is broken while they are unwritten —
each is a capability the tool does not have yet. That was deliberate: handing
back a working built-in as a stub would have cost the user a feature.

The set was revised on 2026-08-29 at the user's request: `properties-to-json`
and `stacktrace-to-json` were dropped as things they will not use,
`java-to-json` changed from parsing `toString()` output to reading a class
declaration into a JSON template with `""` values, and the four JSON-out
transforms were added. `csv-line-to-json.js` and `curl-to-json.js`, mentioned in
earlier versions of this file, no longer exist; `java-to-json.js` is now the
worked example in their place.

**Do not implement these.** The scaffolding is finished:
- stubs with the contract wired up and hints, but no parser
- `scripts/README.md` — the grammar, the shared rules, and the ambiguity policy
- `scripts/*.test.json` — 119 cases; the six reserved ones are red
- `quicktools.exe -test-scripts` — the red/green loop, `-only <text>` to narrow
  it to one script or one case and print the values untruncated

If asked to "finish the project", finish everything except these six and say
so. Do not "helpfully" fill in a stub.

### A seventh, in Go: `api.ParseCurl`

`internal/api/curl.go` holds the same arrangement for the API tab. A `curl`
line in, a `Request` out — shell quoting, backslash continuations, flags that
take a value and flags that do not, `-d` implying POST unless `-X` says
otherwise. **`FormatCurl` is written**, as the worked example of the easy
direction and because copy-as-curl is what makes the tab useful on a remote
machine; `ParseCurl` is the reserved one. That makes it the fourth pair
pointing in opposite directions, after query string, SQL values and the JS
literal.

The red loop is kept out of the default suite, so that `go test ./...` stays
green while it is unwritten — the same reason the six JavaScript stubs live
behind `-test-scripts` rather than in the Go tests:

```bash
QUICKTOOLS_TODO=1 go test ./internal/api/ -run ParseCurl -v
```

Without the variable those three tests skip, printing why. Nothing depends on
it: the tab is complete without it.

The same instinct generalises: when a task has a genuinely interesting
algorithmic core (parser, scheduler, diff, solver), ask before implementing it.
Build all the mechanical parts fully — those were never the point.

---

## Current status

| Milestone | State |
|---|---|
| **M0** — de-risk spike | Done, and since **deleted**. `cmd/m0` proved the Win32 clipboard, focus and paste path before there was a UI; once the real app had exercised all of it for six milestones the spike was only another thing to keep compiling. Removed 2026-09-03 at the user's request, along with `winapi.HotkeyLoop`/`Quit`/`LockThread`, which existed solely to serve it -- the app uses `RegisterHotkeyFor` against its own window instead. Earlier versions of this file said to keep m0 as a diagnostic; that no longer applies. |
| **M1** — palette UI + built-in transforms | Done. Dark themed palette, fuzzy search, live preview. |
| **M2** — goja scripting + handoff | Done. Engine, hot-reload, fixture runner, stubs and spec all in place. |
| **M3** — snippets | Done. Folder tree, searchable in the same palette, Enter copies. |
| **M4** — packaging | Done. Single instance, autostart, icon, manifest, windowless build. |
| **M5** — notes | Done. Second tab: editable notes over the same folder tree. |
| **M6** — places | Done. Third tab: named paths, opened in Explorer, an editor or a terminal. |
| **M7** — API tab | Done. Fifth tab: saved `.http` requests, environments, send, response, editing, the post-response hook, and copy-as-curl. Environments were reworked on 2026-09-08 into a global stage over per-project values -- see below. `ParseCurl` is reserved for the user (see above). The plan is at `~/.claude/plans/m7-api-tab.md`. |
| **M8** — macros | Done. Fourth tab: recorded keystroke sequences, replayed into the previous window. A low-level keyboard hook records, `SendInput` replays, and the first `Ctrl+Z` after a multi-change macro is turned into as many as it needs. |

### What works today

- `internal/winapi` — hotkey registration, clipboard get/set, focus restore,
  synthesised Ctrl+V. All pure `syscall`, no cgo.
- `internal/transform` — 40 built-ins across Case, Path, JSON, Encoding, Lines,
  Misc. `go test ./...` is green.
- `internal/ui` — the palette: dark theme, Segoe UI, DWM rounded corners,
  accent highlight, live preview. `internal/fuzzy` ranks entries.
- **The window is glass.** It sits on a real acrylic blur -- the desktop behind
  it, blurred and tinted by `acrylicTint` -- and the window background and the two lists let it through
  while the search box and every pane holding text stay solid over it. The
  highlighted row is a rounded pill rather than a full-width bar, and the
  scrollbars are drawn by the app -- no arrow buttons, a track the colour of
  whatever is behind it, a rounded thumb over the content. None of the three
  came free; gotchas 46 to 55 are why. The acrylic in particular is not any of
  the documented DWM calls: both of those were tried and neither works on a GDI
  window. Corner radii come in two scales -- the chrome you aim at is a capsule,
  the panels you read out of are barely rounded -- and they are rounded by
  painting the corners out rather than by clipping, which is the only thing that
  works (gotcha 53).
- **Selected text is grey, and that is what decides the caret's colour.** A
  plain edit paints its selection in the system highlight -- the Windows accent,
  blue here -- and no message changes that, so it is recoloured afterwards in
  the pass that is already setting the alpha (`winapi.Selection`, and
  `colTextSel`). The reason is the caret: Windows draws one by inverting the
  pixels under it, so over blue it came out orange. Over the grey it comes out
  #c5c5c5, which is what it looks like everywhere else. Gotcha 55.
- `cmd/quicktools` — the real app.
- Tray icon with a context menu: open, toggle auto-paste (persisted), toggle
  start-with-Windows, quit. **Start-with-Windows is opt-in and off by default**
  -- M4 built the toggle, it does not turn itself on. The same thing from the
  command line: `quicktools.exe -autostart on|off|status`. It writes `HKCU\...\CurrentVersion\Run\quick-tools`
  pointing at the *current* exe, so moving or rebuilding the binary elsewhere
  leaves a stale entry that `AutostartEnabled` deliberately reports as off.
  That is exactly what happened when the build output moved to `bin/`: the Run
  entry still named the repo-root exe, which no longer exists, so login started
  nothing and the tray checkbox showed unchecked. Re-point it after any move
  with `bin\quicktools.exe -autostart on` -- done on 2026-09-04.
- Single instance via a named mutex; a second launch broadcasts a registered
  window message asking the running copy to open, then exits.
- `winres/` + `cmd/quicktools/rsrc_windows_*.syso`: app icon, version info, and
  a manifest with per-monitor-v2 DPI and common controls v6. Regenerate with
  `.uild.ps1 -Resources` after changing the icon or manifest.
  The icon is generated as a 32x32 ICO and `go:embed`ed in `internal/ui`.
- `internal/script` — goja engine with a 2s interrupt, hot-reloading loader,
  and the `.test.json` fixture runner behind `quicktools.exe -test-scripts`.
- `internal/snippet` — walks `snippets/`, one entry per text file plus the
  folders themselves, and writes: `Save`, `CreateNote`, `CreateFolder`.
- The palette has four modes -- `modeTransforms`, `modeNotes`, `modeMacros`
  and `modeAPI` -- shown as four tabs and cycled by the hotkey. The first two
  act on text, and the last two reach outside the process, one synthesising
  input into another application and the other sending a request over the
  network. (There were five; the Places tab was removed -- see
  "Removed: the Places tab".)
  One index per mode, one set of controls: the list, the search box and the right-hand pane are the same three
  controls in each. In notes mode the right pane is a live editor -- `Tab` to
  move into it, `Ctrl+S` to save. Naming happens in `internal/ui/rename.go`: a
  hidden Edit moved over the highlighted row. `Ctrl+Backspace`/`Ctrl+Delete`
  delete a word in either text field.
- **The editing keys work in notes, macros and API**, dispatched by mode in
  `createEntry`, `beginRename` and `deleteEntry`:

  | Key | Notes | Macros | API |
  |---|---|---|---|
  | `Alt+D` | new note | new macro, recording immediately | new `.http` request from a template |
  | `Shift+Alt+D` | new folder | -- | new folder |
  | `F2` | rename the file | rename the entry in macros.json | rename the file |
  | `Del` | delete the file, permanently | delete the entry | delete the file, permanently |

  Requests follow the notes rule, because a request *is* a file. The rename path is shared outright: `renameState.isRequest`
  picks `p.requests` over `p.snippets` and both are a `snippet.Store`, so the
  work is identical and only the index to rebuild afterwards differs.

  `Del` deliberately means two different things. A note *is* a file; a place is
  a shortcut *to* one, and deleting a shortcut has never meant deleting its
  target -- a palette that could erase `.m2` on one keypress would be
  indefensible. Both confirm first, and each confirmation says which of the two
  is about to happen. The note delete is `os.RemoveAll`, so nothing reaches the
  Recycle Bin: correct for a store holding credentials, and the reason it asks.
- `internal/macro` + `internal/ui/macros.go` -- the Macros tab. `macros.json`
  beside the exe holds recorded keystroke sequences; Enter replays one into the
  window the palette was opened from. `Alt+D` hides the palette, installs a
  `WH_KEYBOARD_LL` hook, and records until the app's own hotkey comes round --
  the hook swallows it, so the palette does not also try to open on the key that
  ended the recording. `Ctrl+R` re-records over an existing macro, keeping its
  name and any tuning.

  **The package is free of Win32 on purpose.** `internal/macro` is types, chord
  text in both directions, the recorder's normalisation rules and the store; the
  hook and the `SendInput` replay are `internal/winapi/hook.go` and
  `sendkeys.go`. That split is what makes the interesting half testable: 30
  tests, none of which needs a window.

  Two kinds of step, and the distinction is the design. A **chord** is one
  non-modifier key plus what was held, replayed as that virtual key. **Text** is
  literal characters, replayed as Unicode code units with `KEYEVENTF_UNICODE`.
  The layout is applied once, at record time, by `ToUnicodeEx` -- so a macro that
  typed `"` types `"` afterwards whatever the keyboard is set to, rather than
  replaying whichever key happened to produce it.

  The undo depth counts *runs* of consecutive text-changing steps, not steps,
  because that is how an editor groups an undo: typing three characters is one
  undo and moving the caret between them breaks the group. Below a depth of two
  nothing is armed at all -- the user's own `Ctrl+Z` is already right, and
  hooking the keyboard to watch it happen would be theatre. The estimate is a
  guess and says so; `"undo"` on a macro replaces it.
- ~~`internal/place` + `internal/ui/places.go` -- the Places tab.~~ **Removed
  2026-09-09** -- see "Removed: the Places tab" below.
- `internal/ui/command.go` -- the search box's memory and its command line.
  **The query is remembered per tab** and put back, wholly selected, the next
  time the palette opens on that tab: copying one value out of a config and
  coming back for the next was costing the same typing twice. Selected rather
  than merely present is what makes it free -- the list is already filtered to
  what you were looking at, and the first character typed replaces the lot.
  **A query beginning with `>`, or `Ctrl+Shift+P`, lists the tabs themselves**
  and Enter goes to the one highlighted, so the fifth tab is `>api` rather than
  four presses of the hotkey and a count in your head. It is a *view over the
  search box*, not a sixth mode: `p.mode` does not change while it is showing,
  the tab you came from stays lit, and the right-hand pane keeps what it was
  holding -- nothing is rebuilt or discarded for a prefix that may be gone on
  the next keystroke. Esc leaves it and restores the query rather than closing
  the window. Tabs match on tags as well as names, so `>login` finds Notes.
- **The stage is global and the values are per project.** `Ctrl+E` cycles
  `local -> sit -> uat -> prod` and means "put the app on UAT", not "switch this
  one project"; each folder under `storage/requests/` may hold an `env.json`
  giving its own hosts for those same stage names, and `storage/env.json` is the
  base layer underneath them all. Resolution walks from the request's own folder
  up to the storage root, first value found winning, so a project overrides only
  what differs and a shared value is written once.

  This replaced one flat `env.json` on 2026-09-08. The problem with the flat
  file was not that the list got long -- it was that the list was a *product*.
  Four projects times three stages is twelve entries of which only three are
  states the highlighted request can meaningfully be in, so `Ctrl+E` spent most
  of its cycle somewhere wrong. Splitting the two axes makes the cycle as long
  as the stage list and keeps it there whatever the project count, and a new
  project is a folder with an `env.json` in it rather than a central file to go
  and edit.

  Two options were considered and rejected, and both are worth not
  re-litigating. A **sixth list below the requests** would not have fixed the
  product at all -- the list is still flat, just permanently visible -- and it
  breaks the rule that every mode is the same three controls. **Environments as
  rows in the requests list** overloads Enter, which sends on one row and would
  select on another, and pollutes the fuzzy search with rows nobody was looking
  for.

  `Alt+E` opens whichever file applies to the highlight -- the nearest
  `env.json` at or above it, or, for a project that has none yet, the path its
  own would go at, so the first edit in a new project does not land in the file
  everybody shares. `Ctrl+Shift+E` still does the same thing. The path is held
  in `envEditPath` for the length of the edit: moving the highlight while typing
  must not redirect `Ctrl+S` into another project's file. An `env.json` is
  filtered out of the requests list, since it lives among the requests but is
  not one.

  The session overlay a hook writes into is keyed by **project as well as
  stage**. A token lifted from `gtos` on uat has no business being visible to a
  `payments` request on uat -- the same argument that already scoped it per
  stage, one level down.

  The stage being global means it can be pointed at a project that has never
  heard of it. That is allowed, and the strip says `gtos · uat` or
  `payments · prod (not set here)` rather than resolving nothing quietly.
  `internal/api/env.go` is the whole model and has 17 tests of its own in
  `envtree_test.go`.
- `internal/ui/chain.go` -- Tab runs the highlighted transform and feeds the
  result back in as the input to the next, so several transforms compose in one
  pass. The chain is a list of `{name, before}`: keeping the input each step was
  applied to is what makes Backspace able to undo one, since there is no inverse
  transform to call. Nothing reaches the clipboard until Enter, so an abandoned
  chain leaves it untouched.
- Measured with 500 notes on disk: 26--31 ms to open, 17--45 ms to switch tabs,
  3 ms per second for the background folder poll. The tab switch is dominated by
  inserting the rows into the list view; past a few thousand notes that is the
  thing to make virtual (`LVS_OWNERDATA`), nothing else.
- Transforms run on a worker goroutine and post results back via `wmRunDone`;
  stale preview results are discarded. A slow script cannot freeze the window.

### Known open questions

- Windows 11 hides newly registered tray icons in the overflow ("^") by
  default. The icon is there and works; the user drags it out to pin it. Not a
  bug and nothing to fix in code.
- **Resolved 2026-09-07 by the overlay scrollbar.** The flash at the exact
  moment the list scrollbar appeared or disappeared -- reduced by
  WS_EX_COMPOSITED but never gone -- cannot happen any more: the native bar now
  lives outside the region the control is clipped to, so the transition happens
  where nobody can see it and the client width the rows lay out in never
  changes. That was a side effect rather than the goal; see gotcha 46.
- **A line of a pane flashing pale while a selection is dragged out** was
  reported on 2026-09-07 and is addressed by gotcha 56, but the diagnosis is a
  measurement rather than a sighting: the acrylic behind a pane measures #565656
  over a bright window against the pane's own #1c1c1c, so a line that loses its
  alpha for a frame is exactly that pale. If it still happens, the remaining
  suspect is a compositor frame landing between the control's paint and the
  alpha repaint, which no amount of re-asserting afterwards can close -- that
  would mean buffering the control's paint instead, and only then is it worth
  the risk.
- **Resolved, or never true at 750x500.** A screenshot taken on 2026-09-03 after
  the window resize shows "Search transforms" rendering correctly. Nothing was
  changed to fix it, so if it reappears, note what the window size was.
- Elevated target windows will reject synthesised input (Windows UIPI). This is
  an OS rule, not a bug — do not try to defeat it.
- Nothing has been pushed to the remote yet. The local repo has commits; ask
  before pushing.
- Measured footprint: **7.5 MB private working set** (what Task Manager's
  "Memory" column shows), 28 MB total working set, 52 MB commit charge, 11 MB
  on disk -- with 500 notes on disk, which is what makes it the honest upper
  figure. With a normal tree and all three tabs opened it measures 6.0 MB
  private / 17.7 MB working set. Adding the Places tab cost **143 KB** on disk
  (11.21 -> 11.35 MB), most of it `os/exec` newly linked in for finding the
  openers on PATH; run-time cost is a handful of strings.

  **The Macros tab cost about 35 KB.** Measured with `go tool nm -size` over the
  release binary and summed across `internal/macro`, the hook and replay in
  `internal/winapi`, and `internal/ui/macros.go` -- 16.5 KB, 5.2 KB and 14.1 KB
  of symbols respectively. It links no new standard library at all: `encoding/
  json`, `sync`, `time`, `sort`, `strconv` and `unicode/utf16` were every one of
  them already in. Built in isolation `internal/macro` measures 553 KB, but that
  is almost entirely `encoding/json` being counted a second time, which is why
  the symbol sum is the honest number here and the isolation figure is not. The
  release binary is 15.32 MB.

  **The API tab cost 3.96 MB on disk** (11.95 -> 15.91 MB): `net/http`,
  `crypto/tls` and the certificate verification behind them. Measured in
  isolation the `internal/api` package is 4.35 MB. That is by far the largest
  single addition the app has taken, and it is the price of speaking HTTPS at
  all -- there is no smaller way to do it in Go. Re-measure the private working
  set at rest before quoting a memory figure for it. Quote the private
  working set. `Process.WorkingSet64` is the number
  Go exposes most readily and it was reported here once by mistake -- it counts
  shared DLL pages that every Windows process maps and that cost nothing extra,
  so it overstates the app's real cost by roughly 4x.

---

## Build and test

Go lives at `C:\Program Files\Go`. In Git Bash:

```bash
export PATH="$PATH:/c/Program Files/Go/bin"

go test ./...                      # all tests
go vet ./...                       # see "known vet finding" below

# The exe goes in bin/. config.Root anchors relative config paths to the folder
# holding the exe *except* that a bin is stepped out of, so storage/ and
# scripts/ stay at the root -- the development layout and the release layout are
# the same one, which is the point. Earlier versions of this file said to build
# at the repo root for that reason; the rule in config.Root replaced it.
go build -o bin/quicktools.exe ./cmd/quicktools
./bin/quicktools.exe                    # then press the hotkey
./bin/quicktools.exe -test-scripts      # run the script fixtures
./bin/quicktools.exe -test-scripts -only java   # one script, values in full

# The reserved Go parser's red loop, kept out of the default suite.
QUICKTOOLS_TODO=1 go test ./internal/api/ -run ParseCurl -v
```

Final builds use `-ldflags "-H windowsgui -s -w"` so there is no console window.

### The inner loop: `./restart.sh`

Quit, rebuild, relaunch detached -- the three steps that must happen in that
order, since Windows will not let Go overwrite a running exe.

```bash
./restart.sh              # release build (no console), detached
./restart.sh --dev        # console build, so panics and log output are visible
./restart.sh --test       # go vet and go test first
./restart.sh --no-launch  # quit and build, leave it stopped
```

### `qt` -- restart from any directory

`qt.cmd` restarts the *installed* app without rebuilding: kill, wait for the
single-instance mutex to be released, relaunch `bin/quicktools.exe` detached.
A two-line shim in `%LOCALAPPDATA%\Microsoft\WindowsApps` (on the Windows PATH
by default) calls it, so `qt` works from cmd, PowerShell, Git Bash, the Run
dialog and any working directory.

```
qt                  kill and relaunch the built exe
qt stop             just quit it
qt build [--dev|--test|--no-launch]   rebuild first, via restart.cmd
```

`restart.sh` is still the thing to run while editing Go; `qt` is for when the
resident app is not running or is stale and you are not in the repo.

`build.ps1` is the *release* script and does not touch the running process:
vet, test, build (`-Dev` for a console build), report the size, plus
`-Resources` to regenerate the icon/manifest `.syso` from `winres/`. It is not
the edit-run loop -- run it before shipping, `restart.sh` while working.

**Config is read once, at startup** (`config.Load` in `cmd/quicktools/main.go`).
Scripts and snippets hot-reload; `config.json` does not. Changing the hotkey or
a directory needs a restart.

---

## Layout

```
cmd/quicktools/     the real app -- the only command
bin/quicktools.exe  the build output (gitignored)
internal/winapi/    Win32: hotkey, clipboard, focus, sendinput, DPI, theming;
                    hook.go (WH_KEYBOARD_LL), sendkeys.go (macro replay),
                    acrylic.go (the blur behind), opaque.go (the alpha
                    repaint), capture.go (mouse capture)
internal/transform/ built-in transforms + registry
internal/ui/        palette.go (window, modes, keys), notes.go (notes mode),
                    command.go (remembered queries, the ">" tab switcher),
                    macros.go (record, replay, the undo watch),
                    api.go + api_edit.go (API mode), editing.go (word delete),
                    theme.go (colours, fonts, chrome),
                    glass.go (what is solid and what is backdrop),
                    scrollbar.go (the overlay scrollbar)
internal/fuzzy/     ranked search over the palette entries
internal/config/    %APPDATA%\quick-tools\config.json
internal/script/    goja engine + hot-reload
internal/snippet/   note tree: walk, read, write, create
internal/macro/     macros.json: steps, chord text, the recorder's rules
internal/api/       .http parser, environments, HTTP client, post-response hook
scripts/            user .js transforms -- half checked in, so not under storage/
storage/            everything the user accumulates; gitignored as one unit
  snippets/           the Notes tab
  requests/           the API tab, one .http per file
    <project>/env.json  that project's values for each stage
  macros.json         the Macros tab
  env.json            the base layer the project files override
storage.example/    the checked-in template: cp -r storage.example storage
restart.sh          quit, rebuild, relaunch detached -- the inner loop
restart.cmd         the same, through Git Bash, for cmd/PowerShell/Explorer
qt.cmd              restart the app from anywhere; a shim on PATH calls it
build.ps1           release build: vet, test, build, resources
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
  user edits these directly — keep it that way. Two of the colours are
  structural rather than decorative and say so in a comment: `colGlass` is black
  because black is what the compositor reads as transparent, and `colSurface` is
  whatever it likes so long as it is not black (gotcha 47).
- **The `Transform` struct is the single abstraction** *for transforms*.
  Built-ins and JS scripts both become a `Transform`, so the palette, fuzzy
  index and preview pane never know which is which. Keep it that way — it is
  what makes a dropped-in script a first-class citizen.
- **Notes are deliberately not Transforms.** Until M5 a snippet was registered
  as a `Transform` whose `Run` ignored its input and returned the file, which
  cost no extra code at all. Editing broke the symmetry: a note has a file
  behind it that gets written back, and a transform has nothing of the kind. So
  notes have their own index and their own mode. Do not merge them back.
  **Macros are not Transforms either**: a macro produces no text at all -- its
  whole output is input synthesised into another process -- and its Enter is
  the only one in the app that does not touch the clipboard. Four modes, four
  indexes. (Places was a fifth, on the same reasoning; it was removed.)
- **Nothing may close over a control's `HWND` outside a handler.** Controls have
  no window handle until `WM_CREATE`, which is long after `events()` wires
  everything up, so a captured handle is zero forever. Call `.Hwnd()` inside the
  handler. This cost an hour: the `WM_CHAR` subclass captured a zero handle, so
  every keystroke was passed to `DefSubclassProc` on a null window and typing
  did nothing at all.
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
This is the usual cause of a hotkey that "works sometimes". `main` calls it
before any window exists, and nothing else may register a hotkey off that
thread. The old `winapi.HotkeyLoop`, which refused to run unless a matching
`LockThread` had been called, went with `cmd/m0` -- the guard is gone, so this
is now a rule to follow rather than one that is enforced.

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

**17. A `-H windowsgui` binary has no console.** Anything printed by a
command-line flag goes nowhere. `winapi.AttachConsole` reattaches to the parent
terminal (allocating one if there is none) and rebinds `os.Stdout`/`os.Stderr`,
which Go bound at startup to handles that do not exist in a GUI process.
`-test-scripts` and `-autostart` call it first. Verified working from the
release build.

The allocate-one fallback was itself a trap. Git Bash runs under mintty, which
is not a console: it hands the child an inherited *pipe* for stdout and has no
console for `AttachConsole` to attach to. So the fallback allocated a fresh
console, rebound `os.Stdout` to it -- throwing away the working pipe -- printed
there, and the window vanished the instant the process exited. `-autostart on`
therefore looked like a terminal flashing up and doing nothing, when it had in
fact written the Run entry correctly. `AttachConsole` now checks `GetFileType`
on the existing stdout handle first and leaves usable handles alone; only a
genuinely handle-less GUI process attaches or allocates.

**18. `IsDialogMessage` swallows Enter.** windigo's main loop calls it by
default (`processDlgMsgs: true`) for dialog-style Tab navigation. It turns
VK_RETURN into a dialog IDOK command, so the key never reaches the search box
subclass and pressing Enter silently does nothing -- while arrows and typing
work fine, which makes it look like an Enter-specific bug in our code. The
window sets `ProcessDlgMsgs(false)`; this window does not use Tab navigation.

This shipped broken from M1 to M3 because the earlier testing only exercised
preview, arrow keys and mouse clicks -- never the Enter key. When verifying a
UI, test the path that commits the action, not just the one that displays it.

**19. An Alt combination is `WM_SYSKEYDOWN`, not `WM_KEYDOWN`.** Windows routes
it as a possible menu accelerator. `Alt+D` never reaches a `WM_KEYDOWN`
handler. Handling it in `WM_SYSKEYDOWN` and returning 0 rather than calling
`DefSubclassProc` is also what suppresses the beep an unhandled Alt key makes.

**20. A Win32 Edit has no Ctrl+Backspace.** It does not know the key, so it
inserts DEL (0x7f) and you get a box glyph instead of losing a word. Both the
keydown *and* the character message have to be handled -- swallowing one and
not the other leaves either the glyph or a beep. `internal/ui/editing.go` owns
this, and works in UTF-16 code units because that is what `EM_GETSEL` counts.

**21. Naming a row uses our own Edit, not `LVS_EDITLABELS`.** The list view's
built-in label editing gives the row focus, and this window has spent two
milestones making sure the control never focuses or selects anything (see 6 and
7) -- a selected row ignores custom draw and paints itself in the system accent
colour. `rename.go` asks the control only for the row's rectangle
(`LVM_GETITEMRECT`, whose row index goes in the rect's own `Left` field) and
moves a plain Edit there. It themes correctly for free, because it is a child of
our window and answers our `WM_CTLCOLOREDIT`.

**22. A background reload must not refilter a list that is not on screen.**
The watcher reloads scripts and notes together, and `reloadScripts` used to
refilter whenever the window was open. Saving a note is a change, so one second
after every Ctrl+S it rebuilt the *notes* list and dropped the selection to row
0 -- and `reloadNotes`, running straight after, then faithfully preserved the
wrong row. Both reloads now refilter only when their own mode is showing, and
they preserve the highlighted id rather than resetting it.

**23. `TranslateMessage` runs before dispatch, so swallowing `WM_KEYDOWN` does
not stop the character.** The message loop turns the key into a `WM_CHAR` and
queues it whatever the keydown handler returns, so a key bound as a command is
still typed as text unless the character is swallowed too. That is what
`editChars` is for. Tab is the current example -- it chains a transform, enters
a note, or enters the action list, and is never a character, so `editChars`
drops `0x09`. This was the bug behind Space-to-chain having to live in
`WM_CHAR`; the same trap catches every printable key bound to a command, which
is a good reason to prefer keys that are not printable.

**24. One fuzzy haystack per entry ranks badly once entries have context.** The
matcher rewards a short haystack, so a folder called "work" -- three words and
nothing else -- outranked every note inside it, and the notes tab looked like it
was only searching folders. `internal/fuzzy` now matches in two passes, names
before context, and nothing carries a tag that every entry shares: a "note" tag
on all of them matched any query that was a subsequence of "note" and did
nothing but blur the order.

**25. `SetWindowText` on the editor raises `EN_CHANGE`.** Filling the pane from
a file looks exactly like the user typing, so every note would open already
marked unsaved. `p.quiet` brackets the programmatic set.

**26. Do not quote `WorkingSet64` as memory usage.** It includes shared pages
mapped by every process. Task Manager's "Memory" column is the *private*
working set: `Get-Counter "\Process(quicktools)\Working Set - Private"`. The
two differ by about 4x here.

**27. Launching from a terminal ties the app's life to that terminal.**
`./quicktools.exe &` stays a child of the shell, and closing the terminal takes
the whole process tree with it -- so the resident app quietly dies when you
close Git Bash. `restart.sh` launches through `cmd //c start ""` instead, which
hands the process to the shell and lets it be orphaned. The empty `""` is
`start`'s title argument; without it, `start` reads the quoted path as a title
and opens a console window instead of the app.

**28. `ShellExecuteW` reports failure as a number 32 or less.** Success is a
meaningless instance handle above it. It is the one Win32 function whose error
convention is a magic number rather than a boolean plus `GetLastError`, so
treating a non-zero return as success gets it exactly backwards.
`internal/winapi/shellexec.go` names the codes that actually occur -- 2 and 3
for a moved path, 31 for a file type with nothing registered, and 1223 for an
elevation prompt the user dismissed, which is a decision rather than a failure
and must not be reported as an error.

**29. A path's kind cannot be told from its text.** `hosts` is a file with no
extension and `.m2` is a folder that looks like it has one, and both are in the
first places file anyone writes. So the Places tab stats the path -- which
means it cannot do so on the UI thread, because a stat on a disconnected
network drive blocks for seconds. It runs on a goroutine, the answer is cached,
and `wmPlaceStat` redraws the pane when it lands. `place.Normalize` is
deliberately the opposite: purely textual, so a path that is not there still
resolves and nothing can hang.

**30. Editing a protected file needs `runas`, and failing to ask looks like a
broken editor.** Opening `hosts` without elevation works, and the save at the
end is what fails -- by which point the connection to the missing permission is
long gone. That is what `"elevate": true` on a place is for.

**31. Moving Win32 focus into the action list would undo two milestones of
work.** A real focus change hands the list the arrow keys *and* the selection,
and a selected row ignores custom draw and paints itself in the system accent
colour (see 6 and 7). So `optFocus` is a flag, not a focus change: keyboard
focus stays in the search box throughout and the arrow keys are routed. Which
list they drive is shown by which highlight is accent-coloured and which is
dimmed, since nothing else on the window says.

**32. `a() || b()` in the watcher skips b.** The poll asked
`loader.Changed() || snippets.Changed()`, so on any tick where a script changed
the snippet store was never asked -- and never updated its timestamp, so it
reported changed on the next tick too. Each source's `Changed` is now called
into its own variable first. A short-circuit is wrong wherever the call is the
thing that updates the state.

**33. A destructive key in a text field needs a guard, not just a handler.**
`Del` deletes the highlighted entry -- but the same key deletes the character
after the caret in the search box, so binding it outright would turn "fix a
typo in the query" into "lose a file". It fires only when there is nothing to
the right of the caret and no selection (`atEndOfSearch`), which is the same
rule Backspace already uses for unwinding the chain. The caret is at the end
after typing, so searching and then pressing `Del` still works; `Ctrl+Delete`
still deletes a word, because the handler defers when Ctrl is down.

**34. Editing one entry of a hand-written JSON file must not rewrite the
others.** `places.json` is edited both by hand and by the palette. Every entry
the palette is not touching is carried across as the `json.RawMessage` it was
read as, so a `_comment`, or a field a later version adds, survives an
unrelated rename. The one entry being changed is decoded into a map with
`UseNumber()` -- without it a large integer in an unknown field would come back
as a float64 and be silently rounded on the way out, the same trap the JSON
transforms have a test pinning. Indentation is normalised; that cannot be
helped and is all that changes.

**35. A place's identity is its position in the file, not its row.** The list
is sorted by group and name for reading, and `Load` skips entries with a blank
path -- so the row index is not the file index. `place.Place.Index` carries the
file position, assigned before anything is skipped, and it is what rename and
delete address. Getting this wrong deletes the wrong line, which is why there
is a test for exactly the skipped-entry case.

**36. Swallowing an Alt combination's `WM_SYSKEYDOWN` is what makes the window
stop responding to the keyboard.** Releasing Alt makes `DefWindowProc` send
`WM_SYSCOMMAND`/`SC_KEYMENU`, and its handling of that runs the **menu modal
loop**, which owns the keyboard until it is dismissed. This window has no menu,
so nothing appears on screen -- the palette just goes deaf to every key and only
a mouse click gets out of it.

Normally a character key pressed while Alt is held prevents that. But gotcha 19
has `Alt+D` returning 0 from `WM_SYSKEYDOWN` to suppress the beep, so
`DefWindowProc` never sees the `D` and still treats the Alt press as a bare
one. **The beep fix armed the lockup**, which is why it took two milestones to
show up: it needs the Alt combination the palette itself handles.

The guard is one `WM_SYSCOMMAND` handler on the main window returning 0 for
`SC_KEYMENU`. It goes there rather than on each control because `SC_KEYMENU` is
sent to the top-level window whichever child had focus -- and `Alt+D` moves
focus to the name box between the Alt going down and coming back up, so a
per-control handler would have to cover the one that did not see the keydown.
`WM_SYSCHAR` is swallowed separately on the three Edits, for the beep.

**37. A low-level keyboard hook is owned by the thread that installed it.**
`SetWindowsHookEx(WH_KEYBOARD_LL, ...)` delivers the callback on the installing
thread, while that thread pumps messages -- so `InstallKeyHook` and `Remove`
must both be called from the UI thread, and the callback runs there too. That
last part is load-bearing rather than incidental: it is why `recording` and
`undoArm` have no lock around them, and why anything that changes them has to
go through a posted message rather than being done from a worker.

Windows also gives a hook a few hundred milliseconds and silently unhooks one
that overruns. So the callback appends to a slice and does nothing else; the
Ctrl+Z replacement is posted to the window and sent from there, because sending
it inline would hold up every key on the machine for the length of the replay.

**38. `syscall.NewCallback` allocates a trampoline that is never freed**, and a
process has a hard cap on them. Creating one per recording works perfectly for
a few thousand macros and then fails -- the kind of bug that only ever appears
on someone else's machine. `hookCallback` is a package-level var, created once.

**39. A hook that does not check `LLKHF_INJECTED` records its own replay.**
Everything `SendInput` produces comes back through the hook. Without the check,
replaying a macro while recording feeds the replay into the recorder, and the
undo watcher sees its own Ctrl+Z presses and swallows them.

**40. `ToUnicodeEx` has a side effect on somebody else's window.** Asking what
character a key would produce *consumes* a dead key, so a hook that asks about
every keystroke breaks accent composition in the application being typed into --
a translation call that changes the thing it is asking about. Flag bit 2
(`0x04`, Windows 10 1607 and later) says do not touch the kernel keyboard state,
and `keyText` passes it.

The keyboard state handed to it also has to be built by hand. A low-level hook
runs *before* the key reaches any thread, so `GetKeyboardState` answers about
the key before this one; the hook tracks what is held from its own event stream
instead. Caps Lock is the exception and has to be asked for, since it may well
have been on before the hook existed.

**41. Swallowing the stop key in the hook is what stops `WM_HOTKEY` firing.**
A low-level hook runs before hotkey dispatch, so returning 1 for the palette's
own hotkey means the recording ends and the palette does not additionally try to
open on the same keypress. Getting this backwards gives a recording that stops
and a palette that immediately cycles to the next tab.

**42. Do not swallow the modifier releases when a recording ends.** The obvious
implementation -- "stop passing anything through once stopping is set" -- eats
the Ctrl and Alt key-ups of the stop combination, and the application the user
was typing into is left believing both are still held. Only the stop key's own
release is swallowed.

**43. A macro's characters must not be replayed as keys.** `KEYEVENTF_UNICODE`
puts the character in the scan code field and ignores the virtual key, which is
the only layout-independent way to type. The alternative -- storing the key that
produced the character -- gives a macro that types something else entirely on a
different keyboard layout. The reverse holds for navigation: the arrows, Home,
End, the page keys, Insert and Delete share virtual key codes with the numeric
keypad, and `KEYEVENTF_EXTENDEDKEY` is the only thing telling them apart for
anything reading scan codes -- terminals, remote desktop clients, games.

**44. Zero delay between synthesised keys is a bug, not an optimisation.** Some
applications process a burst arriving inside one turn of their message pump and
apply the navigation and the typing out of order. `macro.DefaultDelay` is 8ms;
`"delay"` on a macro raises it for a target that needs more.

**45. Known `go vet` finding.** `internal/winapi/clipboard.go` has one
`possible misuse of unsafe.Pointer` in `lockGlobal`. It is sound and documented:
the memory came from `GlobalAlloc`, so it lives outside the Go heap and the GC
cannot move it. All such conversions are deliberately funnelled through that one
function. Do not scatter new ones.

**46. A control's scrollbar cannot be restyled, only replaced.** It is
non-client area drawn by the theme engine, and the single knob on offer is
which theme class to use -- which is all `darkenScrollbars` ever did. The VS
Code look (no arrow buttons, a track the colour of whatever is behind it, a
short rounded thumb over the content) is therefore a repaint, and
`internal/ui/scrollbar.go` owns it.

The native bar is not hidden by taking `WS_VSCROLL` away: a list view puts the
style back whenever the row count changes, so anything that removes it is undone
by the next refilter. It is pushed out of sight instead -- the control is made
wider than the space it occupies by exactly a scrollbar's width, and the region
that already rounds its corners clips that strip off the edge of the world. Two
consequences follow and both have bitten already: **nothing may size itself to
the client width** (that is `contentWidth`, not `SetWidthToFill`), and a
multiline edit needs `EM_SETRECT` or it will happily wrap text into the part
nobody can see.

**47. Neither documented route puts acrylic behind a GDI window.** Both were
tried, and the second one wasted the most time because it looks like it works:

- `DWMWA_SYSTEMBACKDROP_TYPE` is accepted, returns success, and does nothing.
  The material is drawn in the window *frame*, and a `WS_POPUP` with no caption
  has no frame.
- `DwmExtendFrameIntoClientArea` with margins of -1 gives it one, and the window
  does change -- so it reads as a win. But what that extends is the *legacy*
  Aero frame, and Windows stopped blurring that in Windows 8. What comes back is
  a flat fill: measured here at **#545454**, identical whatever is behind the
  window. That last part is the test. Capture the screen where the window will
  sit, then capture the window over it: if the "glass" is uniform while the
  thing behind it is not, nothing is being sampled and the material is fake.
  Do not judge this by eye over a dark desktop, where a flat dark fill and a
  blurred dark desktop look the same.
- `WS_EX_COMPOSITED` was the first suspect and was innocent. Removing it changed
  nothing; it is still on, for the reason in gotcha 12.

Every Mica and acrylic window that ships with Windows 11 -- Explorer, Notepad,
Task Manager -- is XAML over DirectComposition and gets the material through a
route GDI has no access to. What works is `SetWindowCompositionAttribute` with
`ACCENT_ENABLE_ACRYLICBLURBEHIND`, which is undocumented, has been stable since
Windows 10 1803, and is wrapped in `winapi.SetAcrylic`. If a future build breaks
it the palette goes flat and dark, not broken -- keep it able to degrade that
way, and do not build anything on it that cannot.

**47a. GDI has no alpha, so the window is glass everywhere or nowhere.** Once
the acrylic is behind it, the compositor reads the fourth byte of every pixel as
alpha -- and GDI never writes that byte, so *everything* painted comes out
transparent whatever colour it is. There is no brush, mode or flag that changes
it.

What changes it is copying a 32-bit bitmap, because that copy is bytewise and
carries the alpha it holds. `winapi.MakeOpaque` blits the finished pixels out,
sets the alpha byte, and blits them back, leaving the colours alone. It has to
run **after** the control has painted -- a control drawing its own text is
exactly the thing that puts the alpha back to zero -- which is why every solid
control has one `WM_PAINT` hook that sequences the overlay scrollbar before the
alpha repaint, and why the strip has to be inside the rectangle that repaint
covers. `internal/ui/glass.go` is the whole policy: solid is anything holding
text that is read or typed, glass is the window background and the two lists.

Nothing can be made *partly* transparent this way. A pixel is either the acrylic
or the colour painted over it; how much desktop comes through is `acrylicTint`'s
alpha, one number for the whole window. `colGlass` is black for the same reason
-- black is what the compositor reads as nothing painted -- so changing it to a
near-black grey silently turns the transparency off.

**48. A subclass has to be asked for before the control exists.** windigo
panics with "Cannot subclass a control after it is created", so anything hung
off `OnSubclass()` belongs in `events()` and not in `applyLook`, which runs on
first show. That is the opposite of the rule for *styling*, which must wait for
`applyLook` because the handles do not exist during `WM_CREATE` (gotcha 10). The
scrollbar and the glass are split across both for exactly this reason: the
handlers are wired early, the widening and the region are applied late.

**49. A list view row highlight cannot be rounded through `ClrTextBk`.** The
control fills the row rectangle with that colour, and a row rectangle is the
full width of the list with the next row hard against it, so there is no corner
in it to round. `drawRow` therefore paints the row itself and returns
`CDRF_SKIPDEFAULT`: a rounded rectangle inset inside the row, then the text.
That is affordable only because every list in this window is one column of plain
text with no icons -- add an icon column and this has to draw it.

**50. `GetClientRect` already excludes a scrollbar.** This is the trap under
gotcha 46 and it cost the most time of anything here. The overlay's geometry was
derived from the client width minus a scrollbar's width -- correct while the
control has no bar, and a scrollbar too far left the moment it grows one, since
the client rect has by then shrunk by exactly that amount itself. The symptom
was an overlay that sat 26 pixels inboard, but *only* on a list long enough to
scroll, which is the only time anybody looks at a scrollbar.

What made it hard to see is when the width changes. `WM_SETREDRAW(TRUE)` at the
end of `refilter` is the moment the list works out it needs a bar, so the client
rect was one width during `applyLook` and another by the first paint, and every
measurement taken at the wrong end of that agreed with itself. `visibleWidth`
therefore measures the **window** rectangle, which is the size we set and does
not move.

**51. A report-mode list view draws a hairline down each column's right edge.**
Nothing turns it off, and it is not coming from the rows -- those are drawn by
hand and skip the default entirely (gotcha 49). It showed as a pale vertical
line a few pixels left of the scrollbar, and the user reported it before we
did. Proved by narrowing the column sixty pixels and watching the line move
sixty pixels with it, which is the cheap way to identify any stray mark: change
one number and see what it is anchored to.

The fix is `columnWidth`: run the column out to the full client width so the
line lands either under the overlay strip, which repaints on top of it, or past
the region the control is clipped to. The full client width and not a pixel
more -- a column wider than the client is what makes a horizontal scrollbar
appear.

**52. Nothing insets the text of a static.** An edit has `EM_SETMARGINS` for a
single line and `EM_SETRECT` for several; a static has neither, and puts its
text against the frame. The two labels in the right-hand column are panels in
their own right, so their text is drawn by hand in `paintLabel` -- over the top
of the control's own, which cannot be suppressed, with a fill in between.

**53. A window region does not round a child control here -- painting does.**
`SetWindowRgn` installs the region and it is genuinely the right shape:
`GetWindowRgn` reports COMPLEXREGION and `PtInRegion` puts (1,1) outside a
capsule and (6,6) inside. It also does hide the native scrollbar pushed off the
right-hand edge, so it is doing *something*. The corners still come back square,
and neither dropping `WS_CLIPCHILDREN`, dropping `WS_EX_COMPOSITED`, nor forcing
a full `RedrawWindow` with `RDW_ALLCHILDREN` changes it.

What works is painting the corners out afterwards -- `maskCorners` fills the
four triangles the rounded shape leaves over with the window background. That
was the clue all along: the highlighted row was the one rounded thing in this
window that was never in doubt, and it is the one nothing was asked to clip.

Two things follow. The mask has to run **after** the alpha repaint, because
those pixels are meant to stay transparent and show the acrylic like the window
around them, and `MakeOpaque` would otherwise have just made them solid. And the
*window itself* cannot be rounded this way at all: the acrylic is drawn behind
the whole window rectangle, so painting a corner transparent shows more acrylic
rather than less. `DWMWCP_ROUND`'s fixed 8 is as round as the palette gets.

**54. `CreateRoundRectRgn` and `RoundRect` take an ellipse, not a radius.** The
last two arguments are the width and height of the ellipse the corner is cut
from, which is *twice* the radius. Hand either of them a radius and you get half
the corner you asked for.

This is nastier than an ordinary off-by-two because half a corner is still a
corner: changing the number does change the picture, just never by as much as
the name says, so it reads as a value that failed to take effect rather than one
being applied wrongly. It survived a round of "the tabs and the window still
look the same" for exactly that reason. It was also not the only thing wrong at
the time -- gotcha 53 was underneath it -- which is worth remembering: two
independent causes for one symptom made each fix look like it had failed.

`roundCorners` now takes a real radius and doubles it in one place. Every radius
in `theme.go` and in `palette.go`'s constants block is a real radius; the two
call sites that talk to GDI directly (`drawRow`, and the scrollbar thumb, whose
ellipse is its full width so the ends come out as semicircles) say so where they
do the conversion.

**55. A caret has no colour of its own -- it is whatever it stands on, inverted.**
So the only way to decide what the caret looks like is to decide what is behind
it, and behind it, when text is selected, is the system highlight colour. Here
that is the Windows accent, #0078d4, and inverting it gives #ff872b: the caret
turned orange the moment anything was selected, in the search box and in every
pane.

Nothing overrides the selection colour of a plain edit. `WM_CTLCOLOREDIT` hands
back the *unselected* colours, and there is no message for the other pair --
that is a rich edit's feature, not an edit's. So the selection is repainted
after the fact, in `MakeOpaque`, which is already walking every pixel of the
update rectangle to set the alpha.

Two things make that honest rather than a hack. The selection is found in the
pixels rather than calculated: its background is the highlight colour *exactly*
and nothing else in this window is that colour, so the run between the first and
last exact match on a scanline is the selection -- no walking the control's
lines, its wrapping and its scroll offset. And the mapping is done per channel,
because ClearType puts down a different amount of each; a whole-pixel test
leaves a blue fringe around every letter. The run is grown outwards over the
half-covered pixel at each end, which is a blend and not an exact match, and
skipping that leaves a blue sliver at both ends of every selection.

The one colour this pins down is `colTextSel`. A caret that does not change
colour means a selection background whose inverse is the same near-white the
caret already is over the panes -- which means a grey close to the surface.
Choose a blue there and the orange caret comes straight back.

**56. A control does not do all of its painting in `WM_PAINT`.** An edit
dragging out a selection redraws as the pointer moves, and what it draws that
way never reaches the paint hook -- so it is neither made opaque nor recoloured,
and the line shows the acrylic through until the next real paint arrives. Over a
bright window behind the palette the backdrop measures #565656 against the
pane's #1c1c1c, which is what "the line goes white for a moment" is.

`refreshSurface` re-asserts both after the mouse and key messages the control
handles for itself. It is guarded on there being a selection at all, so the
common case -- every keystroke, every mouse move over a pane -- costs one
message and an `EM_GETSEL`.

The wiring is the fiddly part: windigo keeps only the **last** handler
registered for a message, so a second `WM_MOUSEMOVE` handler would silently
replace the scrollbar's. That is why `wireScrollDrag` takes the refresh as a
callback instead of it being registered alongside, and why the keys are left
alone -- `palette.go` already owns `WM_KEYDOWN` on all four edits, and
`WM_KEYUP`, which nothing wants, is what the refresh hangs off instead.

**57. `HideCaret` before copying pixels out, or the caret is copied with them.**
The caret is drawn by inverting, so a copy that catches one blink of it takes
the inverted pixels as if they were the control's -- and once the colours
underneath are being rewritten, the next blink inverts a colour the caret was
never drawn over and leaves a stray mark that stays there. Hiding it for the
length of the copy costs nothing: both calls are no-ops unless the window owns
the caret.

**58. A checked-in template that the loader cannot read.**
`storage.example/env.json` opened with a `"_comment"` explaining what the file
was for, and the loader read every top-level key as a stage -- so it tried to
decode a string as an environment body and failed. `cp -r storage.example
storage`, which is the documented first step, produced an `env.json` that did
not parse, and the strip said "env.json is broken" before the user had typed
anything. Nothing caught it because the tests all built their own fixtures; the
one file every new user actually starts from was the only one never loaded.

Two fixes, and the second is the one that generalises. Keys beginning with `_`
are now skipped as comments, the same convention `places.json` already carries
-- their value is *decoded and dropped* rather than skipped by token, because
the decoder is a stream and skipping a token would leave it in the middle of a
value. And there is a test that loads the checked-in template itself. Any file
shipped as a starting point should have one.

---

## Removed: the Places tab

**Removed 2026-09-09**, at the user's request. It worked; it had simply been
made redundant by where it sat on screen.

Why it went: the palette overlaps the taskbar, and Places was being used to
open frequently-visited folders in Explorer. That job moved to the
`quick_files` widget on the YASB status bar (the `tool-bar` project), which is
always visible and does not have to be summoned over what you are looking at.

What was deleted:

- `internal/place/` (5 files, ~980 lines) and `internal/ui/places.go` plus its test
- `modePlaces` and every switch arm on it, across `palette.go`, `command.go`,
  `rename.go` and `run.go`
- the right-hand pane it owned: the `pathLabel` static and the `opts` action
  list, with their painting, scrollbar and rounded-corner wiring
- `renameState.isPlace` and `commitPlaceRename`
- the `wmPlaceStat` message and the background path-classification it drove
- `Config.PlacesFile` and `Config.Openers`
- `storage.example/places.json`

`storage/places.json` was **not** deleted -- `storage/` is gitignored user data,
and that file is the record of what the pins were migrated from. It is inert
now and can be removed by hand whenever.

Where the entries went: all 7 were migrated into the widget's pin store at
`%USERPROFILE%\.config\yasb\quick_files\pins.json`. The `open` and `elevate`
fields were carried across rather than dropped, because without them the hosts
file opens unelevated -- which appears to work and then fails on save, the exact
trap `places.example.json` warned about.

Two things to know if this is ever revisited:

- `moveDown` and `atEndOfSearch` were defined in `places.go` but used by every
  mode. They now live in `palette.go`. `moveDown` is a one-line wrapper around
  `moveSelection` and looks pointless: it is kept because every caller uses it
  and it is the seam where a second list used to be chosen.
- Historical prose elsewhere in this file (the gotchas around stat-ing paths and
  hand-edited JSON, and the size measurement) still discusses Places. That is
  kept deliberately as history -- the reasoning was hard won and still applies
  to `macros.json`, which is edited by hand and by the palette in the same way.

## Next steps

1. The six reserved parsers in `scripts/` are the user's to write. Do not.
2. Notes are stored in plain text -- the same exposure as Win+V, organised
   rather than safer. DPAPI encryption for files marked secret is the obvious
   next thing if it is ever wanted.
3. Notes and places both create with `Alt+D`, rename with `F2` and delete with
   `Del`. Nothing in either tab needs Explorer any more.
4. `places.json` still hot-reloads, so editing it by hand and editing it from
   the palette both work and neither fights the other.
5. A place points at one path. Globs, or a place that lists a folder's
   children, would be a different feature; do not grow the JSON schema into one
   without deciding that first.
6. A macro's steps are not editable in the pane -- the right-hand side is a
   read-only rendering, and correcting one means re-recording with `Ctrl+R` or
   editing `macros.json`, which hot-reloads. Making the pane editable would mean
   parsing the rendered form back, which is possible (`macro.ParseChord` is the
   half that exists) but is a feature rather than a gap: decide it deliberately.
7. Mouse events are deliberately not recorded. A click is a screen position, and
   the whole premise is running the same edit on a line the user picked. Do not
   add mouse recording without a reason that survives that argument.
8. The undo watch cannot restore the caret, only the text. Nothing in Win32
   offers a general way to read another application's caret position, so this is
   a limitation to state rather than a thing to fix.

The full approved plan lives at
`C:\Users\ACER\.claude\plans\i-want-you-to-swirling-possum.md`.
