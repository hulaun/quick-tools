package api

import (
	"errors"
	"strconv"
	"strings"
)

// curl, both directions.
//
// This is the pair that makes the tab useful somewhere it cannot run. You
// debug on remote machines, and a palette on your desktop does not go there --
// but a curl line does. Compose the request here, copy it as curl, paste it
// into the SSH session.
//
// The teaching half is deliberate too: seeing the curl form of every request
// you build is how -H, --data-urlencode and -u stop being things you look up.

// FormatCurl renders a request as a curl command line.
//
// The request should already be expanded: the point of the output is to work
// somewhere this app is not, and a {{base}} means nothing in a remote shell.
//
// Line continuations are backslashes, which is bash -- the shell this project's
// own build scripts run in, and the one on the far end of an ssh session. In
// cmd or PowerShell the line has to be joined back into one.
func FormatCurl(r Request) string {
	var b strings.Builder
	b.WriteString("curl")

	// -X is omitted for a plain GET, which is what curl does anyway and what
	// every "copy as cURL" produces. It is written out for everything else,
	// including a GET with a body -- where curl would otherwise quietly switch
	// to POST because there is data.
	method := r.Method
	if method == "" {
		method = "GET"
	}
	if !(method == "GET" && r.Body == "") {
		b.WriteString(" -X " + method)
	}

	b.WriteString(" " + shellQuote(r.URL))

	for _, h := range r.Headers {
		b.WriteString(" \\\n  -H " + shellQuote(h.Name+": "+h.Value))
	}

	if r.Body != "" {
		// --data-raw, not -d: -d strips newlines and treats a leading @ as "read
		// this file", which would turn a JSON body starting with @ into a file
		// read on whatever machine the line is pasted into.
		b.WriteString(" \\\n  --data-raw " + shellQuote(r.Body))
	}

	if r.Insecure {
		b.WriteString(" \\\n  --insecure")
	}
	if r.Timeout > 0 {
		b.WriteString(" \\\n  --max-time " + trimZeros(r.Timeout.Seconds()))
	}

	return b.String()
}

// shellQuote wraps a value in single quotes for a POSIX shell.
//
// Single quotes because nothing inside them is interpreted -- no $, no
// backtick, no backslash -- which is what a bearer token or a JSON body needs.
// The one thing they cannot contain is a single quote, so each one is closed,
// escaped and reopened -- the standard shell idiom, spelled out in the test
// rather than here, because gofmt rewrites that character sequence in a comment
// into a typographic quote and silently changes what it says.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// trimZeros renders a duration in seconds without a pointless ".0".
func trimZeros(secs float64) string {
	s := strconv.FormatFloat(secs, 'f', -1, 64)
	if s == "" {
		return "0"
	}
	return s
}

// ---------------------------------------------------------------------------
// Reserved: ParseCurl
// ---------------------------------------------------------------------------

// ErrParseCurlNotImplemented is what the stub returns.
var ErrParseCurlNotImplemented = errors.New("ParseCurl is not written yet")

// ParseCurl turns a curl command line into a Request.
//
// **This one is yours to write.** FormatCurl above is the worked example of the
// easy direction; this is the hard one, and it is a real parser rather than a
// formatter. Nothing else depends on it -- the tab is fully working without it
// -- so it can sit unwritten for as long as it likes.
//
// The red loop, kept out of the default suite so `go test ./...` stays green:
//
//	QUICKTOOLS_TODO=1 go test ./internal/api/ -run ParseCurl -v
//
// What it has to handle, all pinned in curl_test.go:
//
//	curl 'https://h/a'                      the simplest case
//	curl https://h/a                        unquoted
//	curl "https://h/a?x=1&y=2"              double quotes, where \ and $ escape
//	curl -X POST https://h/a                an explicit method
//	curl -XPOST https://h/a                 the same, joined -- curl allows it
//	curl -H 'A: 1' -H 'B: 2' https://h/a    repeated flags, order kept
//	curl --header 'A: 1' https://h/a        the long form of the same flag
//	curl -d '{"a":1}' https://h/a           a body, which implies POST
//	curl -d 'a=1' -X PUT https://h/a        unless -X says otherwise
//	curl --data-raw / --data / --data-binary
//	curl --data-urlencode 'q=a b'           which is NOT the same as --data
//	curl -u user:pass https://h/a           becomes an Authorization header
//	curl -k https://h/a                     --insecure, so Request.Insecure
//	curl --max-time 60 https://h/a          Request.Timeout
//	curl -L -s -v --compressed https://h/a  flags with no value, ignored
//
// And a line continued over several lines with a trailing backslash, which is
// how every real one arrives -- copied out of devtools or a colleague's chat
// message.
//
// The shape to reach for is the one java-to-json.js uses: a scanner that
// consumes the things which can hide a delimiter -- quotes, escapes, line
// continuations -- so that nothing downstream has to think about them, then a
// flag loop over the resulting words. The genuinely fiddly parts are that a
// double-quoted string honours backslash escapes while a single-quoted one
// does not, that some flags take a value and some do not, and that a flag can
// be joined to its value with no space.
//
// One ambiguity to decide rather than discover: an argument that is not a flag
// and not a flag's value is the URL, and there should be exactly one. Two of
// them means the line was mangled on its way here, and saying so beats picking
// one.
func ParseCurl(line string) (Request, error) {
	return Request{}, ErrParseCurlNotImplemented
}
