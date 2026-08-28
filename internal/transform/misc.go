package transform

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID(string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// StripANSI removes colour escapes, for pasting terminal output somewhere that
// renders them as garbage.
func StripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

var wsRE = regexp.MustCompile(`[ \t]+`)

func CollapseWhitespace(s string) string {
	return strings.TrimSpace(wsRE.ReplaceAllString(s, " "))
}

// TimestampToDate accepts seconds, milliseconds, microseconds or nanoseconds
// and renders local and UTC. The unit is inferred from magnitude, which is
// reliable for any timestamp within a few centuries of now.
func TimestampToDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return "", fmt.Errorf("not an integer timestamp: %w", err)
	}
	var t time.Time
	switch {
	case n > 1e17:
		t = time.Unix(0, n)
	case n > 1e14:
		t = time.UnixMicro(n)
	case n > 1e11:
		t = time.UnixMilli(n)
	default:
		t = time.Unix(n, 0)
	}
	return fmt.Sprintf("%s\n%s",
		t.Local().Format("2006-01-02 15:04:05.000 -07:00"),
		t.UTC().Format("2006-01-02 15:04:05.000 UTC")), nil
}

// DateToTimestamp parses the common layouts and returns Unix seconds and millis.
func DateToTimestamp(s string) (string, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02 15:04:05.000", "2006-01-02 15:04:05",
		"2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02",
		"02/01/2006 15:04:05", "02/01/2006",
	}
	for _, l := range layouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			return fmt.Sprintf("%d\n%d", t.Unix(), t.UnixMilli()), nil
		}
	}
	return "", fmt.Errorf("unrecognised date format")
}

func registerMisc(r *Registry) {
	addP := func(id, name string, tags []string, f func(string) string) {
		r.Add(Transform{ID: id, Name: name, Group: "Misc", Tags: tags, Run: pure(f)})
	}
	addE := func(id, name string, tags []string, f func(string) (string, error)) {
		r.Add(Transform{ID: id, Name: name, Group: "Misc", Tags: tags, Run: f})
	}
	addE("misc.uuid", "Generate UUID v4", []string{"guid", "uuid"}, NewUUID)
	addP("misc.stripansi", "Strip ANSI colour codes", []string{"terminal"}, StripANSI)
	addP("misc.collapsews", "Collapse whitespace", []string{"squeeze"}, CollapseWhitespace)
	addE("misc.ts2date", "Timestamp -> date", []string{"epoch", "unix", "time"}, TimestampToDate)
	addE("misc.date2ts", "Date -> timestamp", []string{"epoch", "unix"}, DateToTimestamp)
}
