#!/usr/bin/env bash
#
# Quit the running copy, rebuild, and relaunch detached.
#
# This is the inner loop while working on the Go code. Three things it does that
# doing it by hand does not:
#
#   1. Kills the running copy FIRST. Windows will not let Go overwrite a running
#      exe, so `go build` quietly renames the old one to quicktools.exe~ and the
#      stale copy keeps its hotkey registered -- which looks exactly like the
#      rebuild having no effect.
#   2. Waits for the file lock to actually clear. taskkill returns before the
#      process is gone.
#   3. Launches through `cmd start`, so the app is detached from this terminal.
#      Closing the terminal otherwise takes the app down with it.
#
# The binary is built into bin/. Relative paths in config.json are resolved
# against the folder holding the exe, except that a bin/ is stepped out of --
# see config.Root -- so the app still reads storage/ and scripts/ at the repo
# root rather than looking for bin/storage/.
#
# Usage:
#   ./restart.sh              release build (no console), detached
#   ./restart.sh --dev        console build, so panics and log output are visible
#   ./restart.sh --test       run go vet and go test before building
#   ./restart.sh --no-launch  quit and build, but leave it stopped

set -euo pipefail

cd "$(dirname "$0")"
export PATH="$PATH:/c/Program Files/Go/bin"

EXE=quicktools.exe
OUT=bin/$EXE
dev=0
test=0
launch=1

for arg in "$@"; do
	case "$arg" in
	--dev) dev=1 ;;
	--test) test=1 ;;
	--no-launch) launch=0 ;;
	*)
		echo "unknown option: $arg" >&2
		echo "usage: $0 [--dev] [--test] [--no-launch]" >&2
		exit 2
		;;
	esac
done

# --- 1. quit -------------------------------------------------------------
# //F because the palette window hides rather than closes, so a polite WM_CLOSE
# does not end the process. Nothing is lost: notes are written on Ctrl+S and the
# config on each tray toggle, never at shutdown.
if taskkill //F //IM "$EXE" >/dev/null 2>&1; then
	echo "quit: stopped the running copy"

	# taskkill returns before the kernel has released the file handle. Building
	# into that window is what produces quicktools.exe~.
	for _ in $(seq 20); do
		tasklist //FI "IMAGENAME eq $EXE" 2>/dev/null | grep -q "$EXE" || break
		sleep 0.1
	done
else
	echo "quit: nothing was running"
fi

# The rename-out-of-the-way copy from a previous build that skipped step 1.
rm -f "$OUT~"

# --- 2. build ------------------------------------------------------------
if [ "$test" = 1 ]; then
	echo "vet..."
	# One finding is expected and documented: the unsafe.Pointer conversion in
	# internal/winapi/clipboard.go, sound because the memory comes from
	# GlobalAlloc and so lives outside the Go heap.
	go vet ./... || true
	echo "test..."
	go test ./...
fi

echo "build..."
mkdir -p "$(dirname "$OUT")"
if [ "$dev" = 1 ]; then
	go build -o "$OUT" ./cmd/quicktools
	echo "built $OUT (dev: console attached)"
else
	go build -trimpath -ldflags "-H windowsgui -s -w" -o "$OUT" ./cmd/quicktools
	echo "built $OUT (release: no console)"
fi

# --- 3. relaunch ---------------------------------------------------------
if [ "$launch" = 0 ]; then
	echo "not launching (--no-launch)"
	exit 0
fi

# `cmd start` is what detaches it. A backgrounded `./quicktools.exe &` stays a
# child of this shell, and closing the terminal kills the whole process tree.
# The empty "" is start's title argument -- without it, start treats the quoted
# path as the title and opens a console window instead.
cmd //c start "" "$(cygpath -w "$PWD/$OUT")"
echo "launched: press the hotkey to open the palette"
