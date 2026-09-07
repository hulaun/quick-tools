package api

import (
	"strings"
	"testing"
	"time"
)

// TestParse pins the .http format. These cases are the specification: if one of
// them changes, the format changed.
func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Request
	}{
		{
			name: "a bare URL is a GET",
			in:   "https://api.corp/health",
			want: Request{Method: "GET", URL: "https://api.corp/health"},
		},
		{
			name: "method and URL",
			in:   "post https://api.corp/orders",
			want: Request{Method: "POST", URL: "https://api.corp/orders"},
		},
		{
			name: "a trailing HTTP version is discarded",
			in:   "GET https://api.corp/x HTTP/1.1",
			want: Request{Method: "GET", URL: "https://api.corp/x"},
		},
		{
			name: "headers, then a blank line, then the body",
			in: "POST {{base}}/login\n" +
				"Content-Type: application/json\n" +
				"X-Trace: abc\n" +
				"\n" +
				`{"user": "{{user}}"}`,
			want: Request{
				Method: "POST", URL: "{{base}}/login",
				Headers: []Header{
					{"Content-Type", "application/json"},
					{"X-Trace", "abc"},
				},
				Body: `{"user": "{{user}}"}`,
			},
		},
		{
			name: "a header value may contain colons",
			in:   "GET http://h/x\nAuthorization: Bearer a:b:c\n",
			want: Request{
				Method: "GET", URL: "http://h/x",
				Headers: []Header{{"Authorization", "Bearer a:b:c"}},
			},
		},
		{
			name: "a repeated header is kept twice, in order",
			in:   "GET http://h/x\nAccept: text/plain\nAccept: application/json\n",
			want: Request{
				Method: "GET", URL: "http://h/x",
				Headers: []Header{
					{"Accept", "text/plain"},
					{"Accept", "application/json"},
				},
			},
		},
		{
			name: "directives and comments above the request line",
			in: "# @name Login\n" +
				"# @timeout 60\n" +
				"# @insecure\n" +
				"// just a note\n" +
				"POST http://h/login\n",
			want: Request{
				Name: "Login", Method: "POST", URL: "http://h/login",
				Timeout: 60 * time.Second, Insecure: true,
			},
		},
		{
			name: "@timeout also takes a duration",
			in:   "# @timeout 90s\nGET http://h/x",
			want: Request{Method: "GET", URL: "http://h/x", Timeout: 90 * time.Second},
		},
		{
			name: "an unknown directive is ignored, not rejected",
			in:   "# @prompt username\nGET http://h/x",
			want: Request{Method: "GET", URL: "http://h/x"},
		},
		{
			name: "a comment among the headers",
			in:   "GET http://h/x\n# why this header\nAccept: */*\n",
			want: Request{
				Method: "GET", URL: "http://h/x",
				Headers: []Header{{"Accept", "*/*"}},
			},
		},
		{
			name: "a multi-line hook",
			in: "POST http://h/login\n" +
				"\n" +
				"{}\n" +
				"\n" +
				"> {%\n" +
				"  env.token = JSON.parse(response.body).accessToken;\n" +
				"%}\n",
			want: Request{
				Method: "POST", URL: "http://h/login",
				Body: "{}",
				Hook: "  env.token = JSON.parse(response.body).accessToken;",
			},
		},
		{
			name: "a hook on one line",
			in:   "GET http://h/x\n\n> {% env.n = 1; %}\n",
			want: Request{Method: "GET", URL: "http://h/x", Hook: "env.n = 1;"},
		},
		{
			name: "a hook containing %} closes on the last one",
			in:   "GET http://h/x\n\n> {% env.s = \"%}\"; %}\n",
			want: Request{Method: "GET", URL: "http://h/x", Hook: `env.s = "%}";`},
		},
		{
			name: "a hook straight after the headers, with no body",
			in:   "GET http://h/x\nAccept: */*\n\n> {% env.n = 1 %}\n",
			want: Request{
				Method: "GET", URL: "http://h/x",
				Headers: []Header{{"Accept", "*/*"}},
				Hook:    "env.n = 1",
			},
		},
		{
			name: "a body keeps its inner blank lines and loses its outer ones",
			in:   "POST http://h/x\n\n\nline one\n\nline two\n\n\n",
			want: Request{Method: "POST", URL: "http://h/x", Body: "line one\n\nline two"},
		},
		{
			name: "CRLF is accepted",
			in:   "GET http://h/x\r\nAccept: */*\r\n\r\nbody\r\n",
			want: Request{
				Method: "GET", URL: "http://h/x",
				Headers: []Header{{"Accept", "*/*"}},
				Body:    "body",
			},
		},
		{
			name: "leading blank lines are skipped",
			in:   "\n\n\nGET http://h/x\n",
			want: Request{Method: "GET", URL: "http://h/x"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Parse(c.in)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Method != c.want.Method || got.URL != c.want.URL {
				t.Errorf("request line = %q %q, want %q %q", got.Method, got.URL, c.want.Method, c.want.URL)
			}
			if got.Name != c.want.Name {
				t.Errorf("name = %q, want %q", got.Name, c.want.Name)
			}
			if got.Body != c.want.Body {
				t.Errorf("body = %q, want %q", got.Body, c.want.Body)
			}
			if got.Hook != c.want.Hook {
				t.Errorf("hook = %q, want %q", got.Hook, c.want.Hook)
			}
			if got.Timeout != c.want.Timeout {
				t.Errorf("timeout = %v, want %v", got.Timeout, c.want.Timeout)
			}
			if got.Insecure != c.want.Insecure {
				t.Errorf("insecure = %v, want %v", got.Insecure, c.want.Insecure)
			}
			if len(got.Headers) != len(c.want.Headers) {
				t.Fatalf("headers = %v, want %v", got.Headers, c.want.Headers)
			}
			for i := range got.Headers {
				if got.Headers[i] != c.want.Headers[i] {
					t.Errorf("header %d = %v, want %v", i, got.Headers[i], c.want.Headers[i])
				}
			}
		})
	}
}

// TestParseErrors pins what is rejected. A format has to refuse things, and
// each of these refusals is a decision worth keeping.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		name, in, wantContains string
	}{
		{"empty", "", "empty"},
		{"only whitespace", "   \n\n  \n", "empty"},
		{"only comments", "# nothing here\n// nor here\n", "no request line"},
		{"a header line with no colon", "GET http://h/x\nAccept */*\n", "not a header"},
		{"a method with no URL", "GET\nAccept: */*\n\n", "no URL"},
		{"an unclosed hook", "GET http://h/x\n\n> {%\nenv.n = 1\n", "never closed"},
		{"two requests in one file", "GET http://h/a\n\n###\n\nGET http://h/b\n", "one request per file"},
		{"a leading separator", "### Login\nGET http://h/a\n", "one request per file"},
		{"a bad timeout", "# @timeout soon\nGET http://h/x", "@timeout"},
		{"a negative timeout", "# @timeout -5\nGET http://h/x", "positive"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.in)
			if err == nil {
				t.Fatalf("Parse(%q) succeeded, want an error", c.in)
			}
			if !strings.Contains(err.Error(), c.wantContains) {
				t.Errorf("error = %q, want it to mention %q", err, c.wantContains)
			}
		})
	}
}

// TestParseEmptyIsNamed checks that an empty file is distinguishable, because a
// request you have just created and not yet written is empty and is not the
// same thing as a broken one.
func TestParseEmptyIsNamed(t *testing.T) {
	if _, err := Parse("  \n "); err != ErrEmpty {
		t.Errorf("Parse(blank) = %v, want ErrEmpty", err)
	}
}

func TestRequestGet(t *testing.T) {
	r := Request{Headers: []Header{{"Content-Type", "application/json"}}}
	if v, ok := r.Get("content-type"); !ok || v != "application/json" {
		t.Errorf("Get(content-type) = %q, %v", v, ok)
	}
	if _, ok := r.Get("Accept"); ok {
		t.Error("Get(Accept) found a header that is not there")
	}
}
