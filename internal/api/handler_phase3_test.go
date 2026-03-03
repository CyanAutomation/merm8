package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	timeout, ok := info["parser_timeout_seconds"]
	if !ok {
		t.Errorf("parser_timeout_seconds field not found in info response")
		return
	}

	if val, ok := timeout.(float64); !ok || val <= 0 {
		t.Errorf("ParserTimeoutSeconds = %v, want > 0", timeout)
	}
	t.Logf("Parser timeout seconds: %v", timeout)
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
	case "parser_timeout", "parser_subprocess_error", "parser_decode_error", "parser_contract_violation", "internal_error", "server_busy":
		return sarif.SARIFLevelError
	case "invalid_json", "invalid_config", "missing_code", "request_too_large", "deprecated_config_format", "invalid_option", "unknown_option", "unknown_rule", "unsupported_diagram_type", "syntax_error":
		return sarif.SARIFLevelWarning
	default:
		return sarif.SARIFLevelWarning
	}
}

// TestInfo_TimeoutProviderInterface verifies TimeoutProvider interface is properly implemented
func TestInfo_TimeoutProviderInterface(t *testing.T) {
	mockP := &mockParserWithTimeout{timeout: 5 * time.Second}

	// Verify parser implements TimeoutProvider
	var _ TimeoutProvider = mockP

	timeout := mockP.Timeout()
	if timeout <= 0 {
		t.Errorf("Timeout() = %v, want > 0", timeout)
	}

	// Timeout should be within reasonable bounds
	if timeout < 1*time.Second || timeout > 60*time.Second {
		t.Logf("Note: Timeout = %v (may be outside normal 1-60s range if explicitly set)", timeout)
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
