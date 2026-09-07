package api

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Defaults for sending. Both are deliberately modest: this is a palette that
// has to stay responsive, not a load tool.
const (
	// DefaultTimeout is how long a request may take before it is given up on.
	// A request can raise it with "# @timeout".
	DefaultTimeout = 30 * time.Second

	// MaxBody is how much of a response is read. An accidental GET on a log
	// endpoint should not put a hundred megabytes into a string that the UI then
	// tries to render -- and the pane can show a few thousand lines at most in
	// any case. What is dropped is reported, never silently discarded.
	MaxBody = 4 << 20 // 4 MiB
)

// Response is what came back.
type Response struct {
	Status     int
	StatusText string
	Header     http.Header
	Body       string

	// Elapsed is measured around the whole exchange, including reading the body,
	// because that is the number that matches what the wait felt like.
	Elapsed time.Duration

	// Size is how many bytes of body were actually read. With Truncated set it
	// is MaxBody rather than the real length, which the server may not have
	// declared anyway.
	Size      int64
	Truncated bool
}

// Send performs a request. The request must already have had its variables
// expanded; Send does no substitution of its own.
//
// It is written to be called from a worker goroutine, never the UI thread: the
// palette must stay responsive while an endpoint that will never answer is
// timing out. Cancelling ctx returns promptly, which is what makes Esc able to
// abandon a send.
func Send(ctx context.Context, r Request) (Response, error) {
	if strings.TrimSpace(r.URL) == "" {
		return Response{}, errors.New("no URL to send to")
	}
	if i := strings.Index(r.URL, "{{"); i >= 0 {
		// Worth its own error: net/http would otherwise fail with a URL parse
		// message that says nothing about the variable being the cause.
		return Response{}, fmt.Errorf("the URL still contains an unexpanded variable: %s", r.URL[i:])
	}
	if !hasScheme(r.URL) {
		return Response{}, fmt.Errorf("the URL needs a scheme: %q is missing the http:// or https://", r.URL)
	}

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if r.Body != "" {
		body = strings.NewReader(r.Body)
	}

	method := r.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		return Response{}, err
	}
	for _, h := range r.Headers {
		// Host is not a header on the request struct -- net/http reads it from a
		// field, and setting it in the map is silently ignored.
		if strings.EqualFold(h.Name, "Host") {
			req.Host = h.Value
			continue
		}
		req.Header.Add(h.Name, h.Value)
	}

	start := time.Now()
	resp, err := clientFor(r.Insecure).Do(req)
	if err != nil {
		return Response{}, sendError(ctx, err, timeout)
	}
	defer resp.Body.Close()

	// One byte past the cap, so a body exactly at the limit is not reported as
	// truncated and one byte over is.
	read, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return Response{}, sendError(ctx, err, timeout)
	}

	out := Response{
		Status:     resp.StatusCode,
		StatusText: strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprint(resp.StatusCode))),
		Header:     resp.Header,
		Size:       int64(len(read)),
	}
	if len(read) > MaxBody {
		read = read[:MaxBody]
		out.Size = MaxBody
		out.Truncated = true
	}
	out.Body = string(read)
	out.Elapsed = time.Since(start)
	return out, nil
}

// sendError turns the transport's error into one worth showing.
//
// A timeout and a cancellation arrive as the same "context deadline exceeded"
// shape wrapped in a url.Error, and the difference matters: one is the endpoint
// being slow and the other is you pressing Esc.
func sendError(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case errors.Is(err, context.Canceled):
		return errors.New("cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("timed out after %s -- raise it for this request with \"# @timeout %d\"",
			timeout, int(timeout.Seconds())*2)
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return err
}

// Two clients, made once and shared.
//
// Sharing matters: a Transport pools connections, and building one per request
// would open a new TCP and TLS handshake every send while leaking the old
// pool. The insecure one is separate rather than a flag on a shared Transport
// so that turning verification off for one request can never affect another.
var (
	clientOnce sync.Once
	secure     *http.Client
	insecure   *http.Client
)

func clientFor(skipVerify bool) *http.Client {
	clientOnce.Do(func() {
		secure = &http.Client{Transport: http.DefaultTransport}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		insecure = &http.Client{Transport: tr}
	})
	if skipVerify {
		return insecure
	}
	return secure
}

// hasScheme reports whether the URL starts with something://.
func hasScheme(url string) bool {
	i := strings.Index(url, "://")
	if i <= 0 {
		return false
	}
	for _, c := range url[:i] {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') &&
			c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}
