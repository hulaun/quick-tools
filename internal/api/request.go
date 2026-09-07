// Package api is the API tab's model: a request read from a .http file, the
// environment its {{vars}} come from, and the client that sends it.
//
// Nothing here knows about the palette or about Win32, and none of it is
// Windows-only -- which is deliberate, because it means the format and the
// client are testable without a window.
package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Request is one saved request: the contents of one .http file.
//
// It holds the file as written, with {{vars}} unexpanded. Expansion happens
// against an Env on the way to Send, so the same Request can be sent against
// dev and prod without being re-read.
type Request struct {
	// Name is the "# @name" directive. Empty means the caller names it -- in
	// practice from the filename, the same way a note is named.
	Name string

	Method string
	URL    string

	// Headers keeps file order and allows duplicates, because both matter: some
	// APIs are order-sensitive and several headers (Set-Cookie, Accept) are
	// legitimately repeated. A map would quietly discard the second one.
	Headers []Header

	Body string

	// Hook is the JavaScript in the "> {% ... %}" block, run after a response
	// arrives so it can lift a token into the environment. Empty means none.
	Hook string

	// Timeout overrides the default for this one request; 0 means use it.
	Timeout time.Duration

	// Insecure skips TLS verification. It exists because an internal server with
	// a self-signed certificate is exactly the thing you end up debugging, and
	// without it the tab is useless against one. It is per-request on purpose --
	// there is no global switch, so turning it off for one endpoint can never
	// quietly turn it off for the rest.
	Insecure bool
}

// Header is one header line, kept as written.
type Header struct {
	Name  string
	Value string
}

// Get returns the first value of a header, matched case-insensitively, and
// whether it was there at all.
func (r Request) Get(name string) (string, bool) {
	for _, h := range r.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

// The markers that open and close a post-response hook. This is the VS Code
// REST Client syntax, kept so the files stay useful in other tools.
const (
	hookOpen  = "> {%"
	hookClose = "%}"
)

// ErrEmpty is returned for a file with nothing in it. It is a named error
// because an empty file is the ordinary state of a request you have just
// created and not yet written, which the UI shows differently from a file that
// is genuinely malformed.
var ErrEmpty = errors.New("the request is empty")

// Parse reads a .http file.
//
// The shape is the REST Client one:
//
//	# @name Login
//	POST {{base}}/auth/login
//	Content-Type: application/json
//
//	{"user": "{{user}}"}
//
//	> {%
//	  env.token = JSON.parse(response.body).accessToken;
//	%}
//
// Directives and comments may appear anywhere above the body. The blank line
// after the headers is what ends them; everything to the hook or the end of the
// file after that is the body.
func Parse(text string) (Request, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	// The multi-request separator is checked over the whole file rather than at
	// the request line, because "###" starts with "#" and would otherwise be
	// swallowed as a comment -- silently, which is the worst of the options: the
	// second request would be parsed as part of the first one's body.
	//
	// The cost is that a body line beginning with "###" has to be indented. That
	// is a fair trade for never mistaking two requests for one.
	for n, line := range lines {
		if strings.HasPrefix(line, "###") {
			return Request{}, fmt.Errorf("line %d separates a second request with \"###\", which this does not support -- one request per file, so the tree and the search have one entry each", n+1)
		}
	}

	var r Request
	i := 0

	// The request line, preceded by any number of comments and directives.
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if err := r.directive(line); err != nil {
			return Request{}, err
		}
		if isComment(line) {
			continue
		}
		if err := r.requestLine(line); err != nil {
			return Request{}, err
		}
		i++
		break
	}
	if r.URL == "" {
		if strings.TrimSpace(text) == "" {
			return Request{}, ErrEmpty
		}
		return Request{}, errors.New("no request line: expected something like \"GET https://host/path\"")
	}

	// Headers, to the blank line that ends them.
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			break
		}
		if strings.HasPrefix(line, hookOpen) {
			break
		}
		if err := r.directive(line); err != nil {
			return Request{}, err
		}
		if isComment(line) {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return Request{}, fmt.Errorf("line %d is not a header: %q -- headers are \"Name: value\", and a blank line ends them", i+1, line)
		}
		r.Headers = append(r.Headers, Header{
			Name:  strings.TrimSpace(name),
			Value: strings.TrimSpace(value),
		})
	}

	// The body, to the hook or the end.
	body := make([]string, 0, len(lines)-i)
	for ; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), hookOpen) {
			break
		}
		body = append(body, lines[i])
	}
	r.Body = strings.Trim(strings.Join(body, "\n"), "\n")

	// The hook.
	if i < len(lines) {
		hook, err := parseHook(lines[i:])
		if err != nil {
			return Request{}, err
		}
		r.Hook = hook
	}

	return r, nil
}

// requestLine reads "METHOD url" or a bare url.
//
// A bare url is a GET, which is what makes the shortest useful file one line
// long. The trailing "HTTP/1.1" that a copied request often carries is
// accepted and discarded -- the client decides the version, and rejecting the
// line would only mean deleting it by hand every time.
func (r *Request) requestLine(line string) error {
	fields := strings.Fields(line)
	switch {
	case len(fields) == 1 && isMethod(fields[0]):
		// One field, all letters. It cannot be a URL this can send -- no scheme,
		// no host, no path -- so it is a method whose URL was left off, and saying
		// that now is better than a URL parse error at send time.
		return fmt.Errorf("%q is a method with no URL, or a URL with no scheme -- the request line wants something like \"GET https://host/path\"", fields[0])
	case len(fields) == 1:
		r.Method, r.URL = "GET", fields[0]
	case isMethod(fields[0]):
		r.Method, r.URL = strings.ToUpper(fields[0]), fields[1]
	default:
		return fmt.Errorf("%q is not a method: expected something like \"GET %s\"", fields[0], fields[0])
	}
	if r.URL == "" {
		return errors.New("the request line has a method but no URL")
	}
	return nil
}

// directive reads a "# @name value" line. Anything else is left alone.
func (r *Request) directive(line string) error {
	rest, ok := commentBody(line)
	if !ok || !strings.HasPrefix(rest, "@") {
		return nil
	}
	key, value, _ := strings.Cut(strings.TrimPrefix(rest, "@"), " ")
	value = strings.TrimSpace(value)

	switch strings.ToLower(key) {
	case "name":
		r.Name = value
	case "insecure":
		r.Insecure = true
	case "timeout":
		d, err := parseTimeout(value)
		if err != nil {
			return err
		}
		r.Timeout = d
	}
	// An unknown directive is ignored rather than rejected: these files are
	// shared with other tools that have directives of their own, and refusing to
	// open a file because of a line meant for something else would be the wrong
	// trade.
	return nil
}

// parseTimeout accepts "60" as seconds, or any Go duration.
func parseTimeout(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("@timeout needs a value, as in \"# @timeout 60\"")
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("@timeout must be positive, got %d", n)
		}
		return time.Duration(n) * time.Second, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("@timeout %q is neither a number of seconds nor a duration like \"90s\"", s)
	}
	if d <= 0 {
		return 0, fmt.Errorf("@timeout must be positive, got %s", d)
	}
	return d, nil
}

// parseHook reads the "> {% ... %}" block. lines[0] holds the opening marker.
func parseHook(lines []string) (string, error) {
	first := strings.TrimSpace(lines[0])
	first = strings.TrimPrefix(first, hookOpen)

	// The whole hook on one line: "> {% env.x = 1 %}".
	if before, ok := cutLast(first, hookClose); ok {
		return strings.TrimSpace(before), nil
	}

	body := []string{first}
	for _, line := range lines[1:] {
		if before, ok := cutLast(line, hookClose); ok {
			body = append(body, before)
			return strings.Trim(strings.Join(body, "\n"), "\n"), nil
		}
		body = append(body, line)
	}
	return "", errors.New("the \"> {%\" block is never closed with \"%}\"")
}

// cutLast splits at the last occurrence of sep, which is what a closing marker
// wants: "%}" can legitimately appear inside the script before the one that
// ends it.
func cutLast(s, sep string) (string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return "", false
	}
	return s[:i], true
}

func isComment(line string) bool {
	_, ok := commentBody(line)
	return ok
}

// commentBody returns what follows a "#" or "//" comment marker.
func commentBody(line string) (string, bool) {
	switch {
	case strings.HasPrefix(line, "#"):
		return strings.TrimSpace(strings.TrimPrefix(line, "#")), true
	case strings.HasPrefix(line, "//"):
		return strings.TrimSpace(strings.TrimPrefix(line, "//")), true
	}
	return "", false
}

// isMethod reports whether a field looks like an HTTP method: letters only.
//
// The list is not checked against a set of known verbs. WebDAV, and plenty of
// internal APIs, use verbs that are not in any list worth hardcoding, and being
// wrong about one would mean a file that cannot be sent at all.
func isMethod(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}
