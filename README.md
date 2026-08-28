# quick-tools

A resident command palette for Windows. Copy some text, press a hotkey, pick a
transform, and the result is on your clipboard.

Written in Go with no cgo, so it builds with nothing but the Go toolchain.

```
Ctrl+Alt+Space
┌──────────────────────────────────────────────────────┐
│ case                                                 │
├───────────────────────┬──────────────────────────────┤
│ Cases: Camel case     │ userNameExample              │
│ Cases: Snake case     │                              │
│ Cases: Title case     │                              │
│ work: db-conn         │                              │
└───────────────────────┴──────────────────────────────┘
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

## Using it

| | |
|---|---|
| `Ctrl+Alt+Space` | open the palette |
| type | fuzzy-search transforms and snippets |
| `↑` `↓` | move; the preview updates against your clipboard |
| `Enter` | put the result on the clipboard and close |
| `Esc` / click away | dismiss |

Right-click the tray icon for **Paste automatically** (send `Ctrl+V` for you
after Enter), **Start with Windows**, and **Quit**.

On Windows 11 a new tray icon starts hidden behind the `^` chevron — drag it
out to pin it.

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
`<name>.test.json` beside a script and `quicktools.exe -test-scripts` runs it.

See [`scripts/README.md`](scripts/README.md) for the format, the type rules and
the parsers still to be written.

## Snippets

Text files under `snippets/` are searchable in the same palette, and `Enter`
copies the contents. The folder becomes the group, so `work/vpn.conf` shows as
`work: vpn`.

```
snippets/
  work/
    db-conn.txt
    vpn.conf
  personal/
    ssh-key.txt
```

They are ordinary files, so they can be edited anywhere, backed up, or synced.

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
  "snippetsDir": "snippets"
}
```

Relative folders resolve next to the executable, so the whole thing stays
portable — copy the folder and your transforms come with it.

## Building

```powershell
.\build.ps1              # release: vet, test, no console
.\build.ps1 -Dev         # with a console, for debugging
.\build.ps1 -Resources   # also regenerate the icon and manifest
```

Or directly:

```
go build -trimpath -ldflags "-H windowsgui -s -w" -o quicktools.exe ./cmd/quicktools
```

`cmd/m0` is the original de-risk spike — a no-UI program that exercises the
Win32 clipboard, focus and paste path. `bin/m0.exe -selftest` still works as a
diagnostic if the clipboard ever misbehaves.

## Limitations

- **Windows only**, by design — it is built on Win32 directly.
- An **elevated** target window will refuse synthesised input from a
  non-elevated app. That is a Windows rule (UIPI), not a bug.
- Rounded corners and the dark border need **Windows 11**; on 10 the window is
  square and otherwise identical.
- The list flickers slightly at the moment its scrollbar appears or disappears.
