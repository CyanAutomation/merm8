package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

// TestAnalyze_SyntaxError_ArrowOperatorHelp tests help-suggestion for arrow syntax errors.
func TestAnalyze_SyntaxError_ArrowOperatorHelp(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "Unexpected token '>'",
		Line:    2,
		Column:  20,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	// Code with arrow operator error
	code := "flowchart TD\n    Start([Start]) -> Process[Process]\n    Process --> End([End])"
	body, _ := json.Marshal(map[string]string{"code": code})
	req := httptest.NewRequest("POST", "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify valid=false
	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Errorf("expected valid=false, got %v", resp["valid"])
	}

	// Verify help-suggestion is present
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion object, got %#v", resp["help-suggestion"])
	}

	// Verify help-suggestion fields
	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(title, "Arrow") {
		t.Errorf("expected title about arrow syntax, got %q", title)
	}

	if explanation, ok := helpSugg["explanation"].(string); !ok || (!strings.Contains(explanation, "-->") && !strings.Contains(explanation, "arrow")) {
		t.Errorf("expected explanation about --> syntax, got %q", explanation)
	}

	if wrongExample, ok := helpSugg["wrong-example"].(string); !ok || !strings.Contains(wrongExample, "->") {
		t.Errorf("expected wrong example with ->, got %q", wrongExample)
	}

	if correctExample, ok := helpSugg["correct-example"].(string); !ok || !strings.Contains(correctExample, "-->") {
		t.Errorf("expected correct example with -->, got %q", correctExample)
	}

	if fixAction, ok := helpSugg["fix-action"].(string); !ok || !strings.Contains(fixAction, "line") {
		t.Errorf("expected fix-action with line number, got %q", fixAction)
	}
}

// TestAnalyzeRaw_SyntaxError_MissingDiagramTypeHelp tests help-suggestion for missing diagram type.
func TestAnalyzeRaw_SyntaxError_MissingDiagramTypeHelp(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    0,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	// Code without diagram type keyword
	code := "A --> B\nB --> C"
	req := httptest.NewRequest("POST", "/v1/analyze/raw", bytes.NewReader([]byte(code)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify help-suggestion for missing diagram type
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion object for missing diagram type, got %#v", resp["help-suggestion"])
	}

	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(title, "diagram") {
		t.Errorf("expected title about diagram type, got %q", title)
	}

	if wrongExample, ok := helpSugg["wrong-example"].(string); !ok || !strings.Contains(wrongExample, "-->") {
		t.Errorf("expected wrong example without diagram type, got %q", wrongExample)
	}

	if correctExample, ok := helpSugg["correct-example"].(string); !ok || !strings.Contains(correctExample, "flowchart") {
		t.Errorf("expected correct example with flowchart keyword, got %q", correctExample)
	}
}

// TestAnalyzeRaw_SyntaxError_GraphvizDetectionHelp tests help-suggestion for Graphviz syntax.
func TestAnalyzeRaw_SyntaxError_GraphvizDetectionHelp(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    0,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	// Graphviz syntax
	code := "digraph G {\n  A -> B -> C\n}"
	req := httptest.NewRequest("POST", "/v1/analyze/raw", bytes.NewReader([]byte(code)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify help-suggestion for Graphviz
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion for Graphviz, got %#v", resp["help-suggestion"])
	}

	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(title, "Graphviz") {
		t.Errorf("expected title about Graphviz, got %q", title)
	}

	if wrongExample, ok := helpSugg["wrong-example"].(string); !ok || !strings.Contains(wrongExample, "digraph") {
		t.Errorf("expected wrong example with digraph, got %q", wrongExample)
	}

	if correctExample, ok := helpSugg["correct-example"].(string); !ok || !strings.Contains(correctExample, "flowchart") {
		t.Errorf("expected correct example with flowchart, got %q", correctExample)
	}
}

// TestAnalyze_ConfigError_UnknownRuleHelp tests help-suggestion for unknown rule errors.
func TestAnalyze_ConfigError_UnknownRuleHelp(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	// Config with typo in rule name (missing "core/" prefix)
	config := map[string]interface{}{
		"schema-version": "v1",
		"rules": map[string]interface{}{
			"max-fanout": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(map[string]interface{}{
		"code":   "graph TD\n  A-->B",
		"config": config,
	})

	req := httptest.NewRequest("POST", "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown rule, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify help-suggestion for config error
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion for unknown rule, got %#v", resp["help-suggestion"])
	}

	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(title, "Unknown rule") {
		t.Errorf("expected title about unknown rule, got %q", title)
	}

	if wrongExample, ok := helpSugg["wrong-example"].(string); !ok || !strings.Contains(wrongExample, "max-fanout") {
		t.Errorf("expected wrong example with max-fanout (no prefix), got %q", wrongExample)
	}

	if correctExample, ok := helpSugg["correct-example"].(string); !ok || !strings.Contains(correctExample, "core/max-fanout") {
		t.Errorf("expected correct example with core/max-fanout, got %q", correctExample)
	}

	if fixAction, ok := helpSugg["fix-action"].(string); !ok || !strings.Contains(fixAction, "/v1/rules") {
		t.Errorf("expected fix-action mentioning /v1/rules endpoint, got %q", fixAction)
	}
}

// TestAnalyze_ConfigError_InvalidStructureHelp tests help-suggestion for invalid config structure.
func TestAnalyze_ConfigError_InvalidStructureHelp(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	// Config as string instead of object (common mistake)
	body, _ := json.Marshal(map[string]interface{}{
		"code":   "graph TD\n  A-->B",
		"config": "invalid",  // This should be an object
	})

	req := httptest.NewRequest("POST", "/v1/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid config structure, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify help-suggestion for config structure error
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion for invalid config structure, got %#v", resp["help-suggestion"])
	}

	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(strings.ToLower(title), "object") {
		t.Errorf("expected title about config being an object, got %q", title)
	}

	if wrongExample, ok := helpSugg["wrong-example"].(string); !ok || !strings.Contains(wrongExample, "invalid") {
		t.Errorf("expected wrong example with string config, got %q", wrongExample)
	}

	if correctExample, ok := helpSugg["correct-example"].(string); !ok || !strings.Contains(correctExample, "schema-version") {
		t.Errorf("expected correct example with proper config object, got %q", correctExample)
	}
}

// TestAnalyze_SyntaxError_TabDetectionHelp tests help-suggestion for tab indentation.
func TestAnalyze_SyntaxError_TabDetectionHelp(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "Unexpected token",
		Line:    2,
		Column:  5,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	// Code with tab indentation (common editor mistake)
	code := "flowchart TD\n\tA -->B\n\tB --> C"
	req := httptest.NewRequest("POST", "/v1/analyze/raw", bytes.NewReader([]byte(code)))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify help-suggestion for tab indentation
	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion for tab indentation, got %#v", resp["help-suggestion"])
	}

	if title, ok := helpSugg["title"].(string); !ok || !strings.Contains(strings.ToLower(title), "space") {
		t.Errorf("expected title about spaces vs tabs, got %q", title)
	}

	if fixAction, ok := helpSugg["fix-action"].(string); !ok || !strings.Contains(strings.ToLower(fixAction), "space") {
		t.Errorf("expected fix-action about replacing tabs with spaces, got %q", fixAction)
	}
}
