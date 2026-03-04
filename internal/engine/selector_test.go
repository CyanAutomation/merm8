package engine

import "testing"

func TestParseSelectorExamples(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want suppressionSelector
		ok   bool
	}{
		{name: "node selector", raw: "node:A", want: suppressionSelector{Prefix: "node", Value: "A"}, ok: true},
		{name: "negated selector", raw: "!rule:max-fanout", want: suppressionSelector{Negated: true, Prefix: "rule", Value: "max-fanout"}, ok: true},
		{name: "value with colon", raw: `node:team:alpha`, want: suppressionSelector{Prefix: "node", Value: `team:alpha`}, ok: true},
		{name: "empty", raw: "", ok: false},
		{name: "missing value", raw: "subgraph:", ok: false},
		{name: "missing prefix", raw: ":A", ok: false},
		{name: "unknown prefix", raw: "actor:A", ok: false},
		{name: "uppercase prefix invalid", raw: "NoDe:A", ok: false},
		{name: "whitespace invalid", raw: " node:A", ok: false},
		{name: "space after negation invalid", raw: "! node:A", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSelector(tc.raw)
			if ok != tc.ok {
				t.Fatalf("parseSelector(%q) ok=%v, want %v", tc.raw, ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if got != tc.want {
				t.Fatalf("parseSelector(%q)=%#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}
