package engine

import "testing"

func TestParseSelector_ValidatesFormatAndSupport(t *testing.T) {
	tests := []struct {
		raw   string
		valid bool
	}{
		// Valid selectors
		{"node:A", true},
		{"!rule:max-fanout", true},
		{"subgraph:cluster-1", true},
		// Malformed or unsupported
		{"", false},
		{"node", false},
		{"node:", false},
		{"unknown:A", false},
		{"! unknown:A", false},
	}

	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			_, ok := parseSelector(tc.raw)
			if ok != tc.valid {
				t.Fatalf("parseSelector(%q) = ok %v, want %v", tc.raw, ok, tc.valid)
			}
		})
	}
}
