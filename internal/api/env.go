package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Env is the set of named environments from env.json, plus the values a hook
// has set during this session.
//
//	{
//	  "dev":  { "base": "http://localhost:8080", "user": "admin" },
//	  "prod": { "base": "https://api.corp",      "user": "svc"   }
//	}
//
// Two layers, and the second is not an optimisation. A hook writing a token
// straight back into env.json would rewrite the file on every send, which the
// one-second watcher would see as a change, which would reload the file, on
// every request. The overlay also gives a token the right lifetime -- it dies
// with the process -- and means the file you edit by hand is never churned by
// something you did not type.
//
// The file is therefore read-only here. If a value ever genuinely needs to
// outlive the session that is an explicit persist step, added when something
// wants it and not before.
type Env struct {
	names []string                     // file order, which is what Ctrl+E cycles
	file  map[string]map[string]string // as read from disk
	over  map[string]map[string]string // set by hooks, per environment
	act   string
}

// LoadEnv reads env.json. A missing file is not an error: the tab works with no
// environment at all, and every {{var}} simply stays unexpanded.
func LoadEnv(path string) (*Env, error) {
	e := &Env{
		file: map[string]map[string]string{},
		over: map[string]map[string]string{},
	}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return e, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return e, nil
	}

	// Decoded twice: once into ordered raw messages so the environments keep the
	// order they were written in, and once per environment into values. Go maps
	// do not preserve order and Ctrl+E cycling in a random order every restart
	// would be its own small bug.
	var order []string
	dec := json.NewDecoder(bytes.NewReader(raw))

	// UseNumber on the decoder that actually reads the values, so a large
	// integer survives as the text it was written as. Without it
	// 9007199254740993 comes back as a float64 and goes out having lost its last
	// digit -- the same trap the JSON transforms have a test pinning, and an id
	// in an environment is exactly where it would bite.
	dec.UseNumber()

	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for dec.More() {
		// The key is read with Token, not Decode. Interleaving the two is allowed,
		// but Decode reads a whole *value*, and inside an object the next item is
		// a key -- asking Decode for it fails with "not at beginning of value",
		// which says nothing about the cause.
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected an environment name, got %v", path, tok)
		}
		var body map[string]any
		if err := dec.Decode(&body); err != nil {
			return nil, fmt.Errorf("%s: environment %q: %w", path, name, err)
		}
		vals := make(map[string]string, len(body))
		for k, v := range body {
			s, ok := scalar(v)
			if !ok {
				// A nested object or array has no sensible spelling inside a URL or
				// a header, and silently rendering one as "map[]" would be worse
				// than saying so.
				return nil, fmt.Errorf("%s: environment %q: %q is not a string, number or boolean", path, name, k)
			}
			vals[k] = s
		}
		e.file[name] = vals
		order = append(order, name)
	}

	e.names = order
	if len(order) > 0 {
		e.act = order[0]
	}
	return e, nil
}

// Names lists the environments in file order.
func (e *Env) Names() []string { return e.names }

// Active is the environment {{vars}} currently resolve against.
func (e *Env) Active() string { return e.act }

// SetActive selects an environment. An unknown name is ignored rather than
// erroring: env.json hot-reloads, so the name that was selected a second ago
// can legitimately have just been renamed out from under the selection.
func (e *Env) SetActive(name string) {
	if _, ok := e.file[name]; ok || name == "" {
		e.act = name
	}
}

// Next cycles to the following environment, which is what Ctrl+E does.
func (e *Env) Next() {
	if len(e.names) == 0 {
		return
	}
	for i, n := range e.names {
		if n == e.act {
			e.act = e.names[(i+1)%len(e.names)]
			return
		}
	}
	e.act = e.names[0]
}

// Lookup resolves one variable in the active environment: the session overlay
// first, then the file.
func (e *Env) Lookup(key string) (string, bool) {
	if v, ok := e.over[e.act][key]; ok {
		return v, true
	}
	v, ok := e.file[e.act][key]
	return v, ok
}

// Set records a value for the rest of the session. This is what a hook calls,
// and it is scoped to the active environment: a token lifted from dev must not
// still be there after switching to prod.
func (e *Env) Set(key, value string) {
	if e.over[e.act] == nil {
		e.over[e.act] = map[string]string{}
	}
	e.over[e.act][key] = value
}

// Keys lists every variable resolvable in the active environment -- the file's
// and the session's together, without duplicates, sorted.
func (e *Env) Keys() []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]string{e.file[e.act], e.over[e.act]} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Session lists the keys a hook has set in the active environment, sorted, so
// the UI can show what is being carried without implying it is on disk.
func (e *Env) Session() []string {
	keys := make([]string, 0, len(e.over[e.act]))
	for k := range e.over[e.act] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Adopt carries the session overlay across a reload of env.json, so editing the
// file by hand does not throw away the token you just fetched.
func (e *Env) Adopt(old *Env) {
	if old == nil {
		return
	}
	e.over = old.over
	if _, ok := e.file[old.act]; ok {
		e.act = old.act
	}
}

// Expand substitutes {{vars}} and reports, in order and without duplicates, any
// that had no value.
//
// An unknown variable is left exactly as written, for the same reason
// place.expand leaves an unset %VAR% alone: "{{base}}/orders" quietly becoming
// "/orders" would send a real request somewhere wrong, where the unexpanded
// name says what is missing. The caller shows the missing names; nothing is
// silently dropped.
//
// One pass, so a value that itself contains {{...}} is not expanded again. That
// is a deliberate floor rather than a limitation to fix later: recursion here
// buys very little and brings a cycle to detect.
func (e *Env) Expand(s string) (string, []string) {
	var (
		out     strings.Builder
		missing []string
		seen    = map[string]bool{}
		rest    = s
	)
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			break
		}
		end := strings.Index(rest[open:], "}}")
		if end < 0 {
			break
		}
		end += open

		key := strings.TrimSpace(rest[open+2 : end])
		out.WriteString(rest[:open])
		if v, ok := e.Lookup(key); ok {
			out.WriteString(v)
		} else {
			out.WriteString(rest[open : end+2])
			if !seen[key] {
				seen[key] = true
				missing = append(missing, key)
			}
		}
		rest = rest[end+2:]
	}
	out.WriteString(rest)
	return out.String(), missing
}

// ExpandRequest applies Expand to everything in a request that can carry a
// variable: the URL, the headers and the body.
//
// The body is included because it is the useful case -- a login body wants
// {{user}} -- and nothing is escaped on the way in. A value containing a quote
// will break a JSON body, and that is the documented contract: you are writing
// the file and you know what is in it, the same deal the rest of the app makes.
// Escaping would require knowing the body is JSON, which nothing here does.
//
// The hook is deliberately not expanded. It is code, it already has the env
// handed to it as an object, and substituting text into a script before running
// it is how injection bugs are made.
func (e *Env) ExpandRequest(r Request) (Request, []string) {
	var (
		missing []string
		seen    = map[string]bool{}
	)
	note := func(keys []string) {
		for _, k := range keys {
			if !seen[k] {
				seen[k] = true
				missing = append(missing, k)
			}
		}
	}

	out := r
	var keys []string
	out.URL, keys = e.Expand(r.URL)
	note(keys)

	out.Headers = make([]Header, len(r.Headers))
	for i, h := range r.Headers {
		name, nk := e.Expand(h.Name)
		value, vk := e.Expand(h.Value)
		note(nk)
		note(vk)
		out.Headers[i] = Header{Name: name, Value: value}
	}

	out.Body, keys = e.Expand(r.Body)
	note(keys)

	return out, missing
}

// scalar renders a JSON value as the text a header or URL would carry.
func scalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case nil:
		return "", true
	}
	return "", false
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("expected %q at the top level, got %v", want, tok)
	}
	return nil
}
