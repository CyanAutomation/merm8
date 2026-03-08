package engine

import "testing"

func TestParseSelector_AcceptsSupportedSelectors(t *testing.T) {
	for _, raw := range []string{"node:A", "!rule:max-fanout", "subgraph:cluster-1"} {
		t.Run(raw, func(t *testing.T) {
			if _, ok := parseSelector(raw); !ok {
				t.Fatalf("parseSelector(%q) ok=false, want true", raw)
			}
		})
	}
}

func TestParseSelector_RejectsMalformedOrUnsupportedSelectors(t *testing.T) {
	for _, raw := range []string{"", "node", "node:", "unknown:A", "! unknown:A"} {
		t.Run(raw, func(t *testing.T) {
			if _, ok := parseSelector(raw); ok {
				t.Fatalf("parseSelector(%q) ok=true, want false", raw)
			}
		})
	}
}
