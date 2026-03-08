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
			name:                    "prose preamble before diagram maps to dedicated hint and help",
			syntaxErr:               &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1},
			code:                    "This diagram shows approval flow.\nflowchart TD\nA-->B",
			expectedHintCode:        "non_mermaid_preamble_detected",
			expectedHintMessage:     "start line 1",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "preamble",
			expectedHelpExplanation: "line 1",
		},
		{
			name:                    "markdown bullet preamble before diagram maps to dedicated hint and help",
			syntaxErr:               &parser.SyntaxError{Message: "Unexpected token", Line: 1, Column: 1},
			code:                    "- Summary of the flow\n- Start at intake\nflowchart TD\nA-->B",
			expectedHintCode:        "non_mermaid_preamble_detected",
			expectedHintMessage:     "descriptive prose",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "preamble",
			expectedHelpExplanation: "diagram type keyword",
		},
		{
			name:                    "unsupported gantt type maps to dedicated hint and help",
			syntaxErr:               &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1},
			code:                    "gantt\n  title Release Plan",
			expectedHintCode:        "unsupported_diagram_type_gantt",
			expectedHintMessage:     "currently unavailable",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "gantt",
			expectedHelpExplanation: "currently does not support",
		},
		{
			name:                    "unsupported pie type maps to dedicated hint and help",
			syntaxErr:               &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1},
			code:                    "pie\n  title Revenue",
			expectedHintCode:        "unsupported_diagram_type_pie",
			expectedHintMessage:     "currently unavailable",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "pie",
			expectedHelpExplanation: "currently does not support",
		},
		{
			name:                    "unterminated edge label maps to dedicated hint",
			syntaxErr:               &parser.SyntaxError{Message: "Parse error on line 2", Line: 2, Column: 20},
			code:                    "flowchart TD\n  Decision -->|No Retry",
			expectedHintCode:        "unterminated_edge_label",
			expectedHintMessage:     "both pipes",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "syntax",
			expectedHelpExplanation: "diagram",
		},
		{
			name:                    "odd pipe count around edge operator maps to dedicated hint",
			syntaxErr:               &parser.SyntaxError{Message: "Parse error on line 2", Line: 2, Column: 24},
			code:                    "flowchart TD\n  A -->|No| B -->|Maybe C",
			expectedHintCode:        "unterminated_edge_label",
			expectedHintMessage:     "both pipes",
			expectHelpSuggestion:    true,
			expectedHelpTitle:       "syntax",
			expectedHelpExplanation: "diagram",
		},
		{
			name:                    "unknown syntax falls back to generic hint",
			syntaxErr:               &parser.SyntaxError{Message: "Parse failure near token", Line: 3, Column: 8},
			code:                    "flowchart TD\n  A --> B\n  note right of A",
			expectedHintCode:        "generic_syntax_error",
			expectedHintMessage:     "unmatched brackets",
			expectHelpSuggestion:    true,
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
			if strings.HasPrefix(tt.expectedHintCode, "unsupported_diagram_type_") {
				for _, rawHint := range hints {
					hint := rawHint.(map[string]interface{})
					if code, _ := hint["code"].(string); code == tt.expectedHintCode {
						appliesTo, ok := hint["applies-to"].(map[string]interface{})
						if !ok {
							t.Fatalf("expected applies-to on %s hint, got %#v", tt.expectedHintCode, hint["applies-to"])
						}
						if line, ok := appliesTo["line"].(float64); !ok || line != 1 {
							t.Fatalf("expected applies-to.line=1 on %s hint, got %#v", tt.expectedHintCode, appliesTo["line"])
						}
					}
				}
			}

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

func TestAnalyzeRaw_SyntaxError_AlwaysIncludesHelpSuggestion(t *testing.T) {
	t.Parallel()

	syntaxErr := &parser.SyntaxError{
		Message: "Unexpected token",
		Line:    2,
		Column:  3,
	}

	mux := newTestMux(func(string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", bytes.NewReader([]byte("flowchart TD\n  A -- B")))
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

	if _, ok := resp["syntax-error"].(map[string]interface{}); !ok {
		t.Fatalf("expected syntax-error object, got %#v", resp["syntax-error"])
	}

	helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
	if !ok || helpSugg == nil {
		t.Fatalf("expected help-suggestion object when syntax-error is present, got %#v", resp["help-suggestion"])
	}

	if docLink, _ := helpSugg["doc-link"].(string); docLink != "#common-mistakes" {
		t.Fatalf("expected fallback doc-link #common-mistakes, got %q", docLink)
	}
}

func TestAnalyzeRaw_SyntaxError_DiagramTypeHeaderTypo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		code                string
		expectedWrongHeader string
		expectedHeader      string
	}{
		{
			name:                "sequence shorthand normalized",
			code:                "sequence\n  Alice->>Bob: hello",
			expectedWrongHeader: "sequence",
			expectedHeader:      "sequenceDiagram",
		},
		{
			name:                "class shorthand normalized",
			code:                "class\n  class Animal",
			expectedWrongHeader: "class",
			expectedHeader:      "classDiagram",
		},
		{
			name:                "state diagram v2 punctuation normalized",
			code:                "stateDiagramv2\n  [*] --> Idle",
			expectedWrongHeader: "stateDiagramv2",
			expectedHeader:      "stateDiagram-v2",
		},
		{
			name:                "casing mismatch normalized",
			code:                "sequencediagram\n  Alice->>Bob: hello",
			expectedWrongHeader: "sequencediagram",
			expectedHeader:      "sequenceDiagram",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			syntaxErr := &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1}
			mux := newTestMux(func(string) (*model.Diagram, *parser.SyntaxError, error) {
				return nil, syntaxErr, nil
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

			assertHintCodePresent(t, resp, "diagram_type_header_typo")

			hints, ok := resp["hints"].([]interface{})
			if !ok {
				t.Fatalf("expected hints array, got %#v", resp["hints"])
			}

			foundHint := false
			for _, rawHint := range hints {
				hint := rawHint.(map[string]interface{})
				if code, _ := hint["code"].(string); code != "diagram_type_header_typo" {
					continue
				}
				foundHint = true
				message, _ := hint["message"].(string)
				if !strings.Contains(message, tt.expectedWrongHeader) || !strings.Contains(message, tt.expectedHeader) {
					t.Fatalf("expected hint message to include %q and %q, got %q", tt.expectedWrongHeader, tt.expectedHeader, message)
				}
				fixExample, _ := hint["fix-example"].(string)
				expectedFixExample := tt.expectedHeader + "\n  A --> B"
				if fixExample != expectedFixExample {
					t.Fatalf("expected deterministic hint fix-example %q, got %q", expectedFixExample, fixExample)
				}
			}
			if !foundHint {
				t.Fatal("expected to find diagram_type_header_typo hint")
			}

			helpSugg, ok := resp["help-suggestion"].(map[string]interface{})
			if !ok || helpSugg == nil {
				t.Fatalf("expected help-suggestion object, got %#v", resp["help-suggestion"])
			}

			wrongExample, _ := helpSugg["wrong-example"].(string)
			expectedWrongExample := tt.expectedWrongHeader + "\n  A --> B"
			if wrongExample != expectedWrongExample {
				t.Fatalf("expected wrong-example %q, got %q", expectedWrongExample, wrongExample)
			}

			correctExample, _ := helpSugg["correct-example"].(string)
			expectedCorrectExample := tt.expectedHeader + "\n  A --> B"
			if correctExample != expectedCorrectExample {
				t.Fatalf("expected deterministic correct-example %q, got %q", expectedCorrectExample, correctExample)
			}

			fixAction, _ := helpSugg["fix-action"].(string)
			if !strings.Contains(fixAction, "On line 1") || !strings.Contains(fixAction, tt.expectedHeader) {
				t.Fatalf("expected line-specific fix-action mentioning %q, got %q", tt.expectedHeader, fixAction)
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

// TestAnalyze_ConfigError_UnknownRuleHelp tests that unknown rules are gracefully skipped with warnings.
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

	// With graceful degradation, unknown rules return 200 with warnings
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for graceful degradation of unknown rule, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Verify warnings about unknown rule are present
	warnings, ok := resp["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings array for unknown rule, got %#v", resp)
	}

	// Check that at least one warning mentions the unknown rule
	found := false
	for _, w := range warnings {
		if wStr, ok := w.(string); ok && strings.Contains(wStr, "unknown-rule") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unknown-rule, got warnings: %v", warnings)
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
