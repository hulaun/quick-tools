// Package script runs user-written JavaScript transforms.
//
// A script is a plain .js file in the scripts folder. It declares its name and
// optional metadata as globals and defines a transform function:
//
//	var name = "Java toString to JSON";
//	var group = "Json";
//	var tags = ["j2j", "javajson"];
//
//	function transform(input) {
//	    return input.toUpperCase();
//	}
//
// Note this is deliberately not ES module syntax. The engine is goja, which
// implements ECMAScript 5.1 plus parts of later editions but does not parse
// `export` in an ordinary script, so plain globals are the format that works.
package script

import (
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// DefaultTimeout bounds a single transform run.
//
// The point is not performance but recoverability: a while(true) in a script
// being written must not take the palette down with it. goja checks for
// interrupts between statements, so any loop can be broken.
const DefaultTimeout = 2 * time.Second

// Script is one compiled user transform.
type Script struct {
	ID    string
	Name  string
	Group string
	Tags  []string
	Path  string

	// mu serialises access to the runtime. A goja.Runtime is not safe for
	// concurrent use, and preview and apply can otherwise overlap.
	mu      sync.Mutex
	rt      *goja.Runtime
	fn      goja.Callable
	timeout time.Duration
}

// Compile builds a Script from source.
//
// The program is evaluated once here, which is what defines the globals and the
// transform function. Each later run only calls that function, so a script pays
// its parse and setup cost once rather than per keystroke.
func Compile(id, path, source string) (*Script, error) {
	prog, err := goja.Compile(path, source, true)
	if err != nil {
		return nil, fmt.Errorf("could not parse: %w", err)
	}

	rt := goja.New()
	// Scripts transform text. They get no filesystem, no network and no host
	// bindings -- not as a security boundary, but because a transform that needs
	// them is a sign the logic belongs in Go instead.
	if _, err := rt.RunProgram(prog); err != nil {
		return nil, fmt.Errorf("could not run: %w", err)
	}

	fnVal := rt.Get("transform")
	if fnVal == nil || goja.IsUndefined(fnVal) {
		return nil, fmt.Errorf("no transform function: define `function transform(input) { ... }`")
	}
	fn, ok := goja.AssertFunction(fnVal)
	if !ok {
		return nil, fmt.Errorf("`transform` is not a function")
	}

	s := &Script{
		ID:      id,
		Path:    path,
		rt:      rt,
		fn:      fn,
		timeout: DefaultTimeout,
	}

	s.Name = stringGlobal(rt, "name", id)
	s.Group = stringGlobal(rt, "group", "Scripts")
	s.Tags = stringsGlobal(rt, "tags")

	return s, nil
}

// Run executes the transform against input.
func (s *Script) Run(input string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	timer := time.AfterFunc(s.timeout, func() {
		s.rt.Interrupt(fmt.Sprintf("took longer than %s", s.timeout))
	})
	defer timer.Stop()
	// The interrupt flag is sticky: without clearing it, a script that timed out
	// once would fail instantly on every later run.
	defer s.rt.ClearInterrupt()

	v, err := s.fn(goja.Undefined(), s.rt.ToValue(input))
	if err != nil {
		var ie *goja.InterruptedError
		if ok := asInterrupted(err, &ie); ok {
			return "", fmt.Errorf("script timed out after %s -- is there an infinite loop?", s.timeout)
		}
		return "", scriptError(err)
	}
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "", fmt.Errorf("transform returned nothing -- it needs a `return`")
	}
	return v.String(), nil
}

// SetTimeout overrides the run budget, used by tests.
func (s *Script) SetTimeout(d time.Duration) { s.timeout = d }

func stringGlobal(rt *goja.Runtime, key, fallback string) string {
	v := rt.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return fallback
	}
	if s := v.String(); s != "" {
		return s
	}
	return fallback
}

func stringsGlobal(rt *goja.Runtime, key string) []string {
	v := rt.Get(key)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	var out []string
	if err := rt.ExportTo(v, &out); err != nil {
		return nil
	}
	return out
}
