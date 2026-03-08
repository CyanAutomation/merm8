package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

func assertHintCodePresent(t *testing.T, resp map[string]interface{}, expected string) {
	t.Helper()
	hints, ok := resp["hints"].([]interface{})
	if !ok {
		t.Fatalf("expected hints array, got %#v", resp["hints"])
	}
	for _, rawHint := range hints {
		hint, ok := rawHint.(map[string]interface{})
		if !ok {
			continue
		}
		if code, _ := hint["code"].(string); code == expected {
			return
		}
	}
	t.Fatalf("expected hint code %q, got %#v", expected, hints)
}

func assertHintMessageContains(t *testing.T, resp map[string]interface{}, expectedCode, expectedMessageFragment string) {
	t.Helper()
	hints, ok := resp["hints"].([]interface{})
	if !ok {
		t.Fatalf("expected hints array, got %#v", resp["hints"])
	}

	for _, rawHint := range hints {
		hint, ok := rawHint.(map[string]interface{})
		if !ok {
			continue
		}
		code, _ := hint["code"].(string)
		if code != expectedCode {
			continue
		}
		message, _ := hint["message"].(string)
		if !strings.Contains(strings.ToLower(message), strings.ToLower(expectedMessageFragment)) {
			t.Fatalf("expected hint %q message to contain %q, got %q", expectedCode, expectedMessageFragment, message)
		}
		return
	}

	t.Fatalf("expected hint code %q, got %#v", expectedCode, hints)
}

func TestAnalyzeRaw_SyntaxError_HintMapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		syntaxErr               *parser.SyntaxError
		code                    string
		expectedHintCode        string
		expectedHintMessage     string
		expectHelpSuggestion    bool
		expectedHelpTitle       string
		expectedHelpExplanation string
	}{
		{
			name:                    "graphviz syntax maps to graphviz help",
			syntaxErr:               &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1},
			code:                    "digraph G {\n  A -> B\n}",
			expectedHintCode:        "graphviz_syntax_detected",
			expectedHintMessage:     "graphviz",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "Graphviz",
			expectedHelpExplanation: "Mermaid",
		},
		{
			name:                    "tab indentation maps to spacing help",
			syntaxErr:               &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 1},
			code:                    "flowchart TD\n\tA --> B",
			expectedHintCode:        "tab_indentation_detected",
			expectedHintMessage:     "spaces",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "spaces",
			expectedHelpExplanation: "tabs",
		},
		{
			name:                    "single arrow maps to flowchart arrow help",
			syntaxErr:               &parser.SyntaxError{Message: "Unexpected token '>'", Line: 2, Column: 17},
			code:                    "flowchart TD\n  A -> B",
			expectedHintCode:        "flowchart_arrow_operator_detected",
			expectedHintMessage:     "-->",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "Arrow",
			expectedHelpExplanation: "-->",
		},
		{
			name:                    "missing diagram type maps to keyword help",
			syntaxErr:               &parser.SyntaxError{Message: "No diagram type detected", Line: 0, Column: 0},
			code:                    "A --> B\nB --> C",
			expectedHintCode:        "missing_diagram_type_keyword",
			expectedHintMessage:     "type keyword",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "diagram type",
			expectedHelpExplanation: "first line",
		},
		{
			name:                    "unknown syntax falls back to generic hint",
			syntaxErr:               &parser.SyntaxError{Message: "Parse failure near token", Line: 3, Column: 8},
			code:                    "flowchart TD\n  A --> B\n  note right of A",
			expectedHintCode:        "generic_syntax_error",
			expectedHintMessage:     "unmatched brackets",
			expectHelpSuggestion:    false,
			expectedHelpTitle:       "syntax",
			expectedHelpExplanation: "diagram",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mux := newTestMux(func(string) (*model.Diagram, *parser.SyntaxError, error) {
				return nil, tt.syntaxErr, nil
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", bytes.NewReader([]byte(tt.code)))
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

			hints, ok := resp["hints"].([]interface{})
			if !ok || len(hints) == 0 {
				t.Fatalf("expected non-empty hints array, got %#v", resp["hints"])
			}

			assertHintCodePresent(t, resp, tt.expectedHintCode)
			assertHintMessageContains(t, resp, tt.expectedHintCode, tt.expectedHintMessage)

			helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
			if tt.expectHelpSuggestion {
				if !ok || helpSugg == nil {
					t.Fatalf("expected help-suggestion object, got %#v", resp["help-suggestion"])
				}

				title, _ := helpSugg["title"].(string)
				if !strings.Contains(strings.ToLower(title), strings.ToLower(tt.expectedHelpTitle)) {
					t.Fatalf("expected help title to contain %q, got %q", tt.expectedHelpTitle, title)
				}

				explanation, _ := helpSugg["explanation"].(string)
				if !strings.Contains(strings.ToLower(explanation), strings.ToLower(tt.expectedHelpExplanation)) {
					t.Fatalf("expected help explanation to contain %q, got %q", tt.expectedHelpExplanation, explanation)
				}
			} else if ok && helpSugg != nil {
				t.Fatalf("expected no help-suggestion object, got %#v", helpSugg)
			}
		})
	}
}

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

	assertHintCodePresent(t, resp, "flowchart_arrow_operator_detected")

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

	assertHintCodePresent(t, resp, "missing_diagram_type_keyword")

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

	assertHintCodePresent(t, resp, "graphviz_syntax_detected")

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

	// Config with unknown rule name
	config := map[string]interface{}{
		"schema-version": "v1",
		"rules": map[string]interface{}{
			"unknown-rule": map[string]interface{}{},
		},
	}
	body, _ := json.Marshal(map[string]interface{}{
		"code":   "graph TD\n  A-->B",
		"config": config,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
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
		"config": "invalid", // This should be an object
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

	assertHintCodePresent(t, resp, "tab_indentation_detected")

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
