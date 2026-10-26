package jsonpath

import (
	"encoding/json"
	"strings"
	"testing"
)

const estate = `{"items": [
	{"name": "web-01", "cpu": 8, "power_state": "poweredOn", "tags": [{"category": "env", "name": "prod"}]},
	{"name": "web-02", "cpu": 2, "power_state": "poweredOff", "memory_gb": 1.5},
	{"name": "db-01", "cpu": 16, "power_state": "poweredOn", "annotation": null}
]}`

func decode(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestExecute(t *testing.T) {
	data := decode(t, estate)
	for _, tc := range []struct {
		name, template, want string
	}{
		{"field over a wildcard joins with spaces", "{.items[*].name}", "web-01 web-02 db-01"},
		{"index and negative index", "{.items[0].name},{.items[-1].name}", "web-01,db-01"},
		{"slice", "{.items[1:].name}", "web-02 db-01"},
		{"quoted field", "{.items[0]['power_state']}", "poweredOn"},
		{"range with a literal newline", `{range .items[*]}{.name}{"\t"}{.cpu}{"\n"}{end}`, "web-01\t8\nweb-02\t2\ndb-01\t16\n"},
		{"root inside range", "{range .items[0:1]}{$.items[2].name}{end}", "db-01"},
		{"filter on a number", "{.items[?(@.cpu>=8)].name}", "web-01 db-01"},
		{"filter on a string", `{.items[?(@.power_state=="poweredOff")].name}`, "web-02"},
		{"filter on existence", "{.items[?(@.memory_gb)].name}", "web-02"},
		{"decimal and nested object", "{.items[1].memory_gb} {.items[0].tags[0]}", `1.5 {"category":"env","name":"prod"}`},
		{"missing field and null print nothing", "[{.items[0].nope}][{.items[2].annotation}]", "[][]"},
		{"text outside braces is literal", `name: {.items[0].name}\n`, `name: web-01\n`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := Parse(tc.template)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := tmpl.Execute(&b, data); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.want {
				t.Fatalf("Execute(%q) = %q; want %q", tc.template, b.String(), tc.want)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	for _, tc := range []struct{ template, want string }{
		{"{.items[*]", "unclosed {"},
		{"{range .items[*]}{.name}", "{range} without {end}"},
		{"{end}", "{end} without {range}"},
		{"{..name}", "recursive descent"},
		{"{.items[0,1]}", "unions"},
		{"{name}", "must start with"},
		{"{.items[?(@.cpu>=eight)]}", "is not a number"},
	} {
		t.Run(tc.template, func(t *testing.T) {
			_, err := Parse(tc.template)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Parse(%q) error = %v; want %q", tc.template, err, tc.want)
			}
		})
	}
}

// Custom columns and --sort-by give one expression, with or without braces.
func TestParseExprValues(t *testing.T) {
	item := decode(t, `{"name": "web-01", "nics": [{"mac": "a"}, {"mac": "b"}]}`)
	for _, expr := range []string{".nics[*].mac", "{.nics[*].mac}"} {
		tmpl, err := ParseExpr(expr)
		if err != nil {
			t.Fatal(err)
		}
		if got := tmpl.Values(item); len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Fatalf("Values(%q) = %v", expr, got)
		}
	}
	if _, err := ParseExpr(`{.name}{"x"}`); err == nil {
		t.Fatal("two actions accepted as one expression")
	}
}
