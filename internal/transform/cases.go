package transform

import (
	"strings"
	"unicode"
)

// splitWords breaks an identifier into its component words. It is the shared
// basis for every case conversion, so all of them agree on what a "word" is.
//
// It handles the three boundaries that matter in real code:
//
//	separators      user_name / user-name / "user name" -> user, name
//	lower-to-upper  userName                            -> user, Name
//	acronym runs    HTTPServer / parseJSONData          -> HTTP, Server / parse, JSON, Data
//	letter-to-digit addressLine1                        -> address, Line, 1
func splitWords(s string) []string {
	var words []string
	var cur []rune

	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}

	rs := []rune(s)
	for i, r := range rs {
		switch {
		case r == '_' || r == '-' || r == '.' || unicode.IsSpace(r):
			flush()
			continue
		case i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(rs[i-1]):
			// userName -> user | Name
			flush()
		case i > 0 && i+1 < len(rs) && unicode.IsUpper(r) &&
			unicode.IsUpper(rs[i-1]) && unicode.IsLower(rs[i+1]):
			// HTTPServer -> HTTP | Server: break before the last capital of a run
			flush()
		case i > 0 && unicode.IsDigit(r) != unicode.IsDigit(rs[i-1]):
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

func lowerAll(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.ToLower(w)
	}
	return out
}

func title(w string) string {
	if w == "" {
		return w
	}
	rs := []rune(strings.ToLower(w))
	rs[0] = unicode.ToUpper(rs[0])
	return string(rs)
}

func ToCamel(s string) string {
	words := splitWords(s)
	if len(words) == 0 {
		return ""
	}
	out := strings.ToLower(words[0])
	for _, w := range words[1:] {
		out += title(w)
	}
	return out
}

func ToPascal(s string) string {
	var b strings.Builder
	for _, w := range splitWords(s) {
		b.WriteString(title(w))
	}
	return b.String()
}

func ToSnake(s string) string  { return strings.Join(lowerAll(splitWords(s)), "_") }
func ToKebab(s string) string  { return strings.Join(lowerAll(splitWords(s)), "-") }
func ToScreaming(s string) string {
	return strings.ToUpper(ToSnake(s))
}

func ToTitle(s string) string {
	words := splitWords(s)
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = title(w)
	}
	return strings.Join(out, " ")
}

// ToSpaced lowercases and space-separates -- useful for turning an identifier
// into prose for a label or a commit message.
func ToSpaced(s string) string { return strings.Join(lowerAll(splitWords(s)), " ") }

func registerCases(r *Registry) {
	add := func(id, name string, tags []string, f func(string) string) {
		r.Add(Transform{ID: id, Name: name, Group: "Case", Tags: tags, Run: pure(f)})
	}
	add("case.camel", "camelCase", []string{"cc", "lowerCamel"}, ToCamel)
	add("case.pascal", "PascalCase", []string{"pc", "upperCamel"}, ToPascal)
	add("case.snake", "snake_case", []string{"sc", "underscore"}, ToSnake)
	add("case.screaming", "SCREAMING_SNAKE_CASE", []string{"const", "screaming"}, ToScreaming)
	add("case.kebab", "kebab-case", []string{"kc", "dash", "slug"}, ToKebab)
	add("case.title", "Title Case", []string{"tc"}, ToTitle)
	add("case.spaced", "spaced words", []string{"prose"}, ToSpaced)
	add("case.upper", "UPPERCASE", []string{"upper"}, strings.ToUpper)
	add("case.lower", "lowercase", []string{"lower"}, strings.ToLower)
}
