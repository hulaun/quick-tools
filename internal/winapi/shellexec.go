//go:build windows

package winapi

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")
	ole32   = syscall.NewLazyDLL("ole32.dll")

	procShellExecuteW  = shell32.NewProc("ShellExecuteW")
	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
)

const (
	swShowNormal = 1

	// ShellExecute reports failure as a value of 32 or less, and success as a
	// meaningless instance handle above it. It is the one Win32 function whose
	// error convention is a magic number.
	shellExecMinSuccess = 32

	coinitApartmentThreaded = 0x2
	coinitDisableOLE1DDE    = 0x4
)

// comOnce initialises COM for the calling thread.
//
// ShellExecuteW is documented as requiring it, because the verbs it runs are
// resolved through shell handlers that are COM objects. It works without it
// often enough to look unnecessary and then fails on some particular file
// association, which is the worst way to find out.
//
// The thread is the UI thread -- runtime.LockOSThread has already pinned it --
// so initialising once is enough, and a second call from anywhere else returns
// S_FALSE rather than doing harm. An error is ignored deliberately: if the
// framework has already initialised this thread with a different model, the
// answer is RPC_E_CHANGED_MODE and the existing apartment is the right one to
// keep.
var comOnce sync.Once

func initCOM() {
	comOnce.Do(func() {
		procCoInitializeEx.Call(0, coinitApartmentThreaded|coinitDisableOLE1DDE)
	})
}

// shellExecute is the raw call. Every launcher below is a thin wrapper over it.
//
// verb is "open" or "runas"; file is what to launch; args is its command line;
// dir is the working directory the launched program starts in, which matters
// for a terminal and is harmless for everything else.
func shellExecute(verb, file, args, dir string) error {
	initCOM()

	verbPtr, err := utf16OrNil(verb)
	if err != nil {
		return err
	}
	filePtr, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	argsPtr, err := utf16OrNil(args)
	if err != nil {
		return err
	}
	dirPtr, err := utf16OrNil(dir)
	if err != nil {
		return err
	}

	// The conversions happen inside the Call expression on purpose. That is the
	// one form the rules for unsafe.Pointer bless: the pointer stays visible to
	// the garbage collector for the duration of the call, where a uintptr held
	// in a variable first would not.
	r, _, callErr := procShellExecuteW.Call(
		0, // no owner window: the palette hides itself before launching
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(argsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		swShowNormal,
	)
	if r > shellExecMinSuccess {
		return nil
	}
	return shellExecError(uint32(r), callErr)
}

// utf16OrNil converts a string, or returns nil for an empty one -- several of
// ShellExecute's arguments are optional and want a null pointer, not a pointer
// to an empty string.
func utf16OrNil(s string) (*uint16, error) {
	if s == "" {
		return nil, nil
	}
	return syscall.UTF16PtrFromString(s)
}

// shellExecError turns the magic number into something worth showing a user.
//
// The codes that actually happen are worth naming: a path that has been moved,
// a file type with nothing registered to open it, and a UAC prompt the user
// dismissed -- which is a decision, not a failure, and should not be reported
// as one.
func shellExecError(code uint32, callErr error) error {
	switch code {
	case 2, 3: // ERROR_FILE_NOT_FOUND, ERROR_PATH_NOT_FOUND
		return fmt.Errorf("not found")
	case 5: // SE_ERR_ACCESSDENIED
		return fmt.Errorf("access denied")
	case 31: // SE_ERR_NOASSOC
		return fmt.Errorf("no app is registered to open this file type")
	case 1223: // ERROR_CANCELLED -- the elevation prompt was dismissed
		return ErrCancelled
	}
	if callErr != nil && callErr != syscall.Errno(0) {
		return fmt.Errorf("could not launch: %w", callErr)
	}
	return fmt.Errorf("could not launch (shell error %d)", code)
}

// ErrCancelled means the user dismissed the elevation prompt. The caller
// treats it as nothing happening rather than as something going wrong.
var ErrCancelled = fmt.Errorf("cancelled")

// verbFor picks the elevation verb. "runas" is what makes an editor able to
// save to a protected path such as hosts; without it the file opens read-only
// in practice and the save fails at the end, long after it looked fine.
func verbFor(elevate bool) string {
	if elevate {
		return "runas"
	}
	return "open"
}

// Open launches a path with whatever Windows has registered for it: a folder
// opens in Explorer, an executable runs, a .pdf opens in the PDF viewer.
func Open(path string, elevate bool) error {
	return shellExecute(verbFor(elevate), path, "", "")
}

// OpenWith launches a path in a named application.
//
// The working directory is left empty rather than guessed at from the path.
// Whether a path's own folder is the parent or the path itself depends on
// whether it is a file or a folder, which cannot be told from the text -- and
// the caller has already stat'd it. The only launcher that genuinely needs a
// directory is the terminal, which takes it explicitly.
func OpenWith(exe, path string, elevate bool) error {
	return shellExecute(verbFor(elevate), exe, quoteArg(path), "")
}

// Reveal opens Explorer with the item selected, rather than opening the item
// itself. It is what "show in folder" means everywhere else, and the only way
// to get at a file whose own opener is not what is wanted.
func Reveal(path string) error {
	return shellExecute("open", "explorer.exe", "/select,"+quoteArg(path), "")
}

// OpenTerminalAt starts a terminal in a folder.
//
// Windows Terminal takes -d; cmd.exe has no such flag and has to be told to cd
// on the way in, so the two cannot share a command line. Falling back to cmd
// matters because wt.exe is a Store app and is genuinely absent on some
// installs, and a terminal that is missing entirely is a worse answer than a
// plain one.
func OpenTerminalAt(exe, dir string) error {
	if strings.EqualFold(filepath.Base(exe), "wt.exe") {
		return shellExecute("open", exe, "-d "+quoteArg(dir), dir)
	}
	return shellExecute("open", exe, "/K cd /d "+quoteArg(dir), dir)
}

// quoteArg wraps an argument in quotes so a path with a space in it arrives as
// one argument. "C:\Program Files\..." is the entire reason this is needed.
func quoteArg(s string) string {
	if s == "" {
		return ""
	}
	return `"` + strings.Trim(s, `"`) + `"`
}
