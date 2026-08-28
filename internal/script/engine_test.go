package script

import (
	"strings"
	"testing"
	"time"
)

func mustCompile(t *testing.T, src string) *Script {
	t.Helper()
	s, err := Compile("test", "test.js", src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return s
}

func TestRunsTransform(t *testing.T) {
	s := mustCompile(t, `function transform(input) { return input.toUpperCase(); }`)
	got, err := s.Run("hello")
	if err != nil || got != "HELLO" {
		t.Errorf("Run = %q, %v; want \"HELLO\"", got, err)
	}
}

func TestReadsMetadata(t *testing.T) {
	s := mustCompile(t, `
		var name = "My Transform";
		var group = "Custom";
		var tags = ["a", "b"];
		function transform(i) { return i; }
	`)
	if s.Name != "My Transform" || s.Group != "Custom" {
		t.Errorf("metadata = %q / %q", s.Name, s.Group)
	}
	if strings.Join(s.Tags, ",") != "a,b" {
		t.Errorf("tags = %v", s.Tags)
	}
}

func TestMetadataDefaults(t *testing.T) {
	s := mustCompile(t, `function transform(i) { return i; }`)
	if s.Name != "test" {
		t.Errorf("Name fell back to %q, want the id", s.Name)
	}
	if s.Group != "Scripts" {
		t.Errorf("Group = %q, want \"Scripts\"", s.Group)
	}
}

func TestCompileRejectsBadScripts(t *testing.T) {
	cases := []struct{ name, src string }{
		{"syntax error", `function transform( {`},
		{"no transform", `var name = "x";`},
		{"transform not a function", `var transform = 42;`},
		{"throws at load", `throw new Error("boom"); function transform(i){return i;}`},
	}
	for _, c := range cases {
		if _, err := Compile("t", "t.js", c.src); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

func TestRuntimeErrorIsReadable(t *testing.T) {
	s := mustCompile(t, `function transform(i) { return i.nope.deeper; }`)
	_, err := s.Run("x")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The message should name the problem, not just dump a stack.
	if !strings.Contains(strings.ToLower(err.Error()), "undefined") {
		t.Errorf("unhelpful error: %v", err)
	}
}

func TestReturningNothingIsAnError(t *testing.T) {
	s := mustCompile(t, `function transform(i) { }`)
	if _, err := s.Run("x"); err == nil {
		t.Error("expected an error when the script returns nothing")
	}
}

// The whole point of the timeout: a runaway script must not be able to hang the
// palette. Without an interrupt this test would never finish.
func TestInfiniteLoopIsInterrupted(t *testing.T) {
	s := mustCompile(t, `function transform(i) { while (true) {} }`)
	s.SetTimeout(150 * time.Millisecond)

	done := make(chan error, 1)
	go func() {
		_, err := s.Run("x")
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error")
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("error did not mention the timeout: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupt did not fire -- a bad script could freeze the app")
	}
}

// A timeout must not poison the script: the interrupt flag is sticky in goja
// and has to be cleared, or every later run fails instantly.
func TestScriptStillWorksAfterATimeout(t *testing.T) {
	s := mustCompile(t, `
		function transform(i) {
			if (i === "hang") { while (true) {} }
			return i + "!";
		}
	`)
	s.SetTimeout(150 * time.Millisecond)

	if _, err := s.Run("hang"); err == nil {
		t.Fatal("expected the first run to time out")
	}
	got, err := s.Run("ok")
	if err != nil || got != "ok!" {
		t.Errorf("run after timeout = %q, %v; want \"ok!\"", got, err)
	}
}

func TestUnicodeSurvivesRoundTrip(t *testing.T) {
	s := mustCompile(t, `function transform(i) { return i + " wörld 日本語"; }`)
	got, err := s.Run("hello")
	if err != nil || got != "hello wörld 日本語" {
		t.Errorf("Run = %q, %v", got, err)
	}
}

func TestJSONIsAvailable(t *testing.T) {
	// Scripts get no host bindings, but the standard library must be there --
	// the parsers the user will write depend on JSON.stringify.
	s := mustCompile(t, `function transform(i) { return JSON.stringify({v: i}); }`)
	got, err := s.Run("x")
	if err != nil || got != `{"v":"x"}` {
		t.Errorf("Run = %q, %v", got, err)
	}
}
