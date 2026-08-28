package transform

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"
)

func URLEncode(s string) string { return url.QueryEscape(s) }

func URLDecode(s string) (string, error) {
	out, err := url.QueryUnescape(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("not valid URL encoding: %w", err)
	}
	return out, nil
}

func Base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// Base64Decode accepts both standard and URL-safe alphabets, with or without
// padding -- you rarely control which flavour the thing you copied used.
func Base64Decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("-", "+", "_", "/", "\n", "", "\r", "", " ", "").Replace(s)
	if pad := len(s) % 4; pad != 0 {
		s += strings.Repeat("=", 4-pad)
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("not valid base64: %w", err)
	}
	return string(b), nil
}

func HTMLEncode(s string) string { return html.EscapeString(s) }
func HTMLDecode(s string) string { return html.UnescapeString(s) }

func HexEncode(s string) string { return hex.EncodeToString([]byte(s)) }

func HexDecode(s string) (string, error) {
	s = strings.NewReplacer(" ", "", "\n", "", "\r", "", "0x", "", ":", "").Replace(strings.TrimSpace(s))
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("not valid hex: %w", err)
	}
	return string(b), nil
}

// JWTDecode splits a token and pretty-prints its header and payload.
//
// It deliberately does not verify the signature: the point is to read a token
// you already have, and verifying would need the key.
func JWTDecode(s string) (string, error) {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("not a JWT: expected at least 2 dot-separated parts, got %d", len(parts))
	}
	var b strings.Builder
	for i, label := range []string{"header", "payload"} {
		raw, err := Base64Decode(parts[i])
		if err != nil {
			return "", fmt.Errorf("%s: %w", label, err)
		}
		var pretty any
		if err := json.Unmarshal([]byte(raw), &pretty); err != nil {
			return "", fmt.Errorf("%s is not JSON: %w", label, err)
		}
		out, err := json.MarshalIndent(pretty, "", "  ")
		if err != nil {
			return "", err
		}
		b.WriteString("// " + label + "\n")
		b.Write(out)
		b.WriteString("\n")
	}
	if len(parts) > 2 {
		b.WriteString("\n// signature (not verified)\n" + parts[2] + "\n")
	}
	return b.String(), nil
}

func registerEncoding(r *Registry) {
	addP := func(id, name string, tags []string, f func(string) string) {
		r.Add(Transform{ID: id, Name: name, Group: "Encoding", Tags: tags, Run: pure(f)})
	}
	addE := func(id, name string, tags []string, f func(string) (string, error)) {
		r.Add(Transform{ID: id, Name: name, Group: "Encoding", Tags: tags, Run: f})
	}
	addP("enc.urlencode", "URL encode", []string{"percent", "escape"}, URLEncode)
	addE("enc.urldecode", "URL decode", []string{"unescape"}, URLDecode)
	addP("enc.b64encode", "Base64 encode", []string{"b64"}, Base64Encode)
	addE("enc.b64decode", "Base64 decode", []string{"b64", "unbase64"}, Base64Decode)
	addP("enc.htmlencode", "HTML entity encode", []string{"entities"}, HTMLEncode)
	addP("enc.htmldecode", "HTML entity decode", nil, HTMLDecode)
	addP("enc.hexencode", "Hex encode", nil, HexEncode)
	addE("enc.hexdecode", "Hex decode", []string{"unhex"}, HexDecode)
	addE("enc.jwt", "Decode JWT", []string{"token", "jwt"}, JWTDecode)
}
