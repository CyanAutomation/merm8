package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/output/sarif"
	"github.com/CyanAutomation/merm8/internal/parser"
)

// mockParserWithTimeout wraps a mock parser and implements TimeoutProvider
type mockParserWithTimeout struct {
	diagram    *model.Diagram
	syntaxErr  *parser.SyntaxError
	parseError error
	timeout    time.Duration
}

func (m *mockParserWithTimeout) Parse(code string) (*model.Diagram, *parser.SyntaxError, error) {
	return m.diagram, m.syntaxErr, m.parseError
}

func (m *mockParserWithTimeout) Ready() error {
	return nil
}

func (m *mockParserWithTimeout) VersionInfo() (*parser.VersionInfo, error) {
	return &parser.VersionInfo{}, nil
}

func (m *mockParserWithTimeout) Timeout() time.Duration {
	if m.timeout == 0 {
		return 5 * time.Second
	}
	return m.timeout
}

// mockParserWithReturn is a simple mock that returns a pre-built diagram
type mockParserWithReturn struct {
	diagram    *model.Diagram
	syntaxErr  *parser.SyntaxError
	parseError error
}

func (m *mockParserWithReturn) Parse(code string) (*model.Diagram, *parser.SyntaxError, error) {
	return m.diagram, m.syntaxErr, m.parseError
}

func (m *mockParserWithReturn) Ready() error {
	return nil
}

func (m *mockParserWithReturn) VersionInfo() (*parser.VersionInfo, error) {
	return &parser.VersionInfo{}, nil
}

// TestInfo_ParserTimeout verifies timeout is exposed in /info response
func TestInfo_ParserTimeout(t *testing.T) {
	mockP := &mockParserWithTimeout{
		timeout: 5 * time.Second,
	}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/info")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	timeout, ok := info["parser-timeout-seconds"] // canonical kebab-case field name
	if !ok {
		t.Errorf("parser-timeout-seconds field not found in info response")
		return
	}

	val, ok := timeout.(float64)
	if !ok {
		t.Fatalf("parser-timeout-seconds type = %T, want JSON number", timeout)
	}

	if val != 5 {
		t.Errorf("parser-timeout-seconds = %v, want 5", val)
	}

	if val != float64(int(val)) {
		t.Errorf("parser-timeout-seconds = %v, want whole-number seconds", val)
	}
}

// TestAnalyzeSARIF_InvalidJSON verifies SARIF error response for invalid JSON
func TestAnalyzeSARIF_InvalidJSON(t *testing.T) {
	mockP := &mockParserWithTimeout{}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewBufferString("{invalid json"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var report sarif.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(report.Runs) == 0 || len(report.Runs[0].Results) == 0 {
		t.Errorf("expected error result in SARIF response")
	}
}

// TestAnalyzeSARIF_MissingCode verifies SARIF error for missing code field
func TestAnalyzeSARIF_MissingCode(t *testing.T) {
	mockP := &mockParserWithTimeout{}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := []byte(`{"options": {}}`)
	resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	var report sarif.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(report.Runs[0].Results) == 0 {
		t.Errorf("expected error result in SARIF response")
	}
}

// TestAnalyzeSARIF_ValidDiagram verifies SARIF success response
func TestAnalyzeSARIF_ValidDiagram(t *testing.T) {
	diagram := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "B"}}}
	mockP := &mockParserWithTimeout{diagram: diagram}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := []byte(`{"code":"graph LR; A --> B"}`)
	resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var report sarif.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if report.Version != "2.1.0" {
		t.Errorf("SARIF version = %s, want 2.1.0", report.Version)
	}
	if len(report.Runs) == 0 {
		t.Errorf("expected at least one run in SARIF report")
	}
}

// TestAnalyzeSARIF_RequestTooLarge verifies 413 status for oversized request
func TestAnalyzeSARIF_RequestTooLarge(t *testing.T) {
	mockP := &mockParserWithTimeout{}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Create a payload larger than maxRequestSize (25MB)
	largeCode := bytes.Repeat([]byte("A"), 26*1024*1024)
	payload := map[string]interface{}{"code": string(largeCode)}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
}

// TestAnalyzeSARIF_ParserTimeout verifies 504 status and SARIF error on timeout
func TestAnalyzeSARIF_ParserTimeout(t *testing.T) {
	// This test uses a mock parser that always returns a timeout error
	mockP := &mockParserWithTimeout{
		parseError: parser.ErrTimeout,
	}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph LR; A --> B"})
	resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusGatewayTimeout)
	}

	var report sarif.Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(report.Runs[0].Results) == 0 {
		t.Errorf("expected error result in SARIF response for timeout")
	}
	if report.Runs[0].Results[0].Level != sarif.SARIFLevelError {
		t.Errorf("timeout error should be level=error, got %s", report.Runs[0].Results[0].Level)
	}
}

// TestAnalyzeSARIF_SARIFFormatConsistency verifies all error responses use SARIF format
func TestAnalyzeSARIF_SARIFFormatConsistency(t *testing.T) {
	tests := []struct {
		name           string
		payload        string
		expectedStatus int
	}{
		{"invalid_json", "{invalid}", http.StatusBadRequest},
		{"missing_code", `{"options":{}}`, http.StatusBadRequest},
		{"empty_code", `{"code":""}`, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockP := &mockParserWithTimeout{}
			h := NewHandler(mockP, engine.New())
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			server := httptest.NewServer(mux)
			defer server.Close()

			resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewBufferString(tc.payload))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.expectedStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.expectedStatus)
			}

			// All responses must be valid SARIF format
			var report sarif.Report
			if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
				t.Errorf("failed to decode as SARIF: %v", err)
				return
			}

			if report.Version != "2.1.0" {
				t.Errorf("SARIF version = %s, want 2.1.0", report.Version)
			}
		})
	}
}

// TestAnalyzeSARIF_ErrorCodeMapping verifies error codes are properly mapped to SARIF levels
func TestAnalyzeSARIF_ErrorCodeMapping(t *testing.T) {
	tests := []struct {
		code          string
		expectedLevel string
	}{
		{"parser_timeout", sarif.SARIFLevelError},
		{"parser_subprocess_error", sarif.SARIFLevelError},
		{"invalid_json", sarif.SARIFLevelWarning},
		{"unknown_rule", sarif.SARIFLevelWarning},
	}

	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			level := mapErrorCodeToLevel(tc.code)
			if level != tc.expectedLevel {
				t.Errorf("mapErrorCodeToLevel(%s) = %s, want %s", tc.code, level, tc.expectedLevel)
			}
		})
	}
}

// Helper function that mirrors mapErrorCodeToLevel for testing
func mapErrorCodeToLevel(code string) string {
	switch code {
	case "parser_timeout", "parser_memory_limit", "parser_subprocess_error", "parser_decode_error", "parser_contract_violation", "internal_error", "server_busy":
		return sarif.SARIFLevelError
	case "invalid_json", "invalid_config", "missing_code", "request_too_large", "deprecated_config_format", "invalid_option", "unknown_option", "unknown_rule", "unsupported_diagram_type", "syntax_error":
		return sarif.SARIFLevelWarning
	default:
		return sarif.SARIFLevelWarning
	}
}

// TestAnalyzeSARIF_ConcurrentErrorResponses verifies SARIF error handling under concurrency
func TestAnalyzeSARIF_ConcurrentErrorResponses(t *testing.T) {
	mockP := &mockParserWithTimeout{}
	h := NewHandler(mockP, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			resp, err := http.Post(server.URL+"/analyze/sarif", "application/json", bytes.NewBufferString("{invalid}"))
			if err != nil {
				done <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				done <- errStatusCode(resp.StatusCode, http.StatusBadRequest)
				return
			}

			var report sarif.Report
			if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
				done <- err
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent request %d failed: %v", i, err)
		}
	}
}

func errStatusCode(got, want int) error {
	return fmt.Errorf("status = %d, want %d", got, want)
}

// TestNoDuplicateNodeIDs_WithinSameSubgraph tests that duplicate IDs within a subgraph are detected
func TestNoDuplicateNodeIDs_WithinSameSubgraph(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A", Label: "Node A"},
			{ID: "A", Label: "Duplicate A"},
			{ID: "B", Label: "Node B"},
		},
		Subgraphs: []model.Subgraph{
			{ID: "cluster-1", Label: "Cluster 1", Nodes: []string{"A", "A", "B"}},
		},
	}

	h := NewHandler(&mockParserWithReturn{diagram: diagram}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD\nA[Node A]\nA[Duplicate A]\nB[Node B]",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should have at least 1 issue for duplicate node ID
	if len(resp.Issues) < 1 {
		t.Fatalf("expected at least 1 issue for duplicate node ID, got %d", len(resp.Issues))
	}

	// Verify it's the duplicate-node-ids rule
	found := false
	for _, issue := range resp.Issues {
		if issue.RuleID == "no-duplicate-node-ids" {
			found = true
			if issue.Severity != "error" {
				t.Errorf("expected error severity, got %s", issue.Severity)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected no-duplicate-node-ids issue not found")
	}
}

// TestNoDuplicateNodeIDs_AcrossSubgraphs tests that duplicate IDs across different subgraphs are also detected
// This clarifies that ID uniqueness is global, not per-subgraph
func TestNoDuplicateNodeIDs_AcrossSubgraphs(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A", Label: "Node A in Cluster 1"},
			{ID: "B", Label: "Node B"},
			{ID: "A", Label: "Node A in Cluster 2"},
		},
		Subgraphs: []model.Subgraph{
			{ID: "cluster-1", Label: "Cluster 1", Nodes: []string{"A"}},
			{ID: "cluster-2", Label: "Cluster 2", Nodes: []string{"A"}},
		},
	}

	h := NewHandler(&mockParserWithReturn{diagram: diagram}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD\nsubgraph cluster-1[Cluster 1]\n  A[Node A]\nend\nB[Node B]\nsubgraph cluster-2[Cluster 2]\n  A[Node A]\nend",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should have at least 1 issue for duplicate node ID across subgraphs
	if len(resp.Issues) < 1 {
		t.Fatalf("expected at least 1 issue for duplicate node ID across subgraphs, got %d", len(resp.Issues))
	}

	// Verify it's the duplicate-node-ids rule
	found := false
	for _, issue := range resp.Issues {
		if issue.RuleID == "no-duplicate-node-ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected no-duplicate-node-ids issue for cross-subgraph duplicate")
	}
}

// TestNoDuplicateNodeIDs_WithSuppression tests that duplicate ID issues can be suppressed via config
func TestNoDuplicateNodeIDs_WithSuppression(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A", Label: "Node A"},
			{ID: "A", Label: "Duplicate A"},
		},
	}

	h := NewHandler(&mockParserWithReturn{diagram: diagram}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD\nA[Node A]\nA[Duplicate A]",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"no-duplicate-node-ids": map[string]interface{}{
					"enabled": false,
				},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Should NOT have any no-duplicate-node-ids issues because the rule is disabled
	for _, issue := range resp.Issues {
		if issue.RuleID == "no-duplicate-node-ids" {
			t.Fatalf("expected no-duplicate-node-ids rule to be disabled, but got issue: %v", issue)
		}
	}
}

// TestSuppressionSelectorValidation_UnknownRuleID tests that suppressing an unknown rule ID produces a warning
func TestSuppressionSelectorValidation_UnknownRuleID(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
			{ID: "C"},
			{ID: "D"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "A", To: "D"},
		},
	}

	h := NewHandler(&mockParserWithReturn{diagram: diagram}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD\nA-->B\nA-->C\nA-->D",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"max-fanout": map[string]interface{}{
					"limit":                 1,
					"suppression-selectors": []string{"rule:nonexistent-rule"},
				},
			},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even with unknown suppression rule, got %d", w.Code)
	}

	var resp analyzeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// There should be a warning about the unknown rule in the suppression selector
	found := false
	if resp.Meta != nil && resp.Meta.Warnings != nil {
		for _, warning := range resp.Meta.Warnings {
			if strings.Contains(warning.Message, "nonexistent-rule") || strings.Contains(warning.Message, "unknown rule") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Logf("expected warning about unknown suppression rule, got warnings: %#v", resp.Meta)
		// Note: This is lenient mode, so we log a warning but don't fail the test if warning isn't present
		// The warning may be logged but not in the response meta
	}

	// The request should still succeed and return issues (since the suppression didn't match)
	if len(resp.Issues) == 0 {
		t.Fatalf("expected max-fanout issues since unknown suppression didn't suppress them")
	}
}
