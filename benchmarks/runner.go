// Package benchmarks provides benchmarking and testing infrastructure for merm8 linting rules.
package benchmarks

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// Runner orchestrates benchmark execution.
type Runner struct {
	parserScript   string
	benchmarkDir   string
	casesDir       string
	baselinesDir   string
	reportsDir     string
	htmlOutputPath string
	ruleFilter     string
	categoryFilter string
	verbose        bool
}

// NewRunner creates a new benchmark runner.
func NewRunner(benchmarkDir string, parserScript string) *Runner {
	return &Runner{
		benchmarkDir: benchmarkDir,
		parserScript: parserScript,
		casesDir:     filepath.Join(benchmarkDir, "cases"),
		baselinesDir: filepath.Join(benchmarkDir, "baselines"),
		reportsDir:   filepath.Join(benchmarkDir, "reports"),
		htmlOutputPath: filepath.Join(
			filepath.Dir(benchmarkDir),
			"benchmark.html",
		),
	}
}

// RunOptions defines options for running benchmarks.
type RunOptions struct {
	RuleFilter          string // "no-cycles" or "" for all
	CategoryFilter      string // "valid", "violation", "edge-case" or "" for all
	Verbose             bool
	CompareTo           string  // Path to baseline JSON or ""
	RegressionThreshold float64 // e.g., 5.0 for 5%
}

// Run executes all benchmark cases and generates reports.
func (r *Runner) Run(opts RunOptions) error {
	r.ruleFilter = opts.RuleFilter
	r.categoryFilter = opts.CategoryFilter
	r.verbose = opts.Verbose

	start := time.Now()

	// Discover cases from fixtures
	cases, err := r.discoverCases()
	if err != nil {
		return fmt.Errorf("discover cases: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Discovered %d benchmark cases\n", len(cases))
	}

	// Execute all cases
	results, err := r.executeCases(cases)
	if err != nil {
		return fmt.Errorf("execute cases: %w", err)
	}

	results.ExecutionTimeMs = time.Since(start).Milliseconds()
	results.Version = getVersion()
	results.Timestamp = time.Now()

	// Generate reports
	if err := r.generateReports(results); err != nil {
		return fmt.Errorf("generate reports: %w", err)
	}

	// Print summary
	r.printSummary(results)

	// Compare against baseline if requested
	if opts.CompareTo != "" {
		regressions, err := r.compareToBaseline(results, opts.CompareTo, opts.RegressionThreshold)
		if err != nil {
			return fmt.Errorf("compare baseline: %w", err)
		}
		if len(regressions) > 0 {
			r.printRegressions(regressions)
		}
	}

	return nil
}

// DiscoverCases scans the fixtures directory and discovers all test cases.
//
// This public wrapper supports direct testing of discovery behavior without
// running the full benchmark execution pipeline.
func (r *Runner) DiscoverCases() ([]*BenchmarkCase, error) {
	return r.discoverCases()
}

// discoverCases scans the fixtures directory and discovers all test cases.
func (r *Runner) discoverCases() ([]*BenchmarkCase, error) {
	var cases []*BenchmarkCase

	// Walk through diagram types (flowchart, sequence, class, er, state)
	diagramTypes := []string{"flowchart", "sequence", "class", "er", "state"}

	for _, dt := range diagramTypes {
		dtDir := filepath.Join(r.casesDir, dt)
		if _, err := os.Stat(dtDir); os.IsNotExist(err) {
			continue
		}

		// For flowchart, also check subcategories
		if dt == "flowchart" {
			categories := []string{"valid", "violations", "edge-cases"}
			for _, cat := range categories {
				catDir := filepath.Join(dtDir, cat)
				if _, err := os.Stat(catDir); os.IsNotExist(err) {
					continue
				}

				entryCases, err := r.discoverCasesInDir(catDir, dt, cat)
				if err != nil {
					return nil, err
				}
				cases = append(cases, entryCases...)
			}
		} else {
			// For other diagram types, scan directory directly
			entryCases, err := r.discoverCasesInDir(dtDir, dt, "")
			if err != nil {
				return nil, err
			}
			cases = append(cases, entryCases...)
		}
	}

	return cases, nil
}

// discoverCasesInDir discovers cases in a specific directory.
func (r *Runner) discoverCasesInDir(dir string, diagramType, category string) ([]*BenchmarkCase, error) {
	var cases []*BenchmarkCase

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mmd") {
			relPath, err := filepath.Rel("/workspaces/merm8", filepath.Join(dir, entry.Name()))
			if err != nil {
				relPath = filepath.Join(dir, entry.Name())
			}

			// Extract rule ID from metadata comment if present
			ruleID := "*"
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err == nil {
				ruleID = extractRuleIDFromContent(string(content))
			}

			// Create case from discovered fixture
			caseID := strings.TrimSuffix(entry.Name(), ".mmd")
			if category != "" {
				caseID = fmt.Sprintf("%s-%s-%s", diagramType, category[:3], caseID)
			}

			// For "valid" and "edge-cases" where no violations are expected, expect no issues
			// For "violations" and edge-cases with explicit violations, expect issues matching the rule
			// Severity depends on the rule: max-depth and max-fanout use warnings,
			// other rules use errors (based on rule configuration)
			expectedIssues := []ExpectedIssue{}

			// Check if the content indicates violations are expected (has violations mentioned)
			// For edge-cases, default to no issues expected (boundary/passing cases)
			// Violations folder always expects issues
			shouldExpectIssue := category == "violations"

			if shouldExpectIssue {
				if ruleID != "*" && ruleID != "" {
					// Determine severity based on rule type
					severity := "error"
					if ruleID == "max-depth" || ruleID == "max-fanout" {
						severity = "warning"
					}
					expectedIssues = append(expectedIssues, ExpectedIssue{
						RuleID:   ruleID,
						Severity: severity,
					})
				}
			}
			// "valid", "edge-cases", and non-violations expect no issues

			bc := &BenchmarkCase{
				ID:             caseID,
				Description:    fmt.Sprintf("%s (%s)", strings.ReplaceAll(caseID, "-", " "), category),
				DiagramPath:    relPath,
				RuleID:         ruleID,
				Category:       category,
				DiagramType:    diagramType,
				Tags:           []string{},
				ExpectedIssues: expectedIssues,
				CreatedDate:    time.Now().Format(time.RFC3339),
				AddedInVersion: "v0.1.0",
			}

			// Apply filters
			if r.ruleFilter != "" && bc.RuleID != "*" && bc.RuleID != r.ruleFilter {
				continue
			}
			if r.categoryFilter != "" && bc.Category != r.categoryFilter {
				continue
			}

			cases = append(cases, bc)
		}
	}

	return cases, nil
}

// extractRuleIDFromContent parses the mermaid diagram content for rule metadata comments.
// Format: %% @rule: rule-id or %% @rules: rule-id1,rule-id2
func extractRuleIDFromContent(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "%%") {
			if strings.Contains(line, "@rule:") {
				parts := strings.SplitN(line, "@rule:", 2)
				if len(parts) > 1 {
					ruleID := strings.TrimSpace(parts[1])
					// Take first rule if multiple comma-separated
					if idx := strings.Index(ruleID, ","); idx > 0 {
						ruleID = ruleID[:idx]
					}
					return strings.TrimSpace(ruleID)
				}
			}
		}
	}
	return "*"
}

// ExtractRuleIDFromContent parses mermaid metadata comments and returns the
// first declared rule ID, or "*" when no metadata is present.
func ExtractRuleIDFromContent(content string) string {
	return extractRuleIDFromContent(content)
}

// executeCases runs all benchmark cases and aggregates results.
func (r *Runner) executeCases(cases []*BenchmarkCase) (*BenchmarkResults, error) {
	results := &BenchmarkResults{
		TotalCases:  len(cases),
		RuleMetrics: make(map[string]*RuleResult),
	}

	// Create engine once
	eng := engine.New()

	// Track case results for failed cases and overall passing
	var failedCases []CaseResult
	ruleResults := make(map[string]*RuleResult)

	for _, bc := range cases {
		caseResult, err := r.executeCase(bc, eng)
		if err != nil {
			if r.verbose {
				fmt.Printf("Error executing case %s: %v\n", bc.ID, err)
			}
			continue
		}

		// Track by rule
		ruleIDs := []string{}
		if bc.RuleID == "*" {
			// Extract rule IDs from expected issues
			seen := make(map[string]bool)
			for _, exp := range caseResult.Expected {
				parts := strings.Split(exp, ":")
				if len(parts) > 0 && !seen[parts[0]] {
					ruleIDs = append(ruleIDs, parts[0])
					seen[parts[0]] = true
				}
			}
			// If no expected issues, extract from actual
			if len(ruleIDs) == 0 {
				for _, actual := range caseResult.ActualIssuesFull {
					if !seen[actual.RuleID] {
						ruleIDs = append(ruleIDs, actual.RuleID)
						seen[actual.RuleID] = true
					}
				}
			}
		} else {
			ruleIDs = []string{bc.RuleID}
		}

		// Update metrics for each rule
		for _, ruleID := range ruleIDs {
			if rr, ok := ruleResults[ruleID]; !ok {
				ruleResults[ruleID] = &RuleResult{
					RuleID:     ruleID,
					TotalCases: 1,
					Passed:     0,
				}
				if caseResult.Passed {
					ruleResults[ruleID].Passed = 1
				}
			} else {
				rr.TotalCases++
				if caseResult.Passed {
					rr.Passed++
				}
			}
		}

		if !caseResult.Passed {
			failedCases = append(failedCases, caseResult)
		} else {
			results.TotalPassed++
		}

		if r.verbose {
			fmt.Printf("  %s: %v\n", bc.ID, caseResult.Passed)
		}
	}

	// Aggregate metrics by rule
	for ruleID, rr := range ruleResults {
		rr.DetectionRate = float64(rr.Passed) / float64(rr.TotalCases)
		if rr.TotalCases > 0 {
			rr.DetectionRate = math.Round(rr.DetectionRate*10000) / 10000 // 4 decimals
		}
		results.RuleMetrics[ruleID] = rr
	}

	results.FailedCases = failedCases

	return results, nil
}

// executeCase runs a single benchmark case.
func (r *Runner) executeCase(bc *BenchmarkCase, eng *engine.Engine) (CaseResult, error) {
	result := CaseResult{
		CaseID: bc.ID,
	}

	// Read diagram content
	content, err := os.ReadFile(bc.DiagramPath)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("failed to read file: %v", err))
		return result, nil
	}

	code := string(content)

	// Parse
	parseStart := time.Now()
	p, err := parser.New(r.parserScript)
	if err != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("failed to create parser: %v", err))
		return result, nil
	}

	diagram, syntaxErr, parseErr := p.Parse(code)
	result.ParseTimeMs = time.Since(parseStart).Milliseconds()

	// Handle parser errors
	if parseErr != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("parse error: %v", parseErr))
		result.Passed = bc.Category == "valid" // If we expect valid, this is a failure
		return result, nil
	}

	if syntaxErr != nil {
		result.Issues = append(result.Issues, fmt.Sprintf("syntax error: %s", syntaxErr.Message))
		result.Passed = bc.Category == "valid" // If we expect valid, this is a failure
		return result, nil
	}

	// Lint
	lintStart := time.Now()
	var cfg rules.Config
	if bc.Config != nil && len(bc.Config) > 0 {
		if err := json.Unmarshal(bc.Config, &cfg); err != nil {
			result.Issues = append(result.Issues, fmt.Sprintf("invalid config: %v", err))
		}
	}
	issues := eng.Run(diagram, cfg)
	result.LintTimeMs = time.Since(lintStart).Milliseconds()

	// Convert issues to comparable format
	actualIssues := []string{}
	for _, issue := range issues {
		actualIssues = append(actualIssues, fmt.Sprintf("%s:%s", issue.RuleID, issue.Severity))
		result.ActualIssuesFull = append(result.ActualIssuesFull, Issue{
			RuleID:   issue.RuleID,
			Severity: issue.Severity,
			Message:  issue.Message,
			Line:     issue.Line,
			Column:   issue.Column,
		})
	}
	result.Actual = actualIssues

	// Convert expected issues to comparable format
	expectedIssues := []string{}
	for _, exp := range bc.ExpectedIssues {
		expectedIssues = append(expectedIssues, fmt.Sprintf("%s:%s", exp.RuleID, exp.Severity))
	}
	result.Expected = expectedIssues

	// Compare
	result.Passed = compareIssues(expectedIssues, actualIssues)
	if !result.Passed {
		result.Issues = append(result.Issues, fmt.Sprintf("expected %v, got %v", expectedIssues, actualIssues))
	}

	return result, nil
}

// compareIssues checks if actual matches expected (order-independent).
func compareIssues(expected, actual []string) bool {
	if len(expected) != len(actual) {
		return false
	}

	expectedMap := make(map[string]int)
	for _, e := range expected {
		expectedMap[e]++
	}

	actualMap := make(map[string]int)
	for _, a := range actual {
		actualMap[a]++
	}

	return mapsEqual(expectedMap, actualMap)
}

// mapsEqual checks if two maps are equal.
func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// generateReports creates JSON and HTML reports from benchmark results.
func (r *Runner) generateReports(results *BenchmarkResults) error {
	// Ensure reports directory exists
	if err := os.MkdirAll(r.reportsDir, 0755); err != nil {
		return err
	}

	// JSON report
	jsonPath := filepath.Join(r.reportsDir, "latest-results.json")
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return err
	}

	// HTML report (new path + one-release compatibility file)
	htmlPath := r.htmlOutputPath
	if htmlPath == "" {
		htmlPath = filepath.Join(filepath.Dir(r.benchmarkDir), "benchmark.html")
	}
	if err := os.MkdirAll(filepath.Dir(htmlPath), 0755); err != nil {
		return err
	}

	htmlContent := r.generateHTMLReport(results)
	if err := os.WriteFile(htmlPath, []byte(htmlContent), 0644); err != nil {
		return err
	}

	legacyHTMLPath := filepath.Join(r.reportsDir, "index.html")
	if legacyHTMLPath != htmlPath {
		if err := os.MkdirAll(filepath.Dir(legacyHTMLPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(legacyHTMLPath, []byte(htmlContent), 0644); err != nil {
			return err
		}
	}

	fmt.Printf("\nReports generated:\n  JSON: %s\n  HTML: %s\n", jsonPath, htmlPath)
	if legacyHTMLPath != htmlPath {
		fmt.Printf("  HTML (legacy): %s\n", legacyHTMLPath)
	}
	return nil
}

// generateHTMLReport creates an HTML representation of benchmark results.
func (r *Runner) generateHTMLReport(results *BenchmarkResults) string {
	funcs := template.FuncMap{
		"mul": func(a, b float64) float64 {
			return a * b
		},
		"formatPct": func(f float64) string {
			return fmt.Sprintf("%.2f", f*100)
		},
		"join": func(arr []string, sep string) string {
			return strings.Join(arr, sep)
		},
	}

	tmpl := template.Must(template.New("report").Funcs(funcs).Parse(htmlReportTemplate))
	var buf bytes.Buffer

	// Prepare data for template
	passRate := 0.0
	if results.TotalCases > 0 {
		passRate = float64(results.TotalPassed) / float64(results.TotalCases) * 100
	}

	// Sort rule metrics by rule ID
	var sortedRules []*RuleResult
	for _, rm := range results.RuleMetrics {
		sortedRules = append(sortedRules, rm)
	}
	sort.Slice(sortedRules, func(i, j int) bool {
		return sortedRules[i].RuleID < sortedRules[j].RuleID
	})

	data := map[string]interface{}{
		"Timestamp":    results.Timestamp.Format("2006-01-02 15:04:05"),
		"Version":      results.Version,
		"TotalCases":   results.TotalCases,
		"TotalPassed":  results.TotalPassed,
		"TotalFailed":  results.TotalCases - results.TotalPassed,
		"PassRate":     fmt.Sprintf("%.1f%%", passRate),
		"ExecutionSec": float64(results.ExecutionTimeMs) / 1000.0,
		"Rules":        sortedRules,
		"FailedCases":  results.FailedCases,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("<h1>Error generating report: %v</h1>", err)
	}

	return buf.String()
}

// compareToBaseline compares current results against a baseline and returns regressions.
func (r *Runner) compareToBaseline(results *BenchmarkResults, baselinePath string, threshold float64) ([]RegressionAlert, error) {
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}

	var baseline BenchmarkResults
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}

	var alerts []RegressionAlert
	for ruleID, currentMetrics := range results.RuleMetrics {
		baselineMetrics, ok := baseline.RuleMetrics[ruleID]
		if !ok {
			continue // New rule
		}

		dropPercent := (baselineMetrics.DetectionRate - currentMetrics.DetectionRate) * 100
		if dropPercent > threshold {
			alerts = append(alerts, RegressionAlert{
				RuleID:                ruleID,
				BaselineDetectionRate: baselineMetrics.DetectionRate,
				CurrentDetectionRate:  currentMetrics.DetectionRate,
				DropPercent:           math.Round(dropPercent*100) / 100,
				Threshold:             threshold,
				IsFailing:             dropPercent > threshold,
			})
		}
	}

	return alerts, nil
}

// printSummary prints a text summary of benchmark results.
func (r *Runner) printSummary(results *BenchmarkResults) {
	passRate := 0.0
	if results.TotalCases > 0 {
		passRate = float64(results.TotalPassed) / float64(results.TotalCases) * 100
	}

	fmt.Print("\n" + strings.Repeat("=", 80) + "\n")
	fmt.Printf("Benchmark Results - %s (v%s)\n", results.Timestamp.Format("2006-01-02 15:04:05"), results.Version)
	fmt.Print(strings.Repeat("=", 80) + "\n\n")

	fmt.Printf("Overall: %d/%d passed (%.1f%%)\n", results.TotalPassed, results.TotalCases, passRate)
	fmt.Printf("Execution Time: %.2fs\n\n", float64(results.ExecutionTimeMs)/1000.0)

	// Sort rules by ID
	var sortedRules []string
	for ruleID := range results.RuleMetrics {
		sortedRules = append(sortedRules, ruleID)
	}
	sort.Strings(sortedRules)

	// Print rule metrics table
	w := csv.NewWriter(os.Stdout)
	w.Comma = '|'
	w.Write([]string{"Rule ID", "Passed", "Total", "Detection Rate", "False Positives"})

	for _, ruleID := range sortedRules {
		rm := results.RuleMetrics[ruleID]
		record := []string{
			rm.RuleID,
			fmt.Sprintf("%d", rm.Passed),
			fmt.Sprintf("%d", rm.TotalCases),
			fmt.Sprintf("%.2f%%", rm.DetectionRate*100),
			fmt.Sprintf("%d", rm.FalsePositives),
		}
		w.Write(record)
	}

	w.Flush()
	fmt.Print("\n")
}

// printRegressions prints detected regressions.
func (r *Runner) printRegressions(alerts []RegressionAlert) {
	fmt.Print("\n" + strings.Repeat("-", 80) + "\n")
	fmt.Println("REGRESSION ALERTS")
	fmt.Print(strings.Repeat("-", 80) + "\n\n")

	for _, alert := range alerts {
		fmt.Printf("⚠️  %s: Detection rate dropped from %.2f%% to %.2f%% (%.2f%% drop, threshold: %.2f%%)\n",
			alert.RuleID,
			alert.BaselineDetectionRate*100,
			alert.CurrentDetectionRate*100,
			alert.DropPercent,
			alert.Threshold,
		)
	}

	fmt.Print("\n")
}

// getVersion returns the current version (stub - can be populated from git tags or env vars).
func getVersion() string {
	// TODO: Implement proper version detection
	return "v0.1.0-dev"
}

// htmlReportTemplate defines the HTML template for the benchmark report.
const htmlReportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>merm8 Benchmark Report</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: #f5f5f5;
            color: #333;
            line-height: 1.6;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 40px 20px;
            border-radius: 8px;
            margin-bottom: 30px;
            box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
        }
        header h1 { font-size: 2.5em; margin-bottom: 10px; }
        header p { font-size: 1.1em; opacity: 0.95; }
        .summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .summary-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
            border-left: 4px solid #667eea;
        }
        .summary-card h3 { color: #667eea; font-size: 0.9em; text-transform: uppercase; margin-bottom: 10px; }
        .summary-card .value { font-size: 2em; font-weight: bold; color: #333; }
        .summary-card.passed { border-left-color: #48bb78; }
        .summary-card.passed h3 { color: #48bb78; }
        .summary-card.failed { border-left-color: #f56565; }
        .summary-card.failed h3 { color: #f56565; }
        table {
            width: 100%;
            border-collapse: collapse;
            background: white;
            border-radius: 8px;
            overflow: hidden;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
            margin-bottom: 30px;
        }
        th {
            background: #f7fafc;
            padding: 15px;
            text-align: left;
            font-weight: 600;
            color: #4a5568;
            border-bottom: 2px solid #e2e8f0;
        }
        td {
            padding: 12px 15px;
            border-bottom: 1px solid #e2e8f0;
        }
        tr:hover { background: #f8f9fa; }
        .rate-high { color: #48bb78; font-weight: 600; }
        .rate-medium { color: #ed8936; font-weight: 600; }
        .rate-low { color: #f56565; font-weight: 600; }
        .badge {
            display: inline-block;
            padding: 3px 8px;
            border-radius: 3px;
            font-size: 0.85em;
            font-weight: 500;
            margin: 2px;
        }
        .badge.pass { background: #c6f6d5; color: #22543d; }
        .badge.fail { background: #fed7d7; color: #742a2a; }
        h2 {
            margin-top: 40px;
            margin-bottom: 20px;
            color: #2d3748;
            border-bottom: 2px solid #e2e8f0;
            padding-bottom: 10px;
        }
        .details-row { margin: 10px 0; }
        .details-row strong { color: #4a5568; }
        .failed-cases {
            background: white;
            border-radius: 8px;
            overflow: hidden;
            box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
        }
        .failed-case {
            padding: 15px;
            border-bottom: 1px solid #e2e8f0;
            background: #fff5f5;
        }
        .failed-case:last-child { border-bottom: none; }
        .failed-case h4 { color: #f56565; margin-bottom: 8px; }
        .failed-case pre {
            background: #f7fafc;
            padding: 10px;
            border-radius: 4px;
            overflow-x: auto;
            font-size: 0.85em;
            margin: 5px 0;
        }
        footer {
            text-align: center;
            margin-top: 40px;
            padding-top: 20px;
            border-top: 1px solid #e2e8f0;
            color: #718096;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>🎯 merm8 Benchmark Report</h1>
            <p>Mermaid Linting Rule Efficacy Analysis</p>
        </header>

        <div class="summary">
            <div class="summary-card passed">
                <h3>Passed</h3>
                <div class="value">{{.TotalPassed}}</div>
                <p>of {{.TotalCases}} cases</p>
            </div>
            <div class="summary-card failed">
                <h3>Failed</h3>
                <div class="value">{{.TotalFailed}}</div>
                <p>quality failures</p>
            </div>
            <div class="summary-card">
                <h3>Pass Rate</h3>
                <div class="value">{{.PassRate}}</div>
                <p>overall success</p>
            </div>
            <div class="summary-card">
                <h3>Execution Time</h3>
                <div class="value">{{printf "%.2f" .ExecutionSec}}s</div>
                <p>total runtime</p>
            </div>
        </div>

        <h2>Test Metadata</h2>
        <div class="details-row">
            <strong>Timestamp:</strong> {{.Timestamp}}
        </div>
        <div class="details-row">
            <strong>Version:</strong> {{.Version}}
        </div>

        <h2>Rule Metrics</h2>
        <table>
            <thead>
                <tr>
                    <th>Rule</th>
                    <th>Passed</th>
                    <th>Total</th>
                    <th>Detection Rate</th>
                    <th>Avg Parse Time</th>
                    <th>Avg Lint Time</th>
                </tr>
            </thead>
            <tbody>
                {{range .Rules}}
                <tr>
                    <td><strong>{{.RuleID}}</strong></td>
                    <td>{{.Passed}}</td>
                    <td>{{.TotalCases}}</td>
                    <td>
                        {{$pct := mul .DetectionRate 100}}
                        {{if ge .DetectionRate 0.9}}<span class="rate-high">{{printf "%.2f" $pct}}%</span>
                        {{else if ge .DetectionRate 0.7}}<span class="rate-medium">{{printf "%.2f" $pct}}%</span>
                        {{else}}<span class="rate-low">{{printf "%.2f" $pct}}%</span>{{end}}
                    </td>
                    <td>{{.AvgParseTimeMs}}ms</td>
                    <td>{{.AvgLintTimeMs}}ms</td>
                </tr>
                {{end}}
            </tbody>
        </table>

        {{if .FailedCases}}
        <h2>Failed Cases</h2>
        <div class="failed-cases">
            {{range .FailedCases}}
            <div class="failed-case">
                <h4>{{.CaseID}}</h4>
                <div><strong>Expected:</strong> <span class="badge fail">{{join .Expected ", "}}</span></div>
                <div><strong>Got:</strong> <span class="badge fail">{{join .Actual ", "}}</span></div>
                {{if .Issues}}
                <div><strong>Issues:</strong></div>
                <pre>{{join .Issues "\n"}}</pre>
                {{end}}
            </div>
            {{end}}
        </div>
        {{end}}

        <footer>
            <p>Generated by <strong>merm8 Benchmark Runner</strong></p>
            <p>For more information, see <a href="../../BENCHMARK.md">BENCHMARK.md</a></p>
        </footer>
    </div>
</body>
</html>
`
