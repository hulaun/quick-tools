package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// HookTimeout bounds one post-response hook.
//
// The same two seconds a transform gets, and for the same reason: the point is
// not performance but recoverability. goja checks for interrupts between
// statements, so a while(true) written by mistake is survivable rather than
// fatal to the palette.
const HookTimeout = 2 * time.Second

// RunHook executes a request's "> {% ... %}" block against a response.
//
// The script sees two objects:
//
//	response.status      201
//	response.statusText  "Created"
//	response.body        the raw text
//	response.json()      the body parsed, throwing if it is not JSON
//	response.header(n)   one header, matched without regard to case
//	response.headers     every header, first value each
//	response.elapsed     milliseconds
//
//	env.token = "..."    sets a variable for later requests
//	env.base             reads one
//
// It returns the names of the variables the hook set, in order, so the caller
// can say what was carried without having to diff anything itself.
//
// The environment is handed over as a plain object that is read back
// afterwards, rather than as a host object with setters. That is what makes
// `env.token = x` work as ordinary JavaScript, which is the whole point -- a
// hook is meant to look like the two lines you would write in a console.
//
// The consequence, worth knowing: deleting a key from env does nothing. Only
// values that appear changed or new are written back. A hook that wants to
// clear something sets it to "".
func RunHook(src string, resp Response, env *Env) ([]string, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	if env == nil {
		return nil, errors.New("no environment to write to")
	}

	rt := goja.New()

	// Seeded with everything currently resolvable, so a hook can read what it
	// is about to build on -- env.base, say, to construct a follow-up URL.
	before := map[string]string{}
	for _, k := range env.Keys() {
		if v, ok := env.Lookup(k); ok {
			before[k] = v
		}
	}
	envObj := rt.NewObject()
	for k, v := range before {
		if err := envObj.Set(k, v); err != nil {
			return nil, err
		}
	}
	if err := rt.Set("env", envObj); err != nil {
		return nil, err
	}

	respObj, err := responseObject(rt, resp)
	if err != nil {
		return nil, err
	}
	if err := rt.Set("response", respObj); err != nil {
		return nil, err
	}

	timer := time.AfterFunc(HookTimeout, func() {
		rt.Interrupt(fmt.Sprintf("took longer than %s", HookTimeout))
	})
	defer timer.Stop()

	// Wrapped in a function so that `return` is legal in a hook and so that a
	// `var` declared inside it does not leak into the globals the next run
	// would see. The name is what appears in an error's stack line.
	if _, err := rt.RunScript("hook", "(function(){\n"+src+"\n})()"); err != nil {
		var ie *goja.InterruptedError
		if errors.As(err, &ie) {
			return nil, fmt.Errorf("the hook timed out after %s -- is there an infinite loop?", HookTimeout)
		}
		return nil, hookError(err)
	}

	// Read the object back and write the differences into the session overlay.
	var changed []string
	for _, k := range envObj.Keys() {
		v := envObj.Get(k)
		if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
			continue
		}
		s := v.String()
		if old, had := before[k]; had && old == s {
			continue
		}
		env.Set(k, s)
		changed = append(changed, k)
	}
	sort.Strings(changed)
	return changed, nil
}

// responseObject builds the `response` the hook sees.
func responseObject(rt *goja.Runtime, resp Response) (*goja.Object, error) {
	obj := rt.NewObject()

	set := func(k string, v any) error { return obj.Set(k, v) }
	if err := errFirst(
		set("status", resp.Status),
		set("statusText", resp.StatusText),
		set("body", resp.Body),
		set("elapsed", resp.Elapsed.Milliseconds()),
		set("size", resp.Size),
		set("truncated", resp.Truncated),
	); err != nil {
		return nil, err
	}

	headers := rt.NewObject()
	flat := map[string]string{}
	for name, values := range resp.Header {
		if len(values) == 0 {
			continue
		}
		if err := headers.Set(name, values[0]); err != nil {
			return nil, err
		}
		flat[strings.ToLower(name)] = values[0]
	}
	if err := obj.Set("headers", headers); err != nil {
		return nil, err
	}

	// A case-insensitive accessor, because HTTP header names are, and a hook
	// asking for "content-type" should not miss "Content-Type".
	if err := obj.Set("header", func(name string) string {
		return flat[strings.ToLower(name)]
	}); err != nil {
		return nil, err
	}

	// json() rather than a pre-parsed field: most responses are never parsed by
	// a hook, and a body that is not JSON should be an error only when someone
	// actually asks for it as JSON.
	if err := obj.Set("json", func() (any, error) {
		var v any
		dec := json.NewDecoder(strings.NewReader(resp.Body))
		// UseNumber for the same reason it is used everywhere else here: an id
		// that does not fit in a float64 must survive being read.
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("the response body is not JSON: %w", err)
		}
		return v, nil
	}); err != nil {
		return nil, err
	}

	return obj, nil
}

// hookError turns a goja failure into one line worth showing in the status
// strip.
//
// The same shape as the transform runner's, kept separate rather than shared:
// exporting it from internal/script would make this package depend on that one
// for twelve lines, and the two are free to diverge -- a hook's errors are read
// in a one-line strip, a transform's in a whole pane.
func hookError(err error) error {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		msg := ex.Value().String()
		for _, l := range strings.Split(ex.String(), "\n") {
			if l = strings.TrimSpace(l); strings.HasPrefix(l, "at ") {
				return fmt.Errorf("%s (%s)", msg, strings.TrimPrefix(l, "at "))
			}
		}
		return errors.New(msg)
	}
	return err
}

// errFirst returns the first non-nil error, so a run of Set calls that cannot
// realistically fail does not need six identical checks.
func errFirst(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
