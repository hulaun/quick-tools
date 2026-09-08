package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func sample() Response {
	return Response{
		Status:     201,
		StatusText: "Created",
		Body:       `{"accessToken":"ey.123","id":9007199254740993,"nested":{"a":[1,2]}}`,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
			"X-Request-Id": []string{"abc-123"},
		},
		Elapsed: 142 * time.Millisecond,
		Size:    67,
	}
}

// TestHookSetsAVariable is the feature in one test: a token lifted out of a
// response and carried into the next request.
func TestHookSetsAVariable(t *testing.T) {
	e := load(t, twoEnvs)

	changed, err := RunHook(`env.token = response.json().accessToken;`, sample(), e)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if len(changed) != 1 || changed[0] != "token" {
		t.Fatalf("changed = %v, want [token]", changed)
	}
	if v, _ := e.Lookup("token"); v != "ey.123" {
		t.Errorf("token = %q, want ey.123", v)
	}

	// It must land in the session overlay, never in the file layer -- writing
	// through would rewrite env.json on every send and the watcher would reload
	// it on every send in turn.
	if _, onDisk := e.fileValue("dev", "token"); onDisk {
		t.Error("the hook wrote into the file layer; it must only touch the session")
	}
}

// TestHookReadsTheEnvironment: a hook can build on what is already there.
func TestHookReadsTheEnvironment(t *testing.T) {
	e := load(t, twoEnvs)

	if _, err := RunHook(`env.next = env.base + "/orders";`, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if v, _ := e.Lookup("next"); v != "http://localhost:8080/orders" {
		t.Errorf("next = %q", v)
	}
}

// TestHookSeesTheResponse pins the shape of the object the script is handed.
func TestHookSeesTheResponse(t *testing.T) {
	e := load(t, twoEnvs)

	src := `
		env.status = String(response.status);
		env.text = response.statusText;
		env.ctype = response.header("content-type");
		env.ctypeExact = response.headers["Content-Type"];
		env.rid = response.header("X-REQUEST-ID");
		env.missing = response.header("nope");
		env.ms = String(response.elapsed);
		env.len = String(response.size);
		env.cut = String(response.truncated);
		env.raw = response.body.length > 0 ? "yes" : "no";
	`
	if _, err := RunHook(src, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}

	for _, c := range []struct{ key, want string }{
		{"status", "201"},
		{"text", "Created"},
		{"ctype", "application/json"},
		{"ctypeExact", "application/json"},
		{"rid", "abc-123"},
		{"missing", ""},
		{"ms", "142"},
		{"len", "67"},
		{"cut", "false"},
		{"raw", "yes"},
	} {
		if got, _ := e.Lookup(c.key); got != c.want {
			t.Errorf("%s = %q, want %q", c.key, got, c.want)
		}
	}
}

// TestHookBigNumberSurvives is the rule that shows up in every layer here: an
// id too large for a float64 must come back as written. A hook lifting an id
// out of a response is exactly where it would be silently rounded.
func TestHookBigNumberSurvives(t *testing.T) {
	e := load(t, twoEnvs)

	if _, err := RunHook(`env.id = String(response.json().id);`, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if v, _ := e.Lookup("id"); v != "9007199254740993" {
		t.Errorf("id = %q, want 9007199254740993 -- it was rounded", v)
	}
}

// TestHookUnchangedValuesAreNotReported: seeding the object with the current
// environment must not make every existing key look like it was just set.
func TestHookUnchangedValuesAreNotReported(t *testing.T) {
	e := load(t, twoEnvs)

	changed, err := RunHook(`var x = env.base;`, sample(), e)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing", changed)
	}
}

// TestHookCanOverwrite an existing value.
func TestHookCanOverwrite(t *testing.T) {
	e := load(t, twoEnvs)

	changed, err := RunHook(`env.user = "someone-else";`, sample(), e)
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if len(changed) != 1 || changed[0] != "user" {
		t.Fatalf("changed = %v, want [user]", changed)
	}
	if v, _ := e.Lookup("user"); v != "someone-else" {
		t.Errorf("user = %q", v)
	}
	if v, _ := e.fileValue("dev", "user"); v != "admin" {
		t.Error("the file layer was modified")
	}
}

// TestHookIsScopedToTheActiveEnvironment. The same safety property the overlay
// has: a token fetched against dev must not be in scope for prod.
func TestHookIsScopedToTheActiveEnvironment(t *testing.T) {
	e := load(t, twoEnvs)

	if _, err := RunHook(`env.token = "dev-only";`, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	e.SetActive("prod")
	if v, ok := e.Lookup("token"); ok {
		t.Errorf("the dev token leaked into prod as %q", v)
	}
}

// TestHookErrors covers what a hook being written wrong looks like. These are
// read in a one-line status strip, so they have to be short and say where.
func TestHookErrors(t *testing.T) {
	e := load(t, twoEnvs)

	cases := []struct{ name, src, want string }{
		{"a syntax error", `env.token = ;`, "SyntaxError"},
		{"calling something that is not there", `env.x = nope.field;`, "nope is not defined"},
		{"a body that is not JSON", `env.x = response.json();`, "not JSON"},
		{"throwing on purpose", `throw new Error("no token in the response");`, "no token in the response"},
	}

	notJSON := sample()
	notJSON.Body = "<html>gateway timeout</html>"

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := sample()
			if strings.Contains(c.want, "not JSON") {
				resp = notJSON
			}
			_, err := RunHook(c.src, resp, e)
			if err == nil {
				t.Fatalf("RunHook(%q) succeeded, want an error", c.src)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestHookTimeout: an accidental infinite loop must be survivable, not fatal.
func TestHookTimeout(t *testing.T) {
	e := load(t, twoEnvs)

	start := time.Now()
	_, err := RunHook(`while (true) {}`, sample(), e)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("an infinite loop returned without error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to say it timed out", err)
	}
	if elapsed > 3*HookTimeout {
		t.Errorf("took %s to give up on a %s budget", elapsed, HookTimeout)
	}
}

// TestHookEmptyIsNotAnError -- most requests have no hook at all.
func TestHookEmptyIsNotAnError(t *testing.T) {
	e := load(t, twoEnvs)
	for _, src := range []string{"", "   ", "\n\t\n"} {
		changed, err := RunHook(src, sample(), e)
		if err != nil || len(changed) != 0 {
			t.Errorf("RunHook(%q) = %v, %v; want nothing", src, changed, err)
		}
	}
}

// TestHookVarsDoNotLeak: each run gets a fresh runtime, so a `var` in one hook
// cannot be seen by the next.
func TestHookVarsDoNotLeak(t *testing.T) {
	e := load(t, twoEnvs)

	if _, err := RunHook(`var secret = "shh";`, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if _, err := RunHook(`env.leaked = typeof secret;`, sample(), e); err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if v, _ := e.Lookup("leaked"); v != "undefined" {
		t.Errorf("leaked = %q, want undefined", v)
	}
}

// TestHookReturnIsLegal -- the block is wrapped in a function, so an early
// return is a reasonable thing to write.
func TestHookReturnIsLegal(t *testing.T) {
	e := load(t, twoEnvs)

	src := `
		if (response.status !== 200) { return; }
		env.token = "never set";
	`
	changed, err := RunHook(src, sample(), e) // the sample is a 201
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if len(changed) != 0 {
		t.Errorf("changed = %v, want nothing -- the early return should have stopped it", changed)
	}
}

func TestEnvKeys(t *testing.T) {
	e := load(t, twoEnvs)
	e.Set("token", "x")

	got := strings.Join(e.Keys(), ",")
	if got != "base,token,user" {
		t.Errorf("Keys() = %q, want the file's and the session's together, sorted", got)
	}
}
