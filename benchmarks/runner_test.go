package benchmarks_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/benchmarks"
)

func TestExtractRuleID(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "rule with multiple IDs",
			content: `graph TD
  A --> B
  %% @rule: no-cycles, max-fanout
`,
			want: "no-cycles",
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
			name: "malformed annotation empty rule",
			content: `graph TD
  A --> B
  %% @rule:
`,
			want: "",
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
