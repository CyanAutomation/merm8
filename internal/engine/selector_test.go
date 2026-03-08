package engine

import "testing"

func TestParseSelector_ParsesFields(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantNegate bool
		wantPrefix string
		wantValue  string
	}{
		{name: "node selector", raw: "node:A", wantPrefix: "node", wantValue: "A"},
		{name: "negated rule selector", raw: "!rule:max-fanout", wantNegate: true, wantPrefix: "rule", wantValue: "max-fanout"},
		{name: "subgraph selector", raw: "subgraph:cluster-1", wantPrefix: "subgraph", wantValue: "cluster-1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSelector(tc.raw)
			if !ok {
				t.Fatalf("parseSelector(%q) ok=false, want true", tc.raw)
			}
			if got.Negated != tc.wantNegate {
				t.Fatalf("parseSelector(%q) Negated=%v, want %v", tc.raw, got.Negated, tc.wantNegate)
			}
			if got.Prefix != tc.wantPrefix {
				t.Fatalf("parseSelector(%q) Prefix=%q, want %q", tc.raw, got.Prefix, tc.wantPrefix)
			}
			if got.Value != tc.wantValue {
				t.Fatalf("parseSelector(%q) Value=%q, want %q", tc.raw, got.Value, tc.wantValue)
			}
		})
	}
}

func TestParseSelector_RejectsUnknownPrefix(t *testing.T) {
	if _, ok := parseSelector("unknown:A"); ok {
		t.Fatal("expected unknown prefix selector to be rejected")
	}
}
