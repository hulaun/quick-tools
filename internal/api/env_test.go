package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, body string) *Env {
	t.Helper()
	e, err := LoadEnv(write(t, body))
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	return e
}

const twoEnvs = `{
  "dev":  { "base": "http://localhost:8080", "user": "admin" },
  "prod": { "base": "https://api.corp",      "user": "svc"   }
}`

// TestLoadEnvKeepsFileOrder pins that Ctrl+E cycles in the order the file was
// written in. A Go map has no order, so without this the cycle would be a
// different one every restart.
func TestLoadEnvKeepsFileOrder(t *testing.T) {
	e := load(t, twoEnvs)
	if got := e.Names(); len(got) != 2 || got[0] != "dev" || got[1] != "prod" {
		t.Fatalf("Names() = %v, want [dev prod]", got)
	}
	if e.Active() != "dev" {
		t.Errorf("Active() = %q, want the first environment", e.Active())
	}
	e.Next()
	if e.Active() != "prod" {
		t.Errorf("after Next, Active() = %q, want prod", e.Active())
	}
	e.Next()
	if e.Active() != "dev" {
		t.Errorf("Next wraps to %q, want dev", e.Active())
	}
}

// TestMissingFileIsNotAnError: the tab has to work before you have written an
// env.json, with every variable simply left unexpanded.
func TestMissingFileIsNotAnError(t *testing.T) {
	e, err := LoadEnv(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("LoadEnv on a missing file: %v", err)
	}
	got, missing := e.Expand("{{base}}/x")
	if got != "{{base}}/x" {
		t.Errorf("Expand = %q, want it left alone", got)
	}
	if len(missing) != 1 || missing[0] != "base" {
		t.Errorf("missing = %v, want [base]", missing)
	}
}

func TestExpand(t *testing.T) {
	e := load(t, twoEnvs)

	cases := []struct {
		name, in, want string
		missing        []string
	}{
		{name: "one variable", in: "{{base}}/orders", want: "http://localhost:8080/orders"},
		{name: "several", in: "{{base}}/u/{{user}}", want: "http://localhost:8080/u/admin"},
		{name: "none", in: "/plain/path", want: "/plain/path"},
		{name: "spaces inside the braces", in: "{{ base }}/x", want: "http://localhost:8080/x"},
		{
			name: "an unknown one is left as written",
			in:   "{{base}}/{{nope}}", want: "http://localhost:8080/{{nope}}",
			missing: []string{"nope"},
		},
		{
			name: "the same unknown twice is reported once",
			in:   "{{a}}/{{a}}", want: "{{a}}/{{a}}",
			missing: []string{"a"},
		},
		{name: "an unclosed brace is left alone", in: "{{base", want: "{{base"},
		{name: "empty", in: "", want: ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, missing := e.Expand(c.in)
			if got != c.want {
				t.Errorf("Expand(%q) = %q, want %q", c.in, got, c.want)
			}
			if strings.Join(missing, ",") != strings.Join(c.missing, ",") {
				t.Errorf("missing = %v, want %v", missing, c.missing)
			}
		})
	}
}

// TestOverlayWinsOverFile is the whole point of the two layers: a token a hook
// lifted out of a response has to beat whatever the file said.
func TestOverlayWinsOverFile(t *testing.T) {
	e := load(t, twoEnvs)
	e.Set("user", "from-the-hook")

	got, _ := e.Expand("{{user}}")
	if got != "from-the-hook" {
		t.Errorf("Expand(user) = %q, want the overlay value", got)
	}
	if v, _ := e.fileValue("dev", "user"); v != "admin" {
		t.Error("Set wrote through to the file layer; it must not")
	}
}

// TestOverlayIsPerEnvironment is the safety property. A token fetched from dev
// must not still be in scope after switching to prod -- that is a request sent
// to production with a development credential, which is exactly the mistake
// this tab could otherwise make easy.
func TestOverlayIsPerEnvironment(t *testing.T) {
	e := load(t, twoEnvs)
	e.Set("token", "dev-token")

	e.SetActive("prod")
	if v, ok := e.Lookup("token"); ok {
		t.Fatalf("the dev token leaked into prod as %q", v)
	}

	e.Set("token", "prod-token")
	e.SetActive("dev")
	if v, _ := e.Lookup("token"); v != "dev-token" {
		t.Errorf("dev token = %q, want it kept separately", v)
	}
}

// TestAdoptCarriesTheSessionAcrossAReload: env.json hot-reloads, and a reload
// caused by an unrelated hand edit must not throw away the token you just
// fetched.
func TestAdoptCarriesTheSessionAcrossAReload(t *testing.T) {
	old := load(t, twoEnvs)
	old.SetActive("prod")
	old.Set("token", "abc")

	fresh := load(t, twoEnvs)
	fresh.Adopt(old)

	if fresh.Active() != "prod" {
		t.Errorf("Active() = %q, want the selection kept", fresh.Active())
	}
	if v, _ := fresh.Lookup("token"); v != "abc" {
		t.Errorf("token = %q, want it carried across", v)
	}
}

// TestAdoptDropsASelectionThatIsGone: if the environment you were on was
// renamed away in the edit that triggered the reload, falling back is better
// than pointing at nothing.
func TestAdoptDropsASelectionThatIsGone(t *testing.T) {
	old := load(t, twoEnvs)
	old.SetActive("prod")

	fresh := load(t, `{"dev": {"base": "http://localhost"}}`)
	fresh.Adopt(old)

	if fresh.Active() != "dev" {
		t.Errorf("Active() = %q, want it to fall back to an environment that exists", fresh.Active())
	}
}

// TestBigNumbersSurvive is the same rule the JSON transforms have a test
// pinning: an id that does not fit in a float64 must come out as written. An
// environment holding a tenant id is exactly where this bites.
func TestBigNumbersSurvive(t *testing.T) {
	e := load(t, `{"dev": {"tenant": 9007199254740993, "ratio": 1.50, "on": true}}`)

	for _, c := range []struct{ key, want string }{
		{"tenant", "9007199254740993"},
		{"ratio", "1.50"},
		{"on", "true"},
	} {
		if got, _ := e.Lookup(c.key); got != c.want {
			t.Errorf("Lookup(%s) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestLoadEnvErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"not an object", `["dev"]`, "top level"},
		{"a nested object as a value", `{"dev": {"a": {"b": 1}}}`, "not a string"},
		{"an array as a value", `{"dev": {"a": [1]}}`, "not a string"},
		{"broken json", `{"dev": `, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadEnv(write(t, c.body))
			if err == nil {
				t.Fatalf("LoadEnv(%s) succeeded, want an error", c.body)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestExpandRequest checks that everything which can carry a variable does, and
// that the hook does not -- substituting text into a script before running it
// is how injection bugs are made.
func TestExpandRequest(t *testing.T) {
	e := load(t, twoEnvs)
	e.Set("token", "ey.123")

	in := Request{
		Method:  "POST",
		URL:     "{{base}}/orders",
		Headers: []Header{{"Authorization", "Bearer {{token}}"}, {"X-{{user}}", "1"}},
		Body:    `{"who": "{{user}}", "missing": "{{nope}}"}`,
		Hook:    "env.x = {{user}}",
	}

	out, missing := e.ExpandRequest(in)

	if out.URL != "http://localhost:8080/orders" {
		t.Errorf("URL = %q", out.URL)
	}
	if out.Headers[0].Value != "Bearer ey.123" {
		t.Errorf("header value = %q", out.Headers[0].Value)
	}
	if out.Headers[1].Name != "X-admin" {
		t.Errorf("header name = %q, want the name expanded too", out.Headers[1].Name)
	}
	if out.Body != `{"who": "admin", "missing": "{{nope}}"}` {
		t.Errorf("body = %q", out.Body)
	}
	if out.Hook != in.Hook {
		t.Errorf("hook = %q, want it untouched", out.Hook)
	}
	if len(missing) != 1 || missing[0] != "nope" {
		t.Errorf("missing = %v, want [nope]", missing)
	}

	// The input must be unchanged, since the same Request is sent again against
	// another environment.
	if in.URL != "{{base}}/orders" || in.Headers[0].Value != "Bearer {{token}}" {
		t.Error("ExpandRequest mutated its input")
	}
}
