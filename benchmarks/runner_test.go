package benchmarks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CyanAutomation/merm8/benchmarks"
)

func TestRunner_DiscoverCases(t *testing.T) {
	// Create temporary benchmark directory with test fixtures
	tmpDir := t.TempDir()
	benchDir := filepath.Join(tmpDir, "benchmarks")
	casesDir := filepath.Join(benchDir, "cases", "flowchart", "valid")
	os.MkdirAll(casesDir, 0755)

	// Create a simple test fixture
	fixtureFile := filepath.Join(casesDir, "simple.mmd")
	content := "graph TD\n  A --> B\n  %% @rule: no-duplicate-node-ids\n"
	if err := os.WriteFile(fixtureFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test fixture: %v", err)
	}

	// Create runner
	runner := benchmarks.NewRunner(benchDir, "/path/to/parser")
	_ = runner

	// This would require exposing discoverCases as a public method
	// For now, we'll test at a higher level via Run()
	t.Log("Runner discovery test setup complete (requires public API)")
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
			// This requires exposing extractRuleIDFromContent as public
			// For now test is placeholder
			t.Log("Rule extraction test placeholder")
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

	var roundTrip benchmarks.BenchmarkCase
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal benchmark case: %v", err)
	}

	if roundTrip.ID != bc.ID {
		t.Fatalf("expected id %q, got %q", bc.ID, roundTrip.ID)
	}

	if roundTrip.RuleID != bc.RuleID {
		t.Fatalf("expected rule_id %q, got %q", bc.RuleID, roundTrip.RuleID)
	}

	if len(roundTrip.ExpectedIssues) != len(bc.ExpectedIssues) {
		t.Fatalf("expected %d expected_issues, got %d", len(bc.ExpectedIssues), len(roundTrip.ExpectedIssues))
	}

	if got, want := roundTrip.ExpectedIssues[0], bc.ExpectedIssues[0]; got.RuleID != want.RuleID || got.Severity != want.Severity {
		t.Fatalf("unexpected expected_issues[0], want %+v, got %+v", want, got)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal benchmark case into map: %v", err)
	}

	schemaChecks := map[string]string{
		"id":              "string",
		"rule_id":         "string",
		"category":        "string",
		"diagram_type":    "string",
		"tags":            "[]any",
		"expected_issues": "[]any",
	}

	for key, wantType := range schemaChecks {
		v, ok := raw[key]
		if !ok {
			t.Fatalf("expected key %q to be present", key)
		}

		switch wantType {
		case "string":
			if _, ok := v.(string); !ok {
				t.Fatalf("expected key %q to be a string, got %T", key, v)
			}
		case "[]any":
			if _, ok := v.([]any); !ok {
				t.Fatalf("expected key %q to be an array, got %T", key, v)
			}
		default:
			t.Fatalf("unsupported schema check type %q", wantType)
		}
	}

	expectedIssues, _ := raw["expected_issues"].([]any)
	if len(expectedIssues) == 0 {
		t.Fatal("expected expected_issues to contain at least one element")
	}

	issueMap, ok := expectedIssues[0].(map[string]any)
	if !ok {
		t.Fatalf("expected expected_issues[0] to be object, got %T", expectedIssues[0])
	}

	if _, ok := issueMap["rule_id"].(string); !ok {
		t.Fatalf("expected expected_issues[0].rule_id to be string, got %T", issueMap["rule_id"])
	}

	if _, ok := issueMap["severity"].(string); !ok {
		t.Fatalf("expected expected_issues[0].severity to be string, got %T", issueMap["severity"])
	}
}

func TestBenchmarkCase_JSONMarshaling_InvalidInput(t *testing.T) {
	bc := benchmarks.BenchmarkCase{
		ID:          "test-invalid",
		Description: "Invalid config raw JSON should fail marshaling",
		RuleID:      "no-cycles",
		Category:    "violation",
		DiagramType: "flowchart",
		Config:      json.RawMessage(`{"enabled":`),
	}

	_, err := benchmarks.MarshalBenchmarkCase(bc)
	if err == nil {
		t.Fatal("expected marshal error for invalid benchmark case config, got nil")
	}
}
