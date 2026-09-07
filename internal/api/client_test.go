package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSend(t *testing.T) {
	var got *http.Request
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)

		w.Header().Set("X-Answer", "42")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	resp, err := Send(context.Background(), Request{
		Method:  "POST",
		URL:     srv.URL + "/orders?page=2",
		Headers: []Header{{"Content-Type", "application/json"}, {"X-Trace", "abc"}},
		Body:    `{"id":1}`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if resp.Status != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.Status)
	}
	if resp.StatusText != "Created" {
		t.Errorf("status text = %q, want Created", resp.StatusText)
	}
	if resp.Body != `{"ok":true}` {
		t.Errorf("body = %q", resp.Body)
	}
	if resp.Header.Get("X-Answer") != "42" {
		t.Errorf("response header = %q", resp.Header.Get("X-Answer"))
	}
	if resp.Size != int64(len(`{"ok":true}`)) {
		t.Errorf("size = %d", resp.Size)
	}
	if resp.Truncated {
		t.Error("a short body was reported as truncated")
	}
	if resp.Elapsed <= 0 {
		t.Error("elapsed was not measured")
	}

	if got.Method != "POST" {
		t.Errorf("method = %q", got.Method)
	}
	if got.URL.RawQuery != "page=2" {
		t.Errorf("query = %q, want it preserved", got.URL.RawQuery)
	}
	if got.Header.Get("X-Trace") != "abc" {
		t.Errorf("request header = %q", got.Header.Get("X-Trace"))
	}
	if gotBody != `{"id":1}` {
		t.Errorf("request body = %q", gotBody)
	}
}

// TestSendDefaultsToGet: a one-line file with just a URL has no method.
func TestSendDefaultsToGet(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
	}))
	defer srv.Close()

	if _, err := Send(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if method != "GET" {
		t.Errorf("method = %q, want GET", method)
	}
}

// TestSendRepeatedHeader checks that a header written twice is sent twice --
// the reason Headers is a slice and not a map.
func TestSendRepeatedHeader(t *testing.T) {
	var values []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values = r.Header.Values("Accept")
	}))
	defer srv.Close()

	_, err := Send(context.Background(), Request{
		URL:     srv.URL,
		Headers: []Header{{"Accept", "text/plain"}, {"Accept", "application/json"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(values) != 2 || values[0] != "text/plain" || values[1] != "application/json" {
		t.Errorf("Accept = %v, want both values in order", values)
	}
}

// TestSendHostHeader pins the special case: net/http reads Host from a field,
// and setting it in the header map is silently ignored -- which would look like
// the header being dropped for no reason.
func TestSendHostHeader(t *testing.T) {
	var host string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
	}))
	defer srv.Close()

	_, err := Send(context.Background(), Request{
		URL:     srv.URL,
		Headers: []Header{{"Host", "internal.name"}},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if host != "internal.name" {
		t.Errorf("Host = %q, want internal.name", host)
	}
}

// TestSendTruncates is the cap that keeps a log endpoint from putting a hundred
// megabytes into a string the pane then tries to render.
func TestSendTruncates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 1<<16)
		for sent := 0; sent < MaxBody+(1<<16); sent += len(chunk) {
			fmt.Fprint(w, chunk)
		}
	}))
	defer srv.Close()

	resp, err := Send(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !resp.Truncated {
		t.Error("a body over the cap was not reported as truncated")
	}
	if len(resp.Body) != MaxBody {
		t.Errorf("body length = %d, want it cut to %d", len(resp.Body), MaxBody)
	}
	if resp.Size != MaxBody {
		t.Errorf("size = %d, want %d", resp.Size, MaxBody)
	}
}

// TestSendExactlyAtTheCapIsNotTruncated is the off-by-one either side of the
// limit, which is why the reader is given one byte more than the cap.
func TestSendExactlyAtTheCapIsNotTruncated(t *testing.T) {
	const n = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("y", n))
	}))
	defer srv.Close()

	resp, err := Send(context.Background(), Request{URL: srv.URL})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if resp.Truncated || len(resp.Body) != n {
		t.Errorf("truncated = %v, length = %d, want false and %d", resp.Truncated, len(resp.Body), n)
	}
}

// TestSendTimeout: the message has to say it timed out and how to raise it,
// because the alternative is a url.Error that mentions a context deadline and
// leaves you guessing.
func TestSendTimeout(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	_, err := Send(context.Background(), Request{URL: srv.URL, Timeout: 100 * time.Millisecond})
	if err == nil {
		t.Fatal("Send succeeded against a server that never answers")
	}
	if !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "@timeout") {
		t.Errorf("error = %q, want it to say it timed out and how to raise it", err)
	}
}

// TestSendCancel is what makes Esc able to abandon a send. A cancellation must
// be distinguishable from a timeout: one is the endpoint being slow, the other
// is a decision.
func TestSendCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := Send(ctx, Request{URL: srv.URL, Timeout: 30 * time.Second})
	if err == nil {
		t.Fatal("Send succeeded despite being cancelled")
	}
	if err.Error() != "cancelled" {
		t.Errorf("error = %q, want %q", err, "cancelled")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancel took %s; it must return promptly, not wait for the timeout", elapsed)
	}
}

// TestSendInsecure: a self-signed certificate is exactly what an internal
// server has, and is the reason the directive exists.
func TestSendInsecure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	if _, err := Send(context.Background(), Request{URL: srv.URL}); err == nil {
		t.Error("a self-signed certificate was accepted without @insecure")
	}

	resp, err := Send(context.Background(), Request{URL: srv.URL, Insecure: true})
	if err != nil {
		t.Fatalf("Send with @insecure: %v", err)
	}
	if resp.Body != "ok" {
		t.Errorf("body = %q", resp.Body)
	}
}

// TestSendRejects covers what never reaches the network, and why each message
// is worth having: net/http's own errors for these say nothing useful.
func TestSendRejects(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"empty", "", "no URL"},
		{"an unexpanded variable", "{{base}}/orders", "unexpanded variable"},
		{"a variable in the middle", "http://h/{{id}}", "unexpanded variable"},
		{"no scheme", "localhost:8080/x", "scheme"},
		{"a bare path", "/orders", "scheme"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Send(context.Background(), Request{URL: c.url})
			if err == nil {
				t.Fatalf("Send(%q) succeeded, want an error", c.url)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestHasScheme(t *testing.T) {
	yes := []string{"http://h", "https://h", "HTTP://h", "x+y-z.1://h"}
	no := []string{"", "h", "/path", "localhost:8080", "://h", "not a url://x"}
	for _, s := range yes {
		if !hasScheme(s) {
			t.Errorf("hasScheme(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if hasScheme(s) {
			t.Errorf("hasScheme(%q) = true, want false", s)
		}
	}
}
