package engine

import "testing"

func TestParseSelectorExamples(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		// Keep parser-only edge coverage compact; selector behavior is covered via API tests.
		{name: "value with colon", raw: `node:team:alpha`, ok: true},
		{name: "empty", raw: "", ok: false},
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
			_ = got
		})
	}
}
