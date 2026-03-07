package benchmarks_test

import (
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
}
