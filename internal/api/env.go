package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// EnvFileName is what an environments file is called, both at the storage root
// and inside a project folder under the requests tree.
const EnvFileName = "env.json"

// envFile is one parsed environments file: the stages it names, in the order
// they were written, and their values.
type envFile struct {
	scope string // "" for the base file; otherwise a folder under the requests root
	path  string
	names []string
	vals  map[string]map[string]string
}

// Env is what the API tab resolves {{vars}} against.
//
// Two things vary, and keeping them apart is the whole design:
//
//   - The *stage* -- local, sit, uat, prod -- is global. Ctrl+E cycles it, and
//     what it means is "put the app on UAT", not "switch this one project".
//   - The *values* are per project. Each folder under the requests tree may
//     hold an env.json naming those same stages with its own hosts in them.
//
// So the cycle is as long as the stage list and stays that length whatever the
// project count. One flat file makes it the *product* of projects and stages
// instead, and most of that product is a state the highlighted request cannot
// meaningfully be in -- cycling from gtos-uat to payments-prod is not a long
// list, it is a wrong one.
//
// Resolution walks from the request's own folder up to the storage root, first
// value found winning, so the root env.json is a base layer for what every
// project shares and a project file overrides only what differs.
//
//	storage/env.json                      { "uat": { "team": "platform" } }
//	storage/requests/gtos/env.json        { "uat": { "base": "https://gtos.uat" } }
//	storage/requests/payments/env.json    { "uat": { "base": "https://pay.uat"  } }
//
// The second layer is the values a hook has set during this session. A hook
// writing a token straight back into env.json would rewrite the file on every
// send, which the one-second watcher would see as a change, which would reload
// the file, on every request. The overlay also gives a token the right lifetime
// -- it dies with the process -- and means the file you edit by hand is never
// churned by something you did not type.
//
// The overlay is keyed by project as well as by stage: a token lifted from
// gtos/uat has no business being visible to a payments/uat request, which is
// the same argument that already scoped it per stage, one level down.
//
// The files are therefore read-only here. If a value ever genuinely needs to
// outlive the session that is an explicit persist step, added when something
// wants it and not before.
type Env struct {
	bpath string              // where the base file is, whether or not it loaded
	base  *envFile            // the storage-root file; nil if absent or broken
	files map[string]*envFile // project files, by folder under the requests root
	root  string              // the requests root on disk, for EditPath

	act   string // the active stage, shared across every project
	scope string // the folder of the request the highlight is on
	owner string // nearest folder at or above scope that has a file; overlay key
	chain []*envFile

	over map[string]map[string]map[string]string // owner -> stage -> key -> value
}

func newEnv() *Env {
	return &Env{
		files: map[string]*envFile{},
		over:  map[string]map[string]map[string]string{},
	}
}

// LoadEnv reads a single environments file as the base layer, with no project
// files under it. A missing file is not an error: the tab works with no
// environment at all, and every {{var}} simply stays unexpanded.
func LoadEnv(path string) (*Env, error) {
	e := newEnv()
	e.bpath = path
	f, err := loadEnvFile(path, "")
	if err != nil {
		return nil, err
	}
	e.base = f
	e.rebuild()
	return e, nil
}

// LoadEnvTree reads the base file plus every env.json under the requests root.
//
// A file that does not parse is skipped rather than fatal, and named in the
// returned error: one project's typo must not stop the others resolving. The
// Env is always usable, so a caller shows the error beside an environment that
// still works rather than choosing between the two.
func LoadEnvTree(basePath, requestsRoot string) (*Env, error) {
	e := newEnv()
	e.bpath = basePath
	e.root = requestsRoot

	var bad []string

	if basePath != "" {
		f, err := loadEnvFile(basePath, "")
		if err != nil {
			bad = append(bad, err.Error())
		} else {
			e.base = f
		}
	}

	if requestsRoot != "" {
		// The walk error is ignored on purpose: a requests folder that is not
		// there yet is the ordinary state before the first request is written,
		// and it is not this function's job to complain about it.
		_ = filepath.WalkDir(requestsRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // one unreadable directory must not abandon the tree
			}
			if d.IsDir() {
				if p != requestsRoot && strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() != EnvFileName {
				return nil
			}
			rel, err := filepath.Rel(requestsRoot, filepath.Dir(p))
			if err != nil {
				return nil
			}
			scope := normScope(filepath.ToSlash(rel))
			f, ferr := loadEnvFile(p, scope)
			if ferr != nil {
				bad = append(bad, ferr.Error())
				return nil
			}
			e.files[scope] = f
			return nil
		})
	}

	e.rebuild()
	if len(bad) > 0 {
		return e, errors.New(strings.Join(bad, "; "))
	}
	return e, nil
}

// loadEnvFile parses one environments file. A file that is not there comes back
// as an empty one, so a caller never has to distinguish "no file" from "no
// environments in it" -- both resolve nothing, which is the same thing.
func loadEnvFile(p, scope string) (*envFile, error) {
	f := &envFile{scope: scope, path: p, vals: map[string]map[string]string{}}

	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return f, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return f, nil
	}

	// Decoded as a stream rather than into a map so the stages keep the order
	// they were written in. Go maps do not preserve order, and Ctrl+E cycling in
	// a different order every restart would be its own small bug.
	dec := json.NewDecoder(bytes.NewReader(raw))

	// UseNumber on the decoder that actually reads the values, so a large
	// integer survives as the text it was written as. Without it
	// 9007199254740993 comes back as a float64 and goes out having lost its last
	// digit -- the same trap the JSON transforms have a test pinning, and an id
	// in an environment is exactly where it would bite.
	dec.UseNumber()

	if err := expectDelim(dec, '{'); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	for dec.More() {
		// The key is read with Token, not Decode. Interleaving the two is allowed,
		// but Decode reads a whole *value*, and inside an object the next item is
		// a key -- asking Decode for it fails with "not at beginning of value",
		// which says nothing about the cause.
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected an environment name, got %v", p, tok)
		}

		// A key starting with an underscore is a note to the reader, not a stage
		// -- the same convention places.json already carries, and the only way to
		// put a comment in a JSON file. Its value is read and dropped, whatever
		// shape it is: the decoder is a stream, so skipping the token would leave
		// it pointing at the middle of a value.
		//
		// This is not decoration. The checked-in template opened with a
		// "_comment" explaining what the file was for, which the loader read as a
		// stage whose body was a string -- so the very first thing anyone got
		// after copying storage.example was an env.json that did not parse.
		if strings.HasPrefix(name, "_") {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("%s: %q: %w", p, name, err)
			}
			continue
		}

		var body map[string]any
		if err := dec.Decode(&body); err != nil {
			return nil, fmt.Errorf("%s: environment %q: %w", p, name, err)
		}
		vals := make(map[string]string, len(body))
		for k, v := range body {
			s, ok := scalar(v)
			if !ok {
				// A nested object or array has no sensible spelling inside a URL or
				// a header, and silently rendering one as "map[]" would be worse
				// than saying so.
				return nil, fmt.Errorf("%s: environment %q: %q is not a string, number or boolean", p, name, k)
			}
			vals[k] = s
		}
		if _, dup := f.vals[name]; !dup {
			f.names = append(f.names, name)
		}
		f.vals[name] = vals
	}
	return f, nil
}

// normScope puts a folder into the form the file map is keyed by: forward
// slashes, no leading or trailing one, and "" for the root -- which is what
// filepath.Rel spells "." and what a request at the top of the tree has.
func normScope(folder string) string {
	folder = strings.Trim(filepath.ToSlash(folder), "/")
	if folder == "." {
		return ""
	}
	return folder
}

// parentScope is the folder above, with "" as the root's own parent.
func parentScope(scope string) string {
	d := path.Dir(scope)
	if d == "." || d == "/" {
		return ""
	}
	return d
}

// rebuild recomputes the layers the current scope resolves through, and picks
// a stage if nothing has selected one yet.
func (e *Env) rebuild() {
	e.chain = e.chain[:0]
	e.owner = ""
	for s := e.scope; ; s = parentScope(s) {
		if f, ok := e.files[s]; ok {
			if len(e.chain) == 0 {
				// The nearest file is the project, and the project is what a hook's
				// values belong to. Folders below it have no file, by definition of
				// nearest, so this is stable as the highlight moves within one.
				e.owner = f.scope
			}
			e.chain = append(e.chain, f)
		}
		if s == "" {
			break
		}
	}
	if e.base != nil {
		e.chain = append(e.chain, e.base)
	}

	if e.act == "" {
		if names := e.Names(); len(names) > 0 {
			e.act = names[0]
		}
	}
}

// SetScope points the environment at the folder the highlighted request lives
// in, relative to the requests root. Everything else -- Lookup, Expand, Keys --
// answers for that scope, so the callers of those are unchanged by any of this.
func (e *Env) SetScope(folder string) {
	folder = normScope(folder)
	if folder == e.scope {
		return
	}
	e.scope = folder
	e.rebuild()
}

// Scope is the folder the environment is currently resolving for.
func (e *Env) Scope() string { return e.scope }

// Project is the folder whose env.json is being used -- the nearest one at or
// above the scope -- or "" when nothing but the base file applies. It is what
// the tab shows beside the stage, since "uat" alone does not say whose.
func (e *Env) Project() string { return e.owner }

// Names lists the stages the current scope can be in, nearest file first and
// in each file's own order, which is what Ctrl+E cycles.
func (e *Env) Names() []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range e.chain {
		for _, n := range f.names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	return out
}

// Active is the stage {{vars}} currently resolve against.
func (e *Env) Active() string { return e.act }

// Defined reports whether the active stage exists in any layer the current
// scope resolves through. A project with no "sit" in it is worth saying so
// about: the stage is global, so it is reachable from a project that has never
// heard of it, and silently resolving nothing would look like broken
// substitution rather than a missing block.
func (e *Env) Defined() bool {
	if e.act == "" {
		return false
	}
	for _, f := range e.chain {
		if _, ok := f.vals[e.act]; ok {
			return true
		}
	}
	return false
}

// SetActive selects a stage. A name no file defines is ignored rather than
// erroring: the files hot-reload, so the name that was selected a second ago
// can legitimately have just been renamed out from under the selection.
func (e *Env) SetActive(name string) {
	if name == "" || e.known(name) {
		e.act = name
	}
}

// known is whether any loaded file names this stage -- not just the ones the
// current scope resolves through. The stage is global, so selecting one that
// only another project defines is allowed; Defined is what reports that it does
// not apply here.
func (e *Env) known(name string) bool {
	if e.base != nil {
		if _, ok := e.base.vals[name]; ok {
			return true
		}
	}
	for _, f := range e.files {
		if _, ok := f.vals[name]; ok {
			return true
		}
	}
	return false
}

// Next cycles to the following stage, which is what Ctrl+E does.
func (e *Env) Next() {
	names := e.Names()
	if len(names) == 0 {
		return
	}
	for i, n := range names {
		if n == e.act {
			e.act = names[(i+1)%len(names)]
			return
		}
	}
	e.act = names[0]
}

// Lookup resolves one variable: the session overlay first, then each file from
// the request's own folder upwards, ending at the base.
func (e *Env) Lookup(key string) (string, bool) {
	if v, ok := e.over[e.owner][e.act][key]; ok {
		return v, true
	}
	for _, f := range e.chain {
		if v, ok := f.vals[e.act][key]; ok {
			return v, true
		}
	}
	return "", false
}

// Set records a value for the rest of the session. This is what a hook calls,
// and it is scoped to the active stage *of the current project*: a token lifted
// from gtos on uat must not still be there for payments, nor after switching to
// prod.
func (e *Env) Set(key, value string) {
	if e.over[e.owner] == nil {
		e.over[e.owner] = map[string]map[string]string{}
	}
	if e.over[e.owner][e.act] == nil {
		e.over[e.owner][e.act] = map[string]string{}
	}
	e.over[e.owner][e.act][key] = value
}

// Keys lists every variable resolvable here -- every layer's and the session's
// together, without duplicates, sorted.
func (e *Env) Keys() []string {
	seen := map[string]bool{}
	var out []string
	add := func(m map[string]string) {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	add(e.over[e.owner][e.act])
	for _, f := range e.chain {
		add(f.vals[e.act])
	}
	sort.Strings(out)
	return out
}

// Session lists the keys a hook has set here, sorted, so the UI can show what
// is being carried without implying it is on disk.
func (e *Env) Session() []string {
	m := e.over[e.owner][e.act]
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Adopt carries the session overlay, the selected stage and the current scope
// across a reload, so editing a file by hand does not throw away the token you
// just fetched or move the highlight's environment out from under it.
func (e *Env) Adopt(old *Env) {
	if old == nil {
		return
	}
	e.over = old.over
	e.scope = old.scope
	e.act = ""
	if old.act != "" && e.known(old.act) {
		e.act = old.act
	}
	e.rebuild()
}

// EditPath is the file to open for a request in the given folder: the nearest
// env.json at or above it, or -- when the project does not have one yet -- the
// path its own would go at, so the first edit in a new project offers to create
// it in the right place rather than sending you to the shared file.
func (e *Env) EditPath(folder string) string {
	folder = normScope(folder)
	for s := folder; ; s = parentScope(s) {
		if f, ok := e.files[s]; ok {
			return f.path
		}
		if s == "" {
			break
		}
	}
	if folder != "" && e.root != "" {
		return filepath.Join(e.root, filepath.FromSlash(folder), EnvFileName)
	}
	return e.bpath
}

// Expand substitutes {{vars}} and reports, in order and without duplicates, any
// that had no value.
//
// An unknown variable is left exactly as written, on the same reasoning that
// leaves an unset %VAR% alone when expanding a path: "{{base}}/orders" quietly
// becoming "/orders" would send a real request somewhere wrong, where the
// unexpanded name says what is missing. The caller shows the missing names; nothing is
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

// fileValue reads a value straight out of the layers on disk, bypassing the
// session overlay. Only the tests use it -- to assert that a hook setting a
// value never writes through to the file.
func (e *Env) fileValue(stage, key string) (string, bool) {
	for _, f := range e.chain {
		if v, ok := f.vals[stage][key]; ok {
			return v, true
		}
	}
	return "", false
}
