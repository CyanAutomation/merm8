package benchmarks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

	// Assert rule extraction via public discovery path.
	wantRuleByID := map[string]string{
		"flowchart-val-a-first":  "no-cycles",
		"flowchart-val-b-second": "*",
		"flowchart-vio-c-third":  "max-depth",
		"flowchart-edg-d-fourth": "*",
		"alpha":                  "*",
	}
	for _, bc := range cases {
		wantRule, ok := wantRuleByID[bc.ID]
		if !ok {
			t.Fatalf("unexpected discovered case %q", bc.ID)
		}
		if bc.RuleID != wantRule {
			t.Fatalf("case %q rule_id = %q, want %q", bc.ID, bc.RuleID, wantRule)
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
		{
			name: "rule with extra whitespace",
			content: `graph TD
  A --> B
  %%     @rule:    no-cycles    
`,
			want: "no-cycles",
		},
		{
			name: "malformed annotation missing colon",
			content: `graph TD
  A --> B
  %% @rule no-cycles
`,
			want: "*",
		},
		{
			name: "malformed annotation empty rule",
			content: `graph TD
  A --> B
  %% @rule:
`,
			want: "",
		},
		{
			name: "multiple rule lines returns first",
			content: `graph TD
  A --> B
  %% @rule: no-cycles
  %% @rule: max-depth
`,
			want: "no-cycles",
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

func TestBenchmarkCase_JSONMarshaling_RequiredFieldsRoundTrip(t *testing.T) {
	bc := benchmarks.BenchmarkCase{
		ID:     "test-001",
		RuleID: "no-cycles",
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

	var roundTrip benchmarks.BenchmarkCase
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal benchmark case: %v", err)
	}

	if roundTrip.ID != bc.ID || roundTrip.RuleID != bc.RuleID {
		t.Fatalf("expected required fields to round-trip, want id=%q rule_id=%q, got id=%q rule_id=%q", bc.ID, bc.RuleID, roundTrip.ID, roundTrip.RuleID)
	}

	if !reflect.DeepEqual(roundTrip.ExpectedIssues, bc.ExpectedIssues) {
		t.Fatalf("expected expected_issues %v, got %v", bc.ExpectedIssues, roundTrip.ExpectedIssues)
	}
}

func TestBenchmarkCase_JSONMarshaling_OmitsOptionalDefaults(t *testing.T) {
	bc := benchmarks.BenchmarkCase{
		ID:             "test-optional",
		RuleID:         "*",
		ExpectedIssues: []benchmarks.ExpectedIssue{},
	}

	data, err := benchmarks.MarshalBenchmarkCase(bc)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var roundTrip benchmarks.BenchmarkCase
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal benchmark case: %v", err)
	}

	if roundTrip.Description != "" || roundTrip.DiagramPath != "" || roundTrip.Category != "" || roundTrip.DiagramType != "" {
		t.Fatalf("expected omitted optional text fields to keep zero values, got %+v", roundTrip)
	}

	if roundTrip.Tags != nil {
		t.Fatalf("expected omitted tags to remain nil, got %#v", roundTrip.Tags)
	}

	if string(roundTrip.Config) != "null" {
		t.Fatalf("expected omitted config to round-trip as JSON null, got %q", string(roundTrip.Config))
	}

	if !reflect.DeepEqual(roundTrip.ExpectedIssues, bc.ExpectedIssues) {
		t.Fatalf("expected expected_issues %v, got %v", bc.ExpectedIssues, roundTrip.ExpectedIssues)
	}
}

func TestBenchmarkCase_JSONMarshaling_InvalidRawConfig(t *testing.T) {
	bc := benchmarks.BenchmarkCase{
		ID:          "test-invalid",
		Description: "Invalid benchmark config should be rejected",
		RuleID:      "no-cycles",
		Category:    "violation",
		DiagramType: "flowchart",
		Config:      json.RawMessage(`{"enabled":`),
	}

	_, err := benchmarks.MarshalBenchmarkCase(bc)
	if err == nil {
		t.Fatal("expected invalid benchmark config to be rejected during marshaling")
	}

	if !strings.Contains(err.Error(), "benchmarks: invalid config payload") {
		t.Fatalf("expected stable invalid-config prefix, got %q", err.Error())
	}

	if !strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Fatalf("expected user-facing parse failure in error message, got %q", err.Error())
	}
}
