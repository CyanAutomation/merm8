package benchmarks_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CyanAutomation/merm8/benchmarks"
)

func TestRunner_DiscoverCases(t *testing.T) {
	tmpDir := t.TempDir()
	benchDir := filepath.Join(tmpDir, "benchmarks")

	mustWrite := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(benchDir, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Valid fixtures across discovery paths.
	mustWrite("cases/flowchart/valid/a-first.mmd", "graph TD\n  A --> B\n  %% @rule: no-cycles\n")
	mustWrite("cases/flowchart/valid/b-second.mmd", "graph TD\n  C --> D\n")
	mustWrite("cases/flowchart/violations/c-third.mmd", "graph TD\n  E --> F\n  %% @rule: max-depth\n")
	mustWrite("cases/flowchart/edge-cases/d-fourth.mmd", "graph TD\n  G --> H\n")
	mustWrite("cases/sequence/alpha.mmd", "sequenceDiagram\n  A->>B: ping\n")

	// Invalid fixture for discovery contract: non-.mmd files are ignored.
	mustWrite("cases/flowchart/valid/zzz-invalid.txt", "not a mermaid fixture")

	runner := benchmarks.NewRunner(benchDir, "/path/to/parser")
	cases, err := runner.DiscoverCases()
	if err != nil {
		t.Fatalf("discover cases: %v", err)
	}

	if got, want := len(cases), 5; got != want {
		t.Fatalf("discovered case count = %d, want %d", got, want)
	}

	// Assert stable ordering (diagram type + category scan order and filename order).
	var gotIDs []string
	for _, bc := range cases {
		gotIDs = append(gotIDs, bc.ID)
	}
	wantIDs := []string{
		"flowchart-val-a-first",
		"flowchart-val-b-second",
		"flowchart-vio-c-third",
		"flowchart-edg-d-fourth",
		"alpha",
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("discovered IDs = %v, want %v", gotIDs, wantIDs)
	}

	// Assert diagram type/category parsing.
	if cases[0].DiagramType != "flowchart" || cases[0].Category != "valid" {
		t.Fatalf("first case type/category = %s/%s, want flowchart/valid", cases[0].DiagramType, cases[0].Category)
	}
	if cases[2].DiagramType != "flowchart" || cases[2].Category != "violations" {
		t.Fatalf("third case type/category = %s/%s, want flowchart/violations", cases[2].DiagramType, cases[2].Category)
	}
	if cases[4].DiagramType != "sequence" || cases[4].Category != "" {
		t.Fatalf("fifth case type/category = %s/%s, want sequence/empty", cases[4].DiagramType, cases[4].Category)
	}

	for _, bc := range cases {
		if bc.ID == "zzz-invalid" || filepath.Ext(bc.DiagramPath) == ".txt" {
			t.Fatalf("invalid fixture should be ignored, got case %+v", bc)
		}
	}
}

func TestExtractRuleID(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "rule with single ID",
			content: `graph TD
  A --> B
  %% @rule: no-cycles
`,
			want: "no-cycles",
		},
		{
			name: "rule with multiple IDs",
			content: `graph TD
  A --> B
  %% @rule: no-cycles, max-fanout
`,
			want: "no-cycles",
		},
		{
			name: "no rule annotation",
			content: `graph TD
  A --> B
`,
			want: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := benchmarks.ExtractRuleIDFromContent(tt.content)
			if got != tt.want {
				t.Fatalf("ExtractRuleIDFromContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBenchmarkCase_JSONMarshaling(t *testing.T) {
	bc := benchmarks.BenchmarkCase{
		ID:          "test-001",
		Description: "Test case",
		RuleID:      "no-cycles",
		Category:    "violation",
		DiagramType: "flowchart",
		Tags:        []string{"test"},
		ExpectedIssues: []benchmarks.ExpectedIssue{
			{RuleID: "no-cycles", Severity: "error"},
		},
	}

	// Should marshal without error
	data, err := benchmarks.MarshalBenchmarkCase(bc)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}
