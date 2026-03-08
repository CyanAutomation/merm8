// Package benchmarks provides benchmarking and testing infrastructure for merm8 linting rules.
package benchmarks

import (
	"encoding/json"
	"fmt"
	"time"
)

// BenchmarkCase represents a single test case in the benchmark suite.
type BenchmarkCase struct {
	// Identifiers and metadata
	ID          string   `json:"id"`           // Unique case ID (e.g., "flowchart-no-cycles-001")
	Description string   `json:"description"`  // Human-readable test description
	DiagramPath string   `json:"diagram_path"` // Relative path to fixture (e.g., "cases/flowchart/valid/simple.mmd")
	RuleID      string   `json:"rule_id"`      // Rule being tested ("*" for all rules)
	Category    string   `json:"category"`     // "valid" | "violation" | "edge-case"
	DiagramType string   `json:"diagram_type"` // "flowchart" | "sequence" | "class" | "er" | "state"
	Tags        []string `json:"tags"`         // ["edge-case", "regression", "simple"]

	// Expected behavior
	ExpectedIssues []ExpectedIssue `json:"expected_issues"` // Issues this case should produce
	Config         json.RawMessage `json:"config"`          // Custom rule config (optional)

	// Metadata
	CreatedDate    string `json:"created_date"`     // ISO 8601 (e.g., "2026-03-06T00:00:00Z")
	AddedInVersion string `json:"added_in_version"` // Version when case was added (e.g., "v0.1.0")
}

// ExpectedIssue describes an issue expected to be raised by a rule.
type ExpectedIssue struct {
	RuleID   string `json:"rule_id"`  // Rule ID (e.g., "max-fanout")
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Count    int    `json:"count,omitempty"` // Expected count for this rule (0 = any)
}

// BenchmarkResults aggregates results from running a benchmark suite.
type BenchmarkResults struct {
	Timestamp       time.Time              `json:"timestamp"`
	Version         string                 `json:"version"`
	ExecutionTimeMs int64                  `json:"execution_time_ms"`
	TotalCases      int                    `json:"total_cases"`
	TotalPassed     int                    `json:"total_passed"`
	RuleMetrics     map[string]*RuleResult `json:"rule_metrics"`
	FailedCases     []CaseResult           `json:"failed_cases"`
}

// RuleResult contains metrics for a single rule across all its test cases.
type RuleResult struct {
	RuleID            string       `json:"rule_id"`
	TotalCases        int          `json:"total_cases"`
	Passed            int          `json:"passed"`
	DetectionRate     float64      `json:"detection_rate"`      // passed / TotalCases
	FalsePositives    int          `json:"false_positives"`     // Issues found not in expected
	TotalActualIssues int          `json:"total_actual_issues"` // Total issues reported across all cases
	FalsePositiveRate float64      `json:"false_positive_rate"` // false_positives / total_actual_issues
	SelectedCases     []CaseResult `json:"selected_cases"`      // Case-by-case details (failed only)
	AvgParseTimeMs    int64        `json:"avg_parse_time_ms"`
	AvgLintTimeMs     int64        `json:"avg_lint_time_ms"`
}

// CaseResult represents the result of running a single benchmark case.
type CaseResult struct {
	CaseID           string   `json:"case_id"`
	Passed           bool     `json:"passed"`
	Expected         []string `json:"expected"` // Expected issues (rule_id:severity format)
	Actual           []string `json:"actual"`   // Actual issues (rule_id:severity format)
	Issues           []string `json:"issues"`   // Mismatch descriptions
	ParseTimeMs      int64    `json:"parse_time_ms"`
	LintTimeMs       int64    `json:"lint_time_ms"`
	ActualIssuesFull []Issue  `json:"actual_issues_full"` // Full issue details for debugging
}

// Issue represents a lint issue returned by the engine.
type Issue struct {
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Line        *int   `json:"line,omitempty"`
	Column      *int   `json:"column,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// RegressionAlert indicates a detected regression from baseline.
type RegressionAlert struct {
	RuleID                   string  `json:"rule_id"`
	Type                     string  `json:"type"` // "detection_rate" or "performance"
	BaselineDetectionRate    float64 `json:"baseline_detection_rate,omitempty"`
	CurrentDetectionRate     float64 `json:"current_detection_rate,omitempty"`
	DropPercent              float64 `json:"drop_percent,omitempty"`
	BaselineAvgParseTimeMs   int64   `json:"baseline_avg_parse_time_ms,omitempty"`
	CurrentAvgParseTimeMs    int64   `json:"current_avg_parse_time_ms,omitempty"`
	ParseTimeIncreasePercent float64 `json:"parse_time_increase_percent,omitempty"`
	BaselineAvgLintTimeMs    int64   `json:"baseline_avg_lint_time_ms,omitempty"`
	CurrentAvgLintTimeMs     int64   `json:"current_avg_lint_time_ms,omitempty"`
	LintTimeIncreasePercent  float64 `json:"lint_time_increase_percent,omitempty"`
	Threshold                float64 `json:"threshold"` // e.g., 5.0 for 5%
	IsFailing                bool    `json:"is_failing"`
}

// CoverageMetrics describes test coverage statistics.
type CoverageMetrics struct {
	LowCoverageRules       []string // Rules with <5 test cases
	NoViolationsCasesRules []string // Rules with no violations-category test cases
	UncoveredDiagramTypes  []string // Diagram types with zero test cases
	FullySupported         bool     // All rules have >=5 cases and violations cases
}

// TrendMetric tracks a single metric value at a point in time.
type TrendMetric struct {
	Timestamp        time.Time `json:"timestamp"`
	RuleID           string    `json:"rule_id"`
	DetectionRate    float64   `json:"detection_rate"`
	FalsePositiveRate float64   `json:"false_positive_rate"`
	AvgParseTimeMs   int64     `json:"avg_parse_time_ms"`
	AvgLintTimeMs    int64     `json:"avg_lint_time_ms"`
	TotalCases       int       `json:"total_cases"`
	PassedCases      int       `json:"passed_cases"`
}

// TrendHistory maintains historical trend data for comparison and analysis.
type TrendHistory struct {
	BenchmarkVersion string        `json:"benchmark_version"`
	Trends           []TrendMetric `json:"trends"`
}

// MarshalBenchmarkCase marshals a BenchmarkCase to JSON.
//
// If BenchmarkCase.Config is provided, it must contain valid JSON. When the
// config payload is invalid, this function returns an error with the stable
// prefix "benchmarks: invalid config payload".
func MarshalBenchmarkCase(bc BenchmarkCase) ([]byte, error) {
	if bc.Config != nil {
		var cfg any
		if err := json.Unmarshal(bc.Config, &cfg); err != nil {
			return nil, fmt.Errorf("benchmarks: invalid config payload: %w", err)
		}
	}

	data, err := json.Marshal(bc)
	if err != nil {
		return nil, fmt.Errorf("benchmarks: marshal benchmark case: %w", err)
	}

	return data, nil
}
