package transform

import (
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"userName", []string{"user", "Name"}},
		{"UserName", []string{"User", "Name"}},
		{"user_name", []string{"user", "name"}},
		{"user-name", []string{"user", "name"}},
		{"user name", []string{"user", "name"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"parseJSONData", []string{"parse", "JSON", "Data"}},
		{"addressLine1", []string{"address", "Line", "1"}},
		{"ID", []string{"ID"}},
		{"", nil},
	}
	for _, c := range cases {
		got := splitWords(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitWords(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestCaseConversions(t *testing.T) {
	// Every conversion is checked against the same inputs, so a change to word
	// splitting cannot quietly fix one style while breaking another.
	cases := []struct {
		in                                                     string
		camel, pascal, snake, screaming, kebab, titled, spaced string
	}{
		{
			in: "userName",
			camel: "userName", pascal: "UserName", snake: "user_name",
			screaming: "USER_NAME", kebab: "user-name", titled: "User Name",
			spaced: "user name",
		},
		{
			in: "HTTP_SERVER_PORT",
			camel: "httpServerPort", pascal: "HttpServerPort", snake: "http_server_port",
			screaming: "HTTP_SERVER_PORT", kebab: "http-server-port",
			titled: "Http Server Port", spaced: "http server port",
		},
		{
			in: "parseJSONData",
			camel: "parseJsonData", pascal: "ParseJsonData", snake: "parse_json_data",
			screaming: "PARSE_JSON_DATA", kebab: "parse-json-data",
			titled: "Parse Json Data", spaced: "parse json data",
		},
	}
	for _, c := range cases {
		check := func(name, got, want string) {
			t.Helper()
			if got != want {
				t.Errorf("%s(%q) = %q, want %q", name, c.in, got, want)
			}
		}
		check("ToCamel", ToCamel(c.in), c.camel)
		check("ToPascal", ToPascal(c.in), c.pascal)
		check("ToSnake", ToSnake(c.in), c.snake)
		check("ToScreaming", ToScreaming(c.in), c.screaming)
		check("ToKebab", ToKebab(c.in), c.kebab)
		check("ToTitle", ToTitle(c.in), c.titled)
		check("ToSpaced", ToSpaced(c.in), c.spaced)
	}
}

// Round-tripping guards the property that matters day to day: an identifier can
// bounce between styles without degrading.
func TestCaseRoundTrip(t *testing.T) {
	for _, in := range []string{"userName", "user_name", "UserName", "user-name"} {
		if got := ToCamel(ToSnake(in)); got != ToCamel(in) {
			t.Errorf("round trip %q: camel(snake(x)) = %q, want %q", in, got, ToCamel(in))
		}
	}
}
