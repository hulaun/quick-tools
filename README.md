# quick-tools

A resident command palette for Windows, with a notes store, saved paths,
keystroke macros and an HTTP client in the same window.
Copy some text, press a hotkey, pick a transform, and the result is on your
clipboard -- or press the hotkey again and you are in your configs, logins and
commands, in folders, editable in place.

Written in Go with no cgo, so it builds with nothing but the Go toolchain.

```
Ctrl+Alt+Space                        Ctrl+Alt+Space again
┌──────────────────────────────────┐  ┌──────────────────────────────────┐
│ [Transforms]  Notes              │  │  Transforms  [Notes]             │
│ case                             │  │ vpn                              │
├──────────────────┬───────────────┤  ├──────────────────┬───────────────┤
│ Cases: Camel case│userNameExample│  │ work/            │host 10.0.0.1  │
│ Cases: Snake case│               │  │ work: vpn        │user admin_    │
│ Cases: Title case│               │  │ work: db-conn    │               │
└──────────────────┴───────────────┘  └──────────────────┴───────────────┘
        run it with Enter                 edit it, Ctrl+S to save
```

## Why

Three things done by hand, constantly:

- Reshaping copied text — a Java object into JSON, `\` paths into `/`,
  switching between camel, snake and upper case.
- Storing configs and logins in Win+V, which is fast but a flat list with no
  folders, so it cannot be maintained.
- Wanting a notepad smaller and faster than Notepad.

## Speed

The window opens in about one frame. That is not because the app is fast to
start — it is because it never starts. It runs resident in the tray from login
with the palette window already built but hidden, and the hotkey is a
`ShowWindow` call on a window that already exists.

Notes do not change that. The folder is walked in the background, not when you
press the hotkey, and only the one note you highlight is ever read. Measured
with 500 notes on disk: **26–31 ms** to open, **17–45 ms** to switch tabs.

## Footprint

About **7.5 MB** of RAM sitting in the tray (private working set), and a
16 MB executable with no runtime to install. Four megabytes of that are Go's
HTTP and TLS stacks, linked in for the API tab.

## Using it

| | |
|---|---|
| `Ctrl+Alt+Space` | open the palette; press it again to cycle Transforms → Notes → Macros → API |
| type | fuzzy-search whichever list you are in |
| `↑` `↓` | move; the right pane updates |
| `Tab` | carry on from what is highlighted — see each tab below |
| `Enter` | put the result -- or the note -- on the clipboard and close |
| `Esc` / click away | dismiss |
| `Ctrl+Backspace` | delete the word before the caret (`Ctrl+Delete` for the one after) |

In **Transforms**:

| | |
|---|---|
| `Tab` | **chain**: run the highlighted transform and feed the result into the next one |
| `Backspace` on an empty box | undo the last chained step |

In **Notes**:

| | |
|---|---|
| `Tab` | move between the search box and the editor |
| `Alt+D` | new note, named from what you typed in the search box |
| `Shift+Alt+D` | new folder, same |
| `F2` | rename |
| `Del` | delete the note or folder — permanently, so it asks first |
| `Ctrl+S` | save |

In **Macros**:

| | |
|---|---|
| `Enter` | replay the highlighted macro into the window you came from |
| `Alt+D` | record a new one — the palette hides, you do the edit once, `Ctrl+Alt+Space` stops it |
| `Ctrl+R` | record over the highlighted macro, keeping its name and tuning |
| `F2` | rename |
| `Del` | delete the macro — it asks first |

In **API**:

| | |
|---|---|
| `Enter` | send the highlighted request |
| `Ctrl+Enter` / `F5` | send from anywhere, including inside a pane |
| `Esc` | cancel a request in flight; otherwise back to the list, then close |
| `Tab` | move between the list, the request and the response |
| `Ctrl+E` | switch environment — the active one is shown beside the tabs |
| `Ctrl+Shift+E` | edit `env.json` in the request pane; press it again to go back |
| `Ctrl+Shift+C` | copy the request as a `curl` line, variables filled in |
| `Ctrl+S` | save the request (or the environments) |
| `Alt+D` | new request, named from what you typed in the search box |
| `Shift+Alt+D` | new folder, same |
| `F2` | rename |
| `Del` | delete the request or folder — permanently, so it asks first |

### Carrying a token between requests

A request can end with a block that runs after the response arrives. It is the
answer to logging in once and then not pasting a bearer token into six other
requests by hand.

```http
# @name Login
POST {{base}}/auth/login
Content-Type: application/json

{ "username": "{{user}}", "password": "hunter2" }

> {%
  env.token = response.json().accessToken;
%}
```

Then any later request just uses it:

```http
GET {{base}}/auth/me
Authorization: Bearer {{token}}
```

The status strip says `201 Created  9ms  63 B  set token` so you can see it
happened. The hook gets `response.status`, `.statusText`, `.body`, `.json()`,
`.header(name)` (case-insensitive), `.headers`, `.elapsed` and `.size`, and it
runs on the same JavaScript engine as the transforms with the same two-second
timeout — an accidental infinite loop shows an error rather than freezing the
palette.

**What a hook sets lives in memory, not in `env.json`.** It lasts until you quit,
it is scoped to the environment that was active at the time — a token fetched
against dev is not in scope after `Ctrl+E` to prod — and the file you edit by
hand is never rewritten underneath you.

### Taking a request somewhere else

`Ctrl+Shift+C` puts the highlighted request on the clipboard as a `curl` line,
with the variables already filled in:

```sh
curl -X POST 'http://api.corp/auth/login' \
  -H 'Content-Type: application/json' \
  --data-raw '{"username": "admin", "password": "hunter2"}'
```

That is what makes the tab useful when you are debugging on a machine it does
not run on: compose the request here, copy it, paste it into the ssh session.
It also means you see the curl form of everything you build, which is how the
flags stick.

Backslash continuations, so it is a bash line — join it up for cmd or
PowerShell.

A new note or folder lands inside whatever is highlighted, so the folder rows
double as the place-picker. It is created straight away and the name box opens
on its row with the placeholder selected — type the name and press Enter, or
press Esc and keep "New note". If you had already typed something in the search
box, that is the name it starts with.

Renaming a note keeps its extension unless you type one, so `db-conn.txt`
renamed to `prod-db` is `prod-db.txt`. A rename onto a name already in use is
refused rather than silently numbered — that path holds real files.

Unsaved edits are marked with a `*` on the tab, and are saved for you if you
move to another note or close the window — closing is never a way to lose text.

Right-click the tray icon for **Paste automatically** (send `Ctrl+V` for you
after Enter), **Start with Windows**, and **Quit**.

On Windows 11 a new tray icon starts hidden behind the `^` chevron — drag it
out to pin it.

## Macros

The fourth tab, for the edit you have to make on twenty scattered lines and
cannot make with a selection. Put the caret on the line, press `Enter`, and the
same keystrokes happen again exactly as they did the first time.

Recording is one pass. `Alt+D` hides the palette and hands focus back to
whatever you were editing; you do the edit once, for real; `Ctrl+Alt+Space`
stops the recording. What was captured lands in the pane in a form you can read
before you trust it:

```
 Transforms   Notes  [Macros]   API
 quote
┌──────────────────────┬─────────────────────────────────┐
│ Editing: Quote third │ Home                            │
│ Editing: Wrap line   │ Ctrl+Right                      │
│ SQL: Values to rows  │ Ctrl+Right                      │
│                      │ Shift+Alt+Right                 │
│                      │ type "\""                       │
│                      │                                 │
│                      │ -- one Ctrl+Z undoes it         │
└──────────────────────┴─────────────────────────────────┘
```

Only the keys are recorded, never the mouse. That is deliberate: a click is a
screen position, and the whole point is to run the same edit on a line you
picked yourself.

### Undoing one

Most macros are a single change however many keys they took — navigating is
free, and an editor groups a run of typing into one undo. So for the macro
above, your own `Ctrl+Z` already puts the line back and the palette stays out
of it.

When a macro makes more than one change, the palette catches the first `Ctrl+Z`
in the twenty seconds after a replay and turns it into as many presses as the
macro needs, so one press still undoes the whole thing. Any other key calls it
off, and the pane says which case a macro is in before you run it. Set
`"macroUndo": false` to turn that off and have `Ctrl+Z` always mean exactly one
undo; set `"undo"` on a macro in `macros.json` when the count is wrong, which it
can be — what an application groups into one undo is its own decision and there
is no way to ask.

The caret is not restored. Undo puts the *text* back; where the cursor was
before the macro ran is not something any application will tell us.

### What it cannot do

Windows refuses synthesised input to a window running as administrator unless
we are too. That is an OS rule (UIPI), not a bug — a macro replayed into an
elevated editor silently does nothing.

Macros live in `storage/macros.json`, which hot-reloads, so one can be corrected
by hand as easily as re-recorded. See
[`storage.example/macros.json`](storage.example/macros.json) for the shape.

## Chaining transforms

One transform is often not the whole job. A Java object has to become JSON, and
then that JSON has to become an insert statement.

Press `Tab` to run the highlighted transform and hand its result to the next
one. The palette stays open, the search box clears, and the strip beside the
tabs shows what has been applied so far:

```
 Transforms   Notes   Macros   API    Java to JSON > Pretty JSON >
```

`Enter` ends the chain: it runs the highlighted transform against everything
built up so far and puts *that* on the clipboard. `Backspace` on an empty search
box undoes the last step, and `Esc` abandons the whole thing -- nothing is
written to the clipboard until Enter, so a chain you thought better of leaves it
untouched.

Chaining used to be on `Space`, which meant the search box could only type a
space with `Shift` held. `Tab` costs nothing to bind, and reads the same way in
every tab that has somewhere to carry on to: carry on from what is highlighted.

## What is built in

About 40 transforms across **Cases** (camel, Pascal, snake, screaming, kebab,
title, upper, lower), **Paths** (`\`↔`/`, escaping, `file://`, basename,
dirname), **Json** (pretty, minify, escape, sort keys), **Encoding** (URL,
Base64, HTML entities, hex, JWT decode), **Lines** (trim, dedupe, sort,
reverse, join, number, SQL `IN` list) and **Misc** (UUID, timestamps, strip
ANSI).

## Your own transforms

Drop a `.js` file in `scripts/`. It appears in the palette within a second — no
rebuild, no restart.

```js
var name  = "Shout";
var group = "Text";
var tags  = ["upper"];

function transform(input) {
    return input.toUpperCase() + "!";
}
```

Scripts run on [goja](https://github.com/dop251/goja) with a 2-second timeout,
so a runaway loop shows an error instead of freezing the palette. Put a
`<name>.test.json` beside a script and `quicktools.exe -test-scripts` runs it;
`-only <text>` narrows that to one script or one case and prints the values in
full.

See [`scripts/README.md`](scripts/README.md) for the format, the shared rules and
the parsers still to be written.

## Notes

The second tab. Text files under `storage/snippets/` in whatever folders you like,
searchable, editable in the right-hand pane, and `Enter` copies one to the
clipboard -- Win+V with folders and an editor.

```
storage/snippets/
  work/
    db-conn.txt
    vpn.conf
  personal/
    ssh-key.txt
```

They are ordinary files, so they can be edited anywhere, backed up, or synced.
Edits made outside the app show up within a second. Saving writes LF line
endings whatever the editor hands back.

**They are stored in plain text.** That is the same exposure as Win+V today —
organised, not safer. Anything running as you can read them. Encrypting files
marked secret with DPAPI is a planned improvement, not a shipped one.

## Configuration

`%APPDATA%\quick-tools\config.json`:

```json
{
  "hotkey": "Ctrl+Alt+Space",
  "autoPaste": false,
  "scriptsDir": "scripts",
  "snippetsDir": "storage/snippets",
  "macrosFile": "storage/macros.json",
  "macroUndo": true,
  "requestsDir": "storage/requests",
  "envFile": "storage/env.json"
}
```

Everything the app accumulates lives under `storage/` — notes, saved requests,
named paths, recorded macros and their environments. One folder to back up, sync, or keep in a
private repo, and one line in `.gitignore`. `storage.example/` is the
checked-in template:

```
quick-tools/
  bin/quicktools.exe
  scripts/            your .js transforms
  storage/
    snippets/         notes
    requests/         .http requests
    macros.json
    env.json
```

Relative paths resolve against the folder holding the exe — except that a `bin`
folder is stepped out of, which is what lets the binary live in `bin/` while
`storage/` sits beside it. So the whole thing stays portable: copy the folder
and everything comes with it.

## Building

```powershell
.\build.ps1              # release: vet, test, no console
.\build.ps1 -Dev         # with a console, for debugging
.\build.ps1 -Resources   # also regenerate the icon and manifest
```

Or directly:

```
go build -trimpath -ldflags "-H windowsgui -s -w" -o bin/quicktools.exe ./cmd/quicktools
```

`./restart.sh` is the edit-run loop: it quits the running copy, rebuilds and
relaunches detached, in that order — Windows will not let Go overwrite a
running exe.

## Limitations

- **Windows only**, by design — it is built on Win32 directly.
- An **elevated** target window will refuse synthesised input from a
  non-elevated app. That is a Windows rule (UIPI), not a bug.
- Rounded corners and the dark border need **Windows 11**; on 10 the window is
  square and otherwise identical.
- The list flickers slightly at the moment its scrollbar appears or disappears.
