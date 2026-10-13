package engine

import "testing"

func TestParseSelector_ValidatesFormatAndSupport(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  suppressionSelector
		valid bool
	}{
		{name: "node selector", raw: "node:A", want: suppressionSelector{Prefix: "node", Value: "A"}, valid: true},
		{name: "negated rule selector", raw: "!rule:max-fanout", want: suppressionSelector{Negated: true, Prefix: "rule", Value: "max-fanout"}, valid: true},
		{name: "subgraph selector", raw: "subgraph:cluster-1", want: suppressionSelector{Prefix: "subgraph", Value: "cluster-1"}, valid: true},
		{name: "node IDs may contain colons", raw: "node:team:alpha", want: suppressionSelector{Prefix: "node", Value: "team:alpha"}, valid: true},
		{name: "empty selector", raw: "", valid: false},
		{name: "missing separator", raw: "node", valid: false},
		{name: "missing value", raw: "node:", valid: false},
		{name: "unsupported prefix", raw: "unknown:A", valid: false},
		{name: "whitespace after negation", raw: "! unknown:A", valid: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSelector(tc.raw)
			if ok != tc.valid {
				t.Fatalf("parseSelector(%q) = ok %v, want %v", tc.raw, ok, tc.valid)
			}
			if got != tc.want {
				t.Fatalf("parseSelector(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}
