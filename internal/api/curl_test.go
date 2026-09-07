package api

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatCurl(t *testing.T) {
	cases := []struct {
		name string
		in   Request
		want string
	}{
		{
			name: "a plain GET needs no -X",
			in:   Request{Method: "GET", URL: "https://api.corp/health"},
			want: `curl 'https://api.corp/health'`,
		},
		{
			name: "an empty method is a GET",
			in:   Request{URL: "https://api.corp/health"},
			want: `curl 'https://api.corp/health'`,
		},
		{
			name: "anything else is explicit",
			in:   Request{Method: "DELETE", URL: "https://api.corp/orders/1"},
			want: `curl -X DELETE 'https://api.corp/orders/1'`,
		},
		{
			name: "headers, one per line",
			in: Request{
				Method: "GET", URL: "https://api.corp/me",
				Headers: []Header{
					{"Authorization", "Bearer ey.123"},
					{"Accept", "application/json"},
				},
			},
			want: "curl 'https://api.corp/me' \\\n" +
				"  -H 'Authorization: Bearer ey.123' \\\n" +
				"  -H 'Accept: application/json'",
		},
		{
			name: "a body uses --data-raw",
			in: Request{
				Method: "POST", URL: "https://api.corp/orders",
				Headers: []Header{{"Content-Type", "application/json"}},
				Body:    `{"id":1}`,
			},
			want: "curl -X POST 'https://api.corp/orders' \\\n" +
				"  -H 'Content-Type: application/json' \\\n" +
				`  --data-raw '{"id":1}'`,
		},
		{
			name: "a GET with a body keeps -X, or curl would make it a POST",
			in:   Request{Method: "GET", URL: "https://api.corp/search", Body: `{"q":"x"}`},
			want: "curl -X GET 'https://api.corp/search' \\\n" + `  --data-raw '{"q":"x"}'`,
		},
		{
			name: "a query string is quoted, so & does not background the command",
			in:   Request{Method: "GET", URL: "https://api.corp/orders?page=2&size=50"},
			want: `curl 'https://api.corp/orders?page=2&size=50'`,
		},
		{
			name: "a single quote in a value is escaped the standard way",
			in: Request{
				Method: "POST", URL: "https://api.corp/x",
				Body: `{"note":"it's here"}`,
			},
			want: "curl -X POST 'https://api.corp/x' \\\n" +
				`  --data-raw '{"note":"it'\''s here"}'`,
		},
		{
			name: "a dollar sign survives, because single quotes do not interpolate",
			in:   Request{Method: "POST", URL: "https://h/x", Body: `cost=$100`},
			want: "curl -X POST 'https://h/x' \\\n  --data-raw 'cost=$100'",
		},
		{
			name: "insecure",
			in:   Request{Method: "GET", URL: "https://internal/x", Insecure: true},
			want: "curl 'https://internal/x' \\\n  --insecure",
		},
		{
			name: "a timeout becomes --max-time",
			in:   Request{Method: "GET", URL: "https://h/x", Timeout: 90 * time.Second},
			want: "curl 'https://h/x' \\\n  --max-time 90",
		},
		{
			name: "a fractional timeout is not rounded",
			in:   Request{Method: "GET", URL: "https://h/x", Timeout: 1500 * time.Millisecond},
			want: "curl 'https://h/x' \\\n  --max-time 1.5",
		},
		{
			name: "everything at once, in a stable order",
			in: Request{
				Method: "PUT", URL: "https://internal/orders/1",
				Headers:  []Header{{"Authorization", "Bearer x"}},
				Body:     `{"a":1}`,
				Insecure: true,
				Timeout:  30 * time.Second,
			},
			want: "curl -X PUT 'https://internal/orders/1' \\\n" +
				"  -H 'Authorization: Bearer x' \\\n" +
				"  --data-raw '{\"a\":1}' \\\n" +
				"  --insecure \\\n" +
				"  --max-time 30",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatCurl(c.in); got != c.want {
				t.Errorf("FormatCurl:\n got: %s\nwant: %s", got, c.want)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", `'plain'`},
		{"", `''`},
		{"with space", `'with space'`},
		{`it's`, `'it'\''s'`},
		{`'`, `''\'''`},
		{`$HOME`, `'$HOME'`},
		{"a\nb", "'a\nb'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ParseCurl -- reserved
// ---------------------------------------------------------------------------

// TestParseCurl is the red loop for the reserved parser.
//
// It skips by default so that `go test ./...` stays green while the function is
// unwritten -- the same arrangement the six JavaScript stubs have, where the
// failing fixtures live behind `quicktools.exe -test-scripts` rather than in the
// Go suite. Nothing here is broken; this is a capability that does not exist
// yet.
//
// To work on it:
//
//	QUICKTOOLS_TODO=1 go test ./internal/api/ -run ParseCurl -v
func TestParseCurl(t *testing.T) {
	if os.Getenv("QUICKTOOLS_TODO") == "" {
		t.Skip("ParseCurl is reserved -- run with QUICKTOOLS_TODO=1 to work on it")
	}

	cases := []struct {
		name string
		in   string
		want Request
	}{
		{
			name: "the simplest line",
			in:   `curl 'https://h/a'`,
			want: Request{Method: "GET", URL: "https://h/a"},
		},
		{
			name: "an unquoted URL",
			in:   `curl https://h/a`,
			want: Request{Method: "GET", URL: "https://h/a"},
		},
		{
			name: "double quotes",
			in:   `curl "https://h/a?x=1&y=2"`,
			want: Request{Method: "GET", URL: "https://h/a?x=1&y=2"},
		},
		{
			name: "an explicit method",
			in:   `curl -X POST https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a"},
		},
		{
			name: "a method joined to its flag",
			in:   `curl -XPOST https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a"},
		},
		{
			name: "headers, in order",
			in:   `curl -H 'A: 1' -H 'B: 2' https://h/a`,
			want: Request{
				Method: "GET", URL: "https://h/a",
				Headers: []Header{{"A", "1"}, {"B", "2"}},
			},
		},
		{
			name: "the long form of a flag",
			in:   `curl --header 'A: 1' https://h/a`,
			want: Request{
				Method: "GET", URL: "https://h/a",
				Headers: []Header{{"A", "1"}},
			},
		},
		{
			name: "a header value containing a colon",
			in:   `curl -H 'Authorization: Bearer a:b:c' https://h/a`,
			want: Request{
				Method: "GET", URL: "https://h/a",
				Headers: []Header{{"Authorization", "Bearer a:b:c"}},
			},
		},
		{
			name: "data implies POST",
			in:   `curl -d '{"a":1}' https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a", Body: `{"a":1}`},
		},
		{
			name: "unless -X says otherwise",
			in:   `curl -d 'a=1' -X PUT https://h/a`,
			want: Request{Method: "PUT", URL: "https://h/a", Body: "a=1"},
		},
		{
			name: "--data-raw",
			in:   `curl --data-raw '{"a":1}' https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a", Body: `{"a":1}`},
		},
		{
			name: "--data-urlencode encodes, which --data does not",
			in:   `curl --data-urlencode 'q=a b' https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a", Body: "q=a%20b"},
		},
		{
			name: "-u becomes an Authorization header",
			in:   `curl -u alice:s3cret https://h/a`,
			want: Request{
				Method: "GET", URL: "https://h/a",
				Headers: []Header{{"Authorization", "Basic YWxpY2U6czNjcmV0"}},
			},
		},
		{
			name: "-k is insecure",
			in:   `curl -k https://h/a`,
			want: Request{Method: "GET", URL: "https://h/a", Insecure: true},
		},
		{
			name: "--max-time is a timeout",
			in:   `curl --max-time 60 https://h/a`,
			want: Request{Method: "GET", URL: "https://h/a", Timeout: 60 * time.Second},
		},
		{
			name: "valueless flags are ignored",
			in:   `curl -L -s -v --compressed https://h/a`,
			want: Request{Method: "GET", URL: "https://h/a"},
		},
		{
			name: "continued over several lines, as they always arrive",
			in: "curl 'https://h/orders' \\\n" +
				"  -H 'Authorization: Bearer ey.1' \\\n" +
				"  -H 'Content-Type: application/json' \\\n" +
				"  --data-raw '{\"id\":1}'",
			want: Request{
				Method: "POST", URL: "https://h/orders",
				Headers: []Header{
					{"Authorization", "Bearer ey.1"},
					{"Content-Type", "application/json"},
				},
				Body: `{"id":1}`,
			},
		},
		{
			name: "a single quote escaped the standard way",
			in:   `curl --data-raw 'it'\''s' https://h/a`,
			want: Request{Method: "POST", URL: "https://h/a", Body: "it's"},
		},
		{
			name: "backslash escapes inside double quotes, but not single",
			in:   `curl -H "X: a\"b" --data-raw 'c\d' https://h/a`,
			want: Request{
				Method: "POST", URL: "https://h/a",
				Headers: []Header{{"X", `a"b`}},
				Body:    `c\d`,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseCurl(c.in)
			if err != nil {
				t.Fatalf("ParseCurl: %v", err)
			}
			if got.Method != c.want.Method || got.URL != c.want.URL {
				t.Errorf("request line = %q %q, want %q %q",
					got.Method, got.URL, c.want.Method, c.want.URL)
			}
			if got.Body != c.want.Body {
				t.Errorf("body = %q, want %q", got.Body, c.want.Body)
			}
			if got.Insecure != c.want.Insecure {
				t.Errorf("insecure = %v, want %v", got.Insecure, c.want.Insecure)
			}
			if got.Timeout != c.want.Timeout {
				t.Errorf("timeout = %v, want %v", got.Timeout, c.want.Timeout)
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

// TestParseCurlErrors: a parser has to refuse some things, and each refusal is
// a decision worth pinning.
func TestParseCurlErrors(t *testing.T) {
	if os.Getenv("QUICKTOOLS_TODO") == "" {
		t.Skip("ParseCurl is reserved -- run with QUICKTOOLS_TODO=1 to work on it")
	}

	cases := []struct{ name, in, want string }{
		{"empty", ``, "empty"},
		{"not a curl line", `wget https://h/a`, "curl"},
		{"no URL at all", `curl -X POST -H 'A: 1'`, "no URL"},
		{"two URLs", `curl https://h/a https://h/b`, "more than one"},
		{"an unterminated quote", `curl 'https://h/a`, "quote"},
		{"a flag with no value", `curl -H`, "-H"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseCurl(c.in)
			if err == nil {
				t.Fatalf("ParseCurl(%q) succeeded, want an error", c.in)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// TestRoundTrip is the pair's own check, and the reason writing the two
// together is faster than writing either alone: what FormatCurl produces,
// ParseCurl has to read back.
func TestRoundTrip(t *testing.T) {
	if os.Getenv("QUICKTOOLS_TODO") == "" {
		t.Skip("ParseCurl is reserved -- run with QUICKTOOLS_TODO=1 to work on it")
	}

	original := Request{
		Method: "POST",
		URL:    "https://api.corp/orders?page=2&size=50",
		Headers: []Header{
			{"Authorization", "Bearer ey.123"},
			{"Content-Type", "application/json"},
		},
		Body:     `{"note":"it's here","n":9007199254740993}`,
		Insecure: true,
		Timeout:  45 * time.Second,
	}

	back, err := ParseCurl(FormatCurl(original))
	if err != nil {
		t.Fatalf("ParseCurl(FormatCurl(...)): %v", err)
	}
	if back.Method != original.Method || back.URL != original.URL {
		t.Errorf("request line = %q %q, want %q %q",
			back.Method, back.URL, original.Method, original.URL)
	}
	if back.Body != original.Body {
		t.Errorf("body = %q, want %q", back.Body, original.Body)
	}
	if back.Insecure != original.Insecure || back.Timeout != original.Timeout {
		t.Errorf("insecure/timeout = %v/%v, want %v/%v",
			back.Insecure, back.Timeout, original.Insecure, original.Timeout)
	}
	if len(back.Headers) != len(original.Headers) {
		t.Fatalf("headers = %v, want %v", back.Headers, original.Headers)
	}
	for i := range back.Headers {
		if back.Headers[i] != original.Headers[i] {
			t.Errorf("header %d = %v, want %v", i, back.Headers[i], original.Headers[i])
		}
	}
}
