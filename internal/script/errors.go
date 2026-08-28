package script

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dop251/goja"
)

// asInterrupted reports whether err is goja's interrupt error.
func asInterrupted(err error, target **goja.InterruptedError) bool {
	return errors.As(err, target)
}

// scriptError turns a goja failure into something a person can act on.
//
// A raw *goja.Exception prints the JavaScript stack, which is mostly noise for
// a twenty-line transform; the message and the line it came from are what
// matter when the error is going to be shown in the preview pane.
func scriptError(err error) error {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		msg := ex.Value().String()
		if stack := ex.String(); stack != "" {
			if line := firstScriptLine(stack); line != "" {
				return fmt.Errorf("%s (%s)", msg, line)
			}
		}
		return errors.New(msg)
	}
	return err
}

// firstScriptLine picks the first "at ..." frame out of a goja stack trace.
func firstScriptLine(stack string) string {
	for _, l := range strings.Split(stack, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "at ") {
			return strings.TrimPrefix(l, "at ")
		}
	}
	return ""
}
