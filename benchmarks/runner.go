// Package benchmarks provides benchmarking and testing infrastructure for merm8 linting rules.
package benchmarks

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// appVersion is set at build time via -ldflags
var appVersion = "dev"

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
	parser         *parser.Parser
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
	OutputFormats       string  // "json,html,csv" (comma-separated, default "json,html")
}

// Run executes all benchmark cases and generates reports.
func (r *Runner) Run(opts RunOptions) error {
	r.ruleFilter = opts.RuleFilter
	r.categoryFilter = opts.CategoryFilter
	r.verbose = opts.Verbose

	// Initialize parser once for all cases
	p, err := parser.New(r.parserScript)
	if err != nil {
		return fmt.Errorf("initialize parser: %w", err)
	}
	r.parser = p

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

	// Parse output formats (default to json,html)
	if opts.OutputFormats == "" {
		opts.OutputFormats = "json,html"
	}
	outputFormats := strings.Split(opts.OutputFormats, ",")
	for i, fmt := range outputFormats {
		outputFormats[i] = strings.TrimSpace(fmt)
	}

	// Generate reports
	if err := r.generateReports(results, outputFormats); err != nil {
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
	contentMap := make(map[string][]string) // Map of SHA256 hash -> list of case IDs

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

				entryCases, err := r.discoverCasesInDir(catDir, dt, cat, contentMap)
				if err != nil {
					return nil, err
				}
				cases = append(cases, entryCases...)
			}
		} else {
			// For other diagram types, scan directory directly
			entryCases, err := r.discoverCasesInDir(dtDir, dt, "", contentMap)
			if err != nil {
				return nil, err
			}
			cases = append(cases, entryCases...)
		}
	}

	// Warn about duplicates
	for hash, caseIDs := range contentMap {
		if len(caseIDs) > 1 {
			fmt.Fprintf(os.Stderr, "WARNING: cases have identical content (hash: %s): %v\n", hash[:12], strings.Join(caseIDs, ", "))
		}
	}

	return cases, nil
}

// discoverCasesInDir discovers cases in a specific directory.
func (r *Runner) discoverCasesInDir(dir string, diagramType, category string, contentMap map[string][]string) ([]*BenchmarkCase, error) {
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
			var content []byte
			filePath := filepath.Join(dir, entry.Name())
			contentData, err := os.ReadFile(filePath)
			if err == nil {
				content = contentData
				ruleID = extractRuleIDFromContent(string(content))
			}

			// Track content hash for deduplication
			if len(content) > 0 {
				hash := sha256.Sum256(content)
				hashStr := fmt.Sprintf("%x", hash)
				caseID := strings.TrimSuffix(entry.Name(), ".mmd")
				if category != "" {
					caseID = fmt.Sprintf("%s-%s-%s", diagramType, category[:3], caseID)
				}
				contentMap[hashStr] = append(contentMap[hashStr], caseID)
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
		caseResult, err := r.executeCase(bc, eng, r.parser)
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
					RuleID:            ruleID,
					TotalCases:        1,
					Passed:            0,
					TotalActualIssues: 0,
					FalsePositives:    0,
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

		// Count false positives and total actual issues per rule
		actualByRule := make(map[string]int)
		expectedByRule := make(map[string]int)
		for _, actual := range caseResult.Actual {
			parts := strings.Split(actual, ":")
			if len(parts) > 0 {
				actualByRule[parts[0]]++
			}
		}
		for _, expected := range caseResult.Expected {
			parts := strings.Split(expected, ":")
			if len(parts) > 0 {
				expectedByRule[parts[0]]++
			}
		}
		for rule, actualCount := range actualByRule {
			ruleResults[rule].TotalActualIssues += actualCount
			expectedCount := expectedByRule[rule]
			if actualCount > expectedCount {
				ruleResults[rule].FalsePositives += (actualCount - expectedCount)
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
		if rr.TotalActualIssues > 0 {
			rr.FalsePositiveRate = float64(rr.FalsePositives) / float64(rr.TotalActualIssues)
			rr.FalsePositiveRate = math.Round(rr.FalsePositiveRate*10000) / 10000 // 4 decimals
		}
		results.RuleMetrics[ruleID] = rr
	}

	results.FailedCases = failedCases

	return results, nil
}

// executeCase runs a single benchmark case.
func (r *Runner) executeCase(bc *BenchmarkCase, eng *engine.Engine, p *parser.Parser) (CaseResult, error) {
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
func (r *Runner) generateReports(results *BenchmarkResults, outputFormats []string) error {
	// Ensure reports directory exists
	if err := os.MkdirAll(r.reportsDir, 0755); err != nil {
		return err
	}

	var generatedFiles []string

	// Generate requested format types
	for _, format := range outputFormats {
		format = strings.TrimSpace(format)
		switch format {
		case "json":
			jsonPath := filepath.Join(r.reportsDir, "latest-results.json")
			jsonData, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
				return err
			}
			generatedFiles = append(generatedFiles, fmt.Sprintf("JSON: %s", jsonPath))

		case "html":
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
			generatedFiles = append(generatedFiles, fmt.Sprintf("HTML: %s", htmlPath))

		case "csv":
			csvPath := filepath.Join(r.reportsDir, "latest-results.csv")
			csvContent, err := r.generateCSVReport(results)
			if err != nil {
				return fmt.Errorf("generate CSV: %w", err)
			}
			if err := os.WriteFile(csvPath, []byte(csvContent), 0644); err != nil {
				return err
			}
			generatedFiles = append(generatedFiles, fmt.Sprintf("CSV: %s", csvPath))

		case "markdown":
			mdPath := filepath.Join(r.reportsDir, "latest-results.md")
			mdContent, err := r.generateMarkdownReport(results)
			if err != nil {
				return fmt.Errorf("generate Markdown: %w", err)
			}
			if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
				return err
			}
			generatedFiles = append(generatedFiles, fmt.Sprintf("Markdown: %s", mdPath))
		}
	}

	fmt.Printf("\nReports generated:\n")
	for _, f := range generatedFiles {
		fmt.Printf("  %s\n", f)
	}
	return nil
}

// calculateCoverageMetrics analyzes test coverage across rules and diagram types.
func (r *Runner) calculateCoverageMetrics(results *BenchmarkResults) CoverageMetrics {
	coverage := CoverageMetrics{
		LowCoverageRules:       []string{},
		NoViolationsCasesRules: []string{},
		UncoveredDiagramTypes:  []string{},
		FullySupported:         true,
	}

	// Check rule coverage
	for ruleID, metrics := range results.RuleMetrics {
		if metrics.TotalCases < 5 {
			coverage.LowCoverageRules = append(coverage.LowCoverageRules, fmt.Sprintf("%s (%d cases)", ruleID, metrics.TotalCases))
			coverage.FullySupported = false
		}
	}

	// Check for rules with no violations cases (would require analyzing failed cases by category)
	// For now, we track if there are failed cases from violations category
	violationsCasesSeen := make(map[string]bool)
	for _, failedCase := range results.FailedCases {
		if strings.Contains(failedCase.CaseID, "-vio-") {
			parts := strings.Split(failedCase.CaseID, "-")
			if len(parts) > 0 {
				for _, rule := range results.RuleMetrics {
					if strings.HasPrefix(failedCase.CaseID, parts[0]) {
						violationsCasesSeen[rule.RuleID] = true
					}
				}
			}
		}
	}

	// Check diagram type coverage (simplistic: based on file naming convention in failed cases)
	coveredDiagrams := make(map[string]bool)
	for failedCase := range results.FailedCases {
		parts := strings.Split(results.FailedCases[failedCase].CaseID, "-")
		if len(parts) > 0 {
			coveredDiagrams[parts[0]] = true
		}
	}

	// All rules tested have at least one case (from RuleMetrics), diagram types check is approximate
	// Since we only have flowchart cases currently
	expectedDiagrams := []string{"sequence", "class", "er", "state"}
	for _, dt := range expectedDiagrams {
		if !coveredDiagrams[dt] {
			coverage.UncoveredDiagramTypes = append(coverage.UncoveredDiagramTypes, dt)
			coverage.FullySupported = false
		}
	}

	return coverage
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
		"split": func(s, sep string) []string {
			return strings.Split(s, sep)
		},
		"sampleSize": func(passed, total int) string {
			return fmt.Sprintf("%d/%d", passed, total)
		},
		"lowConfidence": func(total int) bool {
			return total < 5
		},
		"ruleContext": func(ruleID string) string {
			// Provide brief rule-specific guidance for debugging
			contexts := map[string]string{
				"no-cycles":             "Verify no circular edges exist. Check for A→B→...→A patterns.",
				"max-fanout":            "Check node has ≤6 outgoing edges. Count direct connections.",
				"max-depth":             "Verify depth ≤4 from root to deepest leaf. Count node levels.",
				"no-disconnected-nodes": "All nodes except source must have incoming edges.",
				"no-duplicate-node-ids": "Node IDs must be unique. Check id duplicates.",
			}
			if ctx, ok := contexts[ruleID]; ok {
				return ctx
			}
			return "See rule definition for expected behavior."
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

	// Calculate coverage metrics
	coverage := r.calculateCoverageMetrics(results)

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
		"Coverage":     coverage,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("<h1>Error generating report: %v</h1>", err)
	}

	return buf.String()
}

// generateCSVReport creates a CSV representation of benchmark results.
func (r *Runner) generateCSVReport(results *BenchmarkResults) (string, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	// Write summary section
	w.Write([]string{"Benchmark Results Summary"})
	w.Write([]string{"Timestamp", results.Timestamp.Format("2006-01-02 15:04:05")})
	w.Write([]string{"Version", results.Version})
	w.Write([]string{"Total Cases", fmt.Sprintf("%d", results.TotalCases)})
	w.Write([]string{"Total Passed", fmt.Sprintf("%d", results.TotalPassed)})
	w.Write([]string{"Pass Rate", fmt.Sprintf("%.2f%%", float64(results.TotalPassed)/float64(results.TotalCases)*100)})
	w.Write([]string{"Execution Time (ms)", fmt.Sprintf("%d", results.ExecutionTimeMs)})
	w.Write([]string{})

	// Write rule metrics header
	w.Write([]string{"Rule ID", "Passed", "Total Cases", "Detection Rate", "False Positives", "FP Rate", "Avg Parse Time (ms)", "Avg Lint Time (ms)"})

	// Sort rules by ID for consistent output
	var sortedRules []string
	for ruleID := range results.RuleMetrics {
		sortedRules = append(sortedRules, ruleID)
	}
	sort.Strings(sortedRules)

	// Write rule metrics
	for _, ruleID := range sortedRules {
		rm := results.RuleMetrics[ruleID]
		w.Write([]string{
			rm.RuleID,
			fmt.Sprintf("%d", rm.Passed),
			fmt.Sprintf("%d", rm.TotalCases),
			fmt.Sprintf("%.4f", rm.DetectionRate),
			fmt.Sprintf("%d", rm.FalsePositives),
			fmt.Sprintf("%.4f", rm.FalsePositiveRate),
			fmt.Sprintf("%d", rm.AvgParseTimeMs),
			fmt.Sprintf("%d", rm.AvgLintTimeMs),
		})
	}

	w.Write([]string{})

	// Write failed cases if any
	if len(results.FailedCases) > 0 {
		w.Write([]string{"Case ID", "Expected", "Actual"})
		for _, fc := range results.FailedCases {
			w.Write([]string{
				fc.CaseID,
				strings.Join(fc.Expected, "; "),
				strings.Join(fc.Actual, "; "),
			})
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// generateMarkdownReport generates a markdown report suitable for CI/CD pipeline integration.
func (r *Runner) generateMarkdownReport(results *BenchmarkResults) (string, error) {
	var buf bytes.Buffer

	// Summary section
	buf.WriteString("# Merm8 Benchmark Report\n\n")
	buf.WriteString(fmt.Sprintf("**Timestamp**: %s  \n", results.Timestamp.Format("2006-01-02 15:04:05")))
	buf.WriteString(fmt.Sprintf("**Version**: %s  \n", results.Version))
	buf.WriteString(fmt.Sprintf("**Execution Time**: %.2fs  \n", float64(results.ExecutionTimeMs)/1000.0))
	buf.WriteString("\n")

	// Overall result
	passPercentage := float64(results.TotalPassed) / float64(results.TotalCases) * 100
	buf.WriteString(fmt.Sprintf("## Overall Result\n\n"))
	buf.WriteString(fmt.Sprintf("**%d/%d** cases passed (**%.1f%%**)\n\n", results.TotalPassed, results.TotalCases, passPercentage))

	// Rule metrics table
	buf.WriteString("## Rule Metrics\n\n")
	buf.WriteString("| Rule ID | Passed | Total | Detection Rate | False Positives | Avg Parse (ms) | Avg Lint (ms) |\n")
	buf.WriteString("|---------|--------|-------|-----------------|-----------------|----------------|---------------|\n")

	// Sort rules by ID
	var sortedRules []string
	for ruleID := range results.RuleMetrics {
		sortedRules = append(sortedRules, ruleID)
	}
	sort.Strings(sortedRules)

	// Write rule metrics
	for _, ruleID := range sortedRules {
		rm := results.RuleMetrics[ruleID]
		buf.WriteString(fmt.Sprintf("| `%s` | %d | %d | %.1f%% | %d | %d | %d |\n",
			rm.RuleID,
			rm.Passed,
			rm.TotalCases,
			rm.DetectionRate*100,
			rm.FalsePositives,
			rm.AvgParseTimeMs,
			rm.AvgLintTimeMs,
		))
	}
	buf.WriteString("\n")

	// Failed cases section
	if len(results.FailedCases) > 0 {
		buf.WriteString("## Failed Cases\n\n")
		for _, fc := range results.FailedCases {
			buf.WriteString(fmt.Sprintf("### %s\n\n", fc.CaseID))
			buf.WriteString(fmt.Sprintf("**Expected**: %s  \n", strings.Join(fc.Expected, ", ")))
			buf.WriteString(fmt.Sprintf("**Actual**: %s  \n", strings.Join(fc.Actual, ", ")))
			buf.WriteString("\n")
		}
	}

	return buf.String(), nil
}

// compareToBaseline compares current results against a baseline and returns regressions.
// It checks for both detection rate drops and performance (parse/lint time) increases.
// performanceThreshold is applied to timing regressions (10% by default).
func (r *Runner) compareToBaseline(results *BenchmarkResults, baselinePath string, detectionThreshold float64) ([]RegressionAlert, error) {
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}

	var baseline BenchmarkResults
	if err := json.Unmarshal(baselineData, &baseline); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}

	var alerts []RegressionAlert
	performanceThreshold := 10.0 // Fixed 10% threshold for performance regressions

	for ruleID, currentMetrics := range results.RuleMetrics {
		baselineMetrics, ok := baseline.RuleMetrics[ruleID]
		if !ok {
			continue // New rule
		}

		// Check for detection rate regression
		dropPercent := (baselineMetrics.DetectionRate - currentMetrics.DetectionRate) * 100
		if dropPercent > detectionThreshold {
			alerts = append(alerts, RegressionAlert{
				RuleID:                ruleID,
				Type:                  "detection_rate",
				BaselineDetectionRate: baselineMetrics.DetectionRate,
				CurrentDetectionRate:  currentMetrics.DetectionRate,
				DropPercent:           math.Round(dropPercent*100) / 100,
				Threshold:             detectionThreshold,
				IsFailing:             true,
			})
		}

		// Check for parse time regression
		if baselineMetrics.AvgParseTimeMs > 0 {
			parseTimeIncrease := float64(currentMetrics.AvgParseTimeMs-baselineMetrics.AvgParseTimeMs) / float64(baselineMetrics.AvgParseTimeMs) * 100
			if parseTimeIncrease > performanceThreshold {
				alerts = append(alerts, RegressionAlert{
					RuleID:                   ruleID,
					Type:                     "performance_parse",
					BaselineAvgParseTimeMs:   baselineMetrics.AvgParseTimeMs,
					CurrentAvgParseTimeMs:    currentMetrics.AvgParseTimeMs,
					ParseTimeIncreasePercent: math.Round(parseTimeIncrease*100) / 100,
					Threshold:                performanceThreshold,
					IsFailing:                true,
				})
			}
		}

		// Check for lint time regression
		if baselineMetrics.AvgLintTimeMs > 0 {
			lintTimeIncrease := float64(currentMetrics.AvgLintTimeMs-baselineMetrics.AvgLintTimeMs) / float64(baselineMetrics.AvgLintTimeMs) * 100
			if lintTimeIncrease > performanceThreshold {
				alerts = append(alerts, RegressionAlert{
					RuleID:                  ruleID,
					Type:                    "performance_lint",
					BaselineAvgLintTimeMs:   baselineMetrics.AvgLintTimeMs,
					CurrentAvgLintTimeMs:    currentMetrics.AvgLintTimeMs,
					LintTimeIncreasePercent: math.Round(lintTimeIncrease*100) / 100,
					Threshold:               performanceThreshold,
					IsFailing:               true,
				})
			}
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
		switch alert.Type {
		case "detection_rate":
			fmt.Printf("⚠️  %s (detection rate): %.2f%% → %.2f%% (%.2f%% drop, threshold: %.2f%%)\n",
				alert.RuleID,
				alert.BaselineDetectionRate*100,
				alert.CurrentDetectionRate*100,
				alert.DropPercent,
				alert.Threshold,
			)
		case "performance_parse":
			fmt.Printf("⚠️  %s (parse time): %dms → %dms (+%.2f%%, threshold: %.2f%%)\n",
				alert.RuleID,
				alert.BaselineAvgParseTimeMs,
				alert.CurrentAvgParseTimeMs,
				alert.ParseTimeIncreasePercent,
				alert.Threshold,
			)
		case "performance_lint":
			fmt.Printf("⚠️  %s (lint time): %dms → %dms (+%.2f%%, threshold: %.2f%%)\n",
				alert.RuleID,
				alert.BaselineAvgLintTimeMs,
				alert.CurrentAvgLintTimeMs,
				alert.LintTimeIncreasePercent,
				alert.Threshold,
			)
		}
	}

	fmt.Print("\n")
}

// getVersion returns the current version.
// Priority:
// 1. MERM8_VERSION environment variable (set by CI/build)
// 2. git describe --tags (local dev builds)
// 3. appVersion (set by ldflags at build time)
// 4. hardcoded fallback
func getVersion() string {
	// Check environment variable first (set by CI/build)
	if v := strings.TrimSpace(os.Getenv("MERM8_VERSION")); v != "" {
		return v
	}

	// Try git describe for local dev builds
	if cmd := exec.Command("git", "describe", "--tags", "--always"); cmd != nil {
		if out, err := cmd.Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return v
			}
		}
	}

	// Use appVersion (set by ldflags: -ldflags="-X github.com/CyanAutomation/merm8/benchmarks.appVersion=v0.1.0")
	if appVersion != "dev" {
		return appVersion
	}

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
            background: #fafafa;
            color: #0f172a;
            line-height: 1.6;
            font-size: 14px;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 24px; }
        header {
            background: #ffffff;
            color: #0f172a;
            padding: 24px;
            border-bottom: 1px solid #e2e8f0;
            margin-bottom: 32px;
        }
        header h1 { font-size: 1.75em; margin-bottom: 8px; font-weight: 600; }
        header p { font-size: 0.95em; color: #64748b; }
        .summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
            gap: 16px;
            margin-bottom: 32px;
        }
        .summary-card {
            background: #ffffff;
            padding: 16px;
            border: 1px solid #e2e8f0;
            border-left: 3px solid #64748b;
        }
        .summary-card h3 { color: #0f172a; font-size: 0.85em; margin-bottom: 8px; font-weight: 600; }
        .summary-card .value { font-size: 1.875em; font-weight: 600; color: #0f172a; }
        .summary-card p { font-size: 0.9em; color: #64748b; margin-top: 4px; }
        .summary-card.passed { border-left-color: #16a34a; }
        .summary-card.passed h3 { color: #16a34a; }
        .summary-card.failed { border-left-color: #dc2626; }
        .summary-card.failed h3 { color: #dc2626; }
        table {
            width: 100%;
            border-collapse: collapse;
            background: #ffffff;
            border: 1px solid #e2e8f0;
            margin-bottom: 32px;
        }
        th {
            background: #f8fafc;
            padding: 12px 16px;
            text-align: left;
            font-weight: 600;
            color: #0f172a;
            border-bottom: 1px solid #e2e8f0;
            font-size: 0.9em;
            cursor: pointer;
            user-select: none;
        }
        td {
            padding: 12px 16px;
            border-bottom: 1px solid #e2e8f0;
        }
        tr:hover { background: #f8fafc; }
        .rate-high { color: #16a34a; font-weight: 600; }
        .rate-medium { color: #ea580c; font-weight: 600; }
        .rate-low { color: #dc2626; font-weight: 600; }
        .badge {
            display: inline-block;
            padding: 2px 6px;
            border-radius: 3px;
            font-size: 0.8em;
            font-weight: 500;
            margin: 1px;
            border: 1px solid;
        }
        .badge.pass { background: #f0fdf4; color: #16a34a; border-color: #16a34a; }
        .badge.fail { background: #fef2f2; color: #dc2626; border-color: #dc2626; }
        h2 {
            margin-top: 32px;
            margin-bottom: 16px;
            color: #0f172a;
            font-size: 1.1em;
            font-weight: 600;
            border-bottom: 1px solid #e2e8f0;
            padding-bottom: 8px;
        }
        .details-row { margin: 8px 0; }
        .details-row strong { color: #0f172a; }
        .coverage-section {
            background: #ffffff;
            padding: 16px;
            border: 1px solid #e2e8f0;
            border-left: 3px solid #ea580c;
            margin-bottom: 20px;
        }
        .coverage-section p { margin: 6px 0; font-size: 0.95em; color: #0f172a; }
        .failed-cases {
            background: #ffffff;
            border: 1px solid #e2e8f0;
        }
        .failed-case {
            padding: 16px;
            border-bottom: 1px solid #e2e8f0;
            background: #ffffff;
            border-left: 3px solid #dc2626;
        }
        .failed-case:last-child { border-bottom: none; }
        .failed-case h4 { color: #dc2626; margin-bottom: 8px; font-weight: 600; }
        .failed-case pre {
            background: #f8fafc;
            padding: 10px;
            border: 1px solid #e2e8f0;
            overflow-x: auto;
            font-size: 0.85em;
            margin: 6px 0;
        }
        footer {
            text-align: center;
            margin-top: 40px;
            padding-top: 16px;
            border-top: 1px solid #e2e8f0;
            color: #64748b;
            font-size: 0.85em;
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>merm8 Benchmark Report</h1>
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

        {{if .Coverage}}
        <h2>Coverage Analysis</h2>
        <div class="coverage-section">
            {{if .Coverage.FullySupported}}
            <div style="color: #48bb78; font-weight: 600;">✅ Full Coverage</div>
            <p>All rules have adequate test cases (≥5 per rule) and diagram types are covered.</p>
            {{else}}
            <div style="color: #f56565; font-weight: 600;">⚠️ Limited Coverage</div>
            {{if .Coverage.LowCoverageRules}}
            <p><strong>Low-coverage rules (&lt;5 cases):</strong> {{join .Coverage.LowCoverageRules ", "}}</p>
            {{end}}
            {{if .Coverage.UncoveredDiagramTypes}}
            <p><strong>Uncovered diagram types:</strong> {{join .Coverage.UncoveredDiagramTypes ", "}}</p>
            {{end}}
            {{end}}
        </div>
        {{end}}

        <h2>Rule Metrics</h2>
        <div style="margin-bottom: 12px;">
            <input type="text" id="ruleFilter" placeholder="Filter by rule name..." style="padding: 8px 12px; border: 1px solid #e2e8f0; width: 100%; max-width: 320px; font-size: 14px; color: #0f172a;">
        </div>
        <table id="metricsTable">
            <thead>
                <tr>
                    <th data-sort="rule">Rule <span id="sortRule">↕</span></th>
                    <th data-sort="passed">Passed <span id="sortPassed">↕</span></th>
                    <th data-sort="total">Total <span id="sortTotal">↕</span></th>
                    <th data-sort="detection">Detection Rate <span id="sortDetection">↕</span></th>
                    <th data-sort="fp">False Positives <span id="sortFp">↕</span></th>
                    <th data-sort="parse">Avg Parse Time <span id="sortParse">↕</span></th>
                    <th data-sort="lint">Avg Lint Time <span id="sortLint">↕</span></th>
                </tr>
            </thead>
            <tbody id="metricsBody">
                {{range .Rules}}
                <tr data-rule="{{.RuleID}}" data-passed="{{.Passed}}" data-total="{{.TotalCases}}" data-detection="{{.DetectionRate}}" data-fp="{{.FalsePositiveRate}}" data-parse="{{.AvgParseTimeMs}}" data-lint="{{.AvgLintTimeMs}}">
                    <td><strong>{{.RuleID}}</strong>{{if lowConfidence .TotalCases}} <span style="color: #ed8936; font-weight: 600;">⚠️</span>{{end}}</td>
                    <td>{{.Passed}}</td>
                    <td>{{.TotalCases}}</td>
                    <td>
                        {{$pct := mul .DetectionRate 100}}
                        {{if ge .DetectionRate 0.9}}<span class="rate-high">{{printf "%.2f" $pct}}%</span>
                        {{else if ge .DetectionRate 0.7}}<span class="rate-medium">{{printf "%.2f" $pct}}%</span>
                        {{else}}<span class="rate-low">{{printf "%.2f" $pct}}%</span>{{end}}
                        <span style="font-size: 0.85em; color: #718096;">({{sampleSize .Passed .TotalCases}})</span>
                    </td>
                    <td>{{.FalsePositives}} ({{printf "%.2f" (mul .FalsePositiveRate 100)}}%)</td>
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
                {{if .Expected}}
                <div style="background: #f8fafc; border-left: 1px solid #e2e8f0; padding: 8px 12px; margin-top: 8px; font-size: 0.9em;">
                    <strong>Rule Context:</strong>
                    {{$firstRule := index (split (index .Expected 0) ":") 0}}
                    <div style="margin-top: 4px; color: #475569; font-size: 0.9em;">{{ruleContext $firstRule}}</div>
                </div>
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
    <script>
        // Filter functionality for rule metrics table
        document.getElementById('ruleFilter').addEventListener('input', function(e) {
            const filter = e.target.value.toLowerCase();
            const rows = document.querySelectorAll('#metricsBody tr');
            rows.forEach(row => {
                const ruleCell = row.getAttribute('data-rule');
                if (ruleCell.toLowerCase().includes(filter)) {
                    row.style.display = '';
                } else {
                    row.style.display = 'none';
                }
            });
        });

        // Sortable table functionality
        const sortableHeaders = document.querySelectorAll('th[data-sort]');
        let currentSort = { column: null, direction: 'asc' };
        
        sortableHeaders.forEach(header => {
            header.addEventListener('click', function() {
                const column = this.getAttribute('data-sort');
                const body = document.getElementById('metricsBody');
                const rows = Array.from(body.querySelectorAll('tr'));
                
                // Toggle sort direction
                const direction = currentSort.column === column && currentSort.direction === 'asc' ? 'desc' : 'asc';
                currentSort = { column, direction };
                
                // Sort rows
                rows.sort((a, b) => {
                    let aVal = a.getAttribute('data-' + column);
                    let bVal = b.getAttribute('data-' + column);
                    
                    // Try to parse as number
                    const aNum = parseFloat(aVal);
                    const bNum = parseFloat(bVal);
                    
                    if (!isNaN(aNum) && !isNaN(bNum)) {
                        return direction === 'asc' ? aNum - bNum : bNum - aNum;
                    }
                    
                    // String comparison
                    return direction === 'asc' ? aVal.localeCompare(bVal) : bVal.localeCompare(aVal);
                });
                
                // Re-render table
                rows.forEach(row => body.appendChild(row));
                
                // Update sort indicators
                sortableHeaders.forEach(h => {
                    const col = h.getAttribute('data-sort');
                    const indicator = h.querySelector('span');
                    if (col === column) {
                        indicator.textContent = direction === 'asc' ? ' ↑' : ' ↓';
                    } else {
                        indicator.textContent = ' ↕';
                    }
                });
            });
        });
    </script>
</body>
</html>
`
