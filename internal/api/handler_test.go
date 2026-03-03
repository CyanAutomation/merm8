package api_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// mockParser is a test double for ParserInterface.
type mockParser struct {
	diagram    *model.Diagram
	syntaxErr  *parser.SyntaxError
	parseError error
	parseFunc  func(string) (*model.Diagram, *parser.SyntaxError, error)
	readyError error
}

func (m *mockParser) Parse(code string) (*model.Diagram, *parser.SyntaxError, error) {
	if m.parseFunc != nil {
		return m.parseFunc(code)
	}
	return m.diagram, m.syntaxErr, m.parseError
}

func (m *mockParser) Ready() error {
	return m.readyError
}

// newTestMux creates a test HTTP server backed by a handler using a mock parser.
func newTestMux(mockParseFn func(string) (*model.Diagram, *parser.SyntaxError, error)) *http.ServeMux {
	mux := http.NewServeMux()
	mockP := &mockParser{
		parseFunc: mockParseFn,
	}
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	return mux
}

// newTestMuxWithRealParser creates a test mux that uses the real parser subprocess.
// Used for integration tests. Returns nil mux if parser script doesn't exist.
func newTestMuxWithRealParser(t *testing.T, scriptPath string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	p, err := parser.New(scriptPath)
	if err != nil {
		t.Skipf("skipping integration test; parser init failed: %v", err)
	}
	if err := p.Ready(); err != nil {
		t.Skipf("skipping integration test; parser not ready: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	h.RegisterRoutes(mux)
	return mux
}

func getParserScriptPath(t *testing.T) string {
	t.Helper()

	if script := os.Getenv("PARSER_SCRIPT"); script != "" {
		if _, err := os.Stat(script); err == nil {
			return script
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	for {
		candidate := filepath.Join(cwd, "parser-node", "parse.mjs")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	t.Skip("real parser script not found")
	return ""
}

func assertExactErrorResponse(t *testing.T, body []byte, wantCode, wantMessage string) {
	t.Helper()

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if len(resp) != 3 {
		t.Fatalf("expected exactly 3 top-level fields, got %d: %v", len(resp), resp)
	}
	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Fatalf("expected valid=false, got %#v", resp["valid"])
	}
	issues, ok := resp["issues"].([]interface{})
	if !ok {
		t.Fatalf("expected issues array, got %#v", resp["issues"])
	}
	if len(issues) != 0 {
		t.Fatalf("expected empty issues array, got %#v", issues)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %#v", resp["error"])
	}
	if code, ok := errObj["code"].(string); !ok || code != wantCode {
		t.Fatalf("expected error.code=%q, got %#v", wantCode, errObj["code"])
	}
	if msg, ok := errObj["message"].(string); !ok || msg != wantMessage {
		t.Fatalf("expected error.message=%q, got %#v", wantMessage, errObj["message"])
	}
	if _, hasPath := errObj["path"]; hasPath {
		t.Fatalf("did not expect error.path for generic errors, got %#v", errObj["path"])
	}
	if _, hasSupported := errObj["supported"]; hasSupported {
		t.Fatalf("did not expect error.supported for generic errors, got %#v", errObj["supported"])
	}
}

func assertValidationErrorResponse(t *testing.T, body []byte, wantCode, wantMessage, wantPath string, wantSupported []string) {
	t.Helper()

	var resp struct {
		Valid  bool          `json:"valid"`
		Issues []model.Issue `json:"issues"`
		Error  struct {
			Code      string   `json:"code"`
			Message   string   `json:"message"`
			Path      string   `json:"path"`
			Supported []string `json:"supported"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if resp.Valid {
		t.Fatal("expected valid=false")
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected empty issues array, got %#v", resp.Issues)
	}
	if resp.Error.Code != wantCode {
		t.Fatalf("expected error.code=%q, got %q", wantCode, resp.Error.Code)
	}
	if resp.Error.Message != wantMessage {
		t.Fatalf("expected error.message=%q, got %q", wantMessage, resp.Error.Message)
	}
	if resp.Error.Path != wantPath {
		t.Fatalf("expected error.path=%q, got %q", wantPath, resp.Error.Path)
	}
	if len(wantSupported) == 0 {
		if len(resp.Error.Supported) != 0 {
			t.Fatalf("expected empty supported list, got %#v", resp.Error.Supported)
		}
		return
	}
	if strings.Join(resp.Error.Supported, ",") != strings.Join(wantSupported, ",") {
		t.Fatalf("expected supported=%v, got %v", wantSupported, resp.Error.Supported)
	}
}

func TestAnalyze_MissingCode(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "missing_code", "field 'code' is required")
}

func TestAnalyze_InvalidJSON(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "invalid_json", "invalid JSON body")
}

func TestAnalyze_RequestBodyTooLarge(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return nil, nil, nil
	})

	largeCode := strings.Repeat("A", (1<<20)+1024)
	body, _ := json.Marshal(map[string]string{"code": largeCode})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized request body, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called for oversized request body")
	}

	assertExactErrorResponse(t, w.Body.Bytes(), "request_too_large", "request body exceeds 1 MiB limit")
}

func TestAnalyze_ParserFails_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, errors.New("mock parser error")
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser fails, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "internal_error", "internal server error")
}

func TestAnalyze_ParserTimeout_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: after 2s", parser.ErrTimeout)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser times out, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_timeout", "parser timed out while validating Mermaid code")
}

func TestAnalyze_ParserSubprocessError_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: exit status 1", parser.ErrSubprocess)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser subprocess fails, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_subprocess_error", "parser subprocess failed")
}

func TestAnalyze_ParserDecodeError_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: malformed json", parser.ErrDecode)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser decode fails, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_decode_error", "parser returned malformed output")
}

func TestAnalyze_ParserContractViolation_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: missing ast", parser.ErrContract)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser contract is violated, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_contract_violation", "parser response violated service contract")
}

func TestAnalyze_ParserReturnsNilDiagram_Returns500(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser returns nil diagram, got %d", w.Code)
	}


	var resp struct {
		Valid  bool                   `json:"valid"`
		Issues []interface{}          `json:"issues"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	
	// Validate complete response structure
	if resp.Valid {
		t.Fatalf("expected valid=false, got %v", resp.Valid)
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected empty issues array, got %#v", resp.Issues)
	}
	if resp.Error.Code != "internal_error" {
		t.Fatalf("expected error.code=internal_error, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "parser returned nil diagram" {
		t.Fatalf("expected exact error message %q, got %q", "parser returned nil diagram", resp.Error.Message)
	}

}

// TestAnalyze_NoPanicOnNilDiagram tests that nil diagram from parser doesn't cause panic.
func TestAnalyze_NoPanicOnNilDiagram(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Verify no panic on nil diagram
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Analyze panicked for nil diagram: %v", r)
		}
	}()
	mux.ServeHTTP(w, req)
}

// TestAnalyze_ValidDiagram_SuccessPath tests a valid diagram end-to-end.
func TestAnalyze_ValidDiagram_SuccessPath(t *testing.T) {
	validDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A", Label: "Start"},
			{ID: "B", Label: "Process"},
			{ID: "C", Label: "End"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"},
			{From: "B", To: "C", Type: "arrow"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return validDiagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B\n  B-->C"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
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

	// Verify response structure
	if valid, ok := resp["valid"].(bool); !ok || !valid {
		t.Error("expected valid=true")
	}
	if syntaxErr := resp["syntax_error"]; syntaxErr != nil {
		t.Error("expected syntax_error=null")
	}
	if diagramType, ok := resp["diagram_type"].(string); !ok || diagramType != "flowchart" {
		t.Errorf("expected diagram_type=flowchart, got %v", resp["diagram_type"])
	}
	if lintSupported, ok := resp["lint_supported"].(bool); !ok || !lintSupported {
		t.Errorf("expected lint_supported=true, got %v", resp["lint_supported"])
	}
	if issues, ok := resp["issues"].([]interface{}); !ok {
		t.Error("expected issues array")
	} else if len(issues) != 0 {
		t.Errorf("expected 0 issues for clean diagram, got %d", len(issues))
	}
	if metrics, ok := resp["metrics"].(map[string]interface{}); !ok {
		t.Error("expected metrics object")
	} else {
		if nodeCount, ok := metrics["node_count"].(float64); !ok || nodeCount != 3 {
			t.Errorf("expected node_count=3, got %v", metrics["node_count"])
		}
		if edgeCount, ok := metrics["edge_count"].(float64); !ok || edgeCount != 2 {
			t.Errorf("expected edge_count=2, got %v", metrics["edge_count"])
		}
		if disconnected, ok := metrics["disconnected_node_count"].(float64); !ok || disconnected != 0 {
			t.Errorf("expected disconnected_node_count=0, got %v", metrics["disconnected_node_count"])
		}
		if duplicate, ok := metrics["duplicate_node_count"].(float64); !ok || duplicate != 0 {
			t.Errorf("expected duplicate_node_count=0, got %v", metrics["duplicate_node_count"])
		}
		if maxFanin, ok := metrics["max_fanin"].(float64); !ok || maxFanin != 1 {
			t.Errorf("expected max_fanin=1, got %v", metrics["max_fanin"])
		}
		if maxFanout, ok := metrics["max_fanout"].(float64); !ok || maxFanout != 1 {
			t.Errorf("expected max_fanout=1, got %v", metrics["max_fanout"])
		}
		if diagramType, ok := metrics["diagram_type"].(string); !ok || diagramType != "flowchart" {
			t.Errorf("expected metrics.diagram_type=flowchart, got %v", metrics["diagram_type"])
		}
		if direction, ok := metrics["direction"].(string); !ok || direction != "TD" {
			t.Errorf("expected metrics.direction=TD, got %v", metrics["direction"])
		}
		issueCounts, ok := metrics["issue_counts"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected metrics.issue_counts object, got %T", metrics["issue_counts"])
		}
		if bySeverity, ok := issueCounts["by_severity"].(map[string]interface{}); !ok || len(bySeverity) != 0 {
			t.Errorf("expected empty issue_counts.by_severity, got %v", issueCounts["by_severity"])
		}
		if byRule, ok := issueCounts["by_rule"].(map[string]interface{}); !ok || len(byRule) != 0 {
			t.Errorf("expected empty issue_counts.by_rule, got %v", issueCounts["by_rule"])
		}
	}
}

// TestAnalyze_SyntaxError_Returns200 tests that syntax errors return 200 with error details.
func TestAnalyze_SyntaxError_Returns200(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    0,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "invalid code"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for syntax error, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Error("expected valid=false")
	}
	if lintSupported, ok := resp["lint_supported"].(bool); !ok || lintSupported {
		t.Errorf("expected lint_supported=false for syntax error, got %v", resp["lint_supported"])
	}
	if syntaxErrResp, ok := resp["syntax_error"].(map[string]interface{}); !ok {
		t.Error("expected syntax_error object")
	} else {
		if msg, ok := syntaxErrResp["message"].(string); !ok || msg != "No diagram type detected" {
			t.Errorf("expected error message, got %v", syntaxErrResp["message"])
		}
	}
}

func TestAnalyze_UnsupportedDiagramType_ReturnsFallbackIssue(t *testing.T) {
	diagram := &model.Diagram{Type: model.DiagramTypeSequence}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "sequenceDiagram\n  Alice->>Bob: Hi"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
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

	if lintSupported, ok := resp["lint_supported"].(bool); !ok || lintSupported {
		t.Fatalf("expected lint_supported=false, got %v", resp["lint_supported"])
	}
	if diagramType, ok := resp["diagram_type"].(string); !ok || diagramType != "sequence" {
		t.Fatalf("expected diagram_type=sequence, got %v", resp["diagram_type"])
	}
	issues, ok := resp["issues"].([]interface{})
	if !ok || len(issues) != 1 {
		t.Fatalf("expected one fallback issue, got %#v", resp["issues"])
	}
	issue, _ := issues[0].(map[string]interface{})
	if issue["rule_id"] != "unsupported-diagram-type" {
		t.Fatalf("expected unsupported-diagram-type issue, got %v", issue["rule_id"])
	}
}

// TestAnalyze_ConfigApplied_MaxFanout tests that custom rule config is applied.
func TestAnalyze_ConfigApplied_MaxFanout(t *testing.T) {
	// Diagram with node A having 3 outgoing edges (violates limit of 2)
	diagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A", Label: "A"},
			{ID: "B", Label: "B"},
			{ID: "C", Label: "C"},
			{ID: "D", Label: "D"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"},
			{From: "A", To: "C", Type: "arrow"},
			{From: "A", To: "D", Type: "arrow"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	// Config with max-fanout limit of 2
	config := map[string]interface{}{
		"rules": map[string]interface{}{
			"max-fanout": map[string]interface{}{
				"limit": 2,
			},
		},
	}
	body, _ := json.Marshal(map[string]interface{}{
		"code":   "graph TD\n  A-->B\n  A-->C\n  A-->D",
		"config": config,
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	// Should have at least one max-fanout issue
	issues, ok := resp["issues"].([]interface{})
	if !ok {
		t.Fatal("expected issues array")
	}
	if len(issues) == 0 {
		t.Fatal("expected at least one issue for fanout violation")
	}

	found := false
	for _, issue := range issues {
		if issueMap, ok := issue.(map[string]interface{}); ok {
			if ruleID, ok := issueMap["rule_id"].(string); ok && ruleID == "max-fanout" {
				if _, hasLine := issueMap["line"]; hasLine {
					t.Fatal("expected max-fanout issue line to be omitted when unknown")
				}
				if _, hasColumn := issueMap["column"]; hasColumn {
					t.Fatal("expected max-fanout issue column to be omitted when unknown")
				}
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected max-fanout issue not found")
	}
}

// TestAnalyze_ConfigParsing tests that both flat and nested config formats are accepted and applied.
func TestAnalyze_ConfigParsing(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
	}{
		{
			name:   "flat format",
			config: map[string]interface{}{"max-fanout": map[string]interface{}{"limit": 2}},
		},
		{
			name:   "nested format",
			config: map[string]interface{}{"rules": map[string]interface{}{"max-fanout": map[string]interface{}{"limit": 2}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Diagram with node A having 3 outgoing edges (violates custom limit of 2)
			diagram := &model.Diagram{
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

			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return diagram, nil, nil
			})

			body, _ := json.Marshal(map[string]interface{}{
				"code":   "graph TD; A-->B; A-->C; A-->D",
				"config": tt.config,
			})

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 with %s config, got %d", tt.name, w.Code)
			}

			// Verify config was actually applied: should have max-fanout issue
			var resp map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &resp)

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatalf("expected issues array in response, got %T", resp["issues"])
			}

			// Verify max-fanout issue is present (config must have been applied)
			found := false
			for _, issue := range issues {
				if issueMap, ok := issue.(map[string]interface{}); ok {
					if ruleID, ok := issueMap["rule_id"].(string); ok && ruleID == "max-fanout" {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("expected max-fanout issue not found; config may not have been applied to %s", tt.name)
			}
		})
	}
}

// TestAnalyze_MultipleRulesAggregate tests that multiple rule violations are aggregated.
func TestAnalyze_MultipleRulesAggregate(t *testing.T) {
	// Diagram with duplicate node ID "A" and disconnected node "C"
	diagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A", Label: "A"},
			{ID: "A", Label: "A2"}, // Duplicate
			{ID: "C", Label: "C"},  // Will be disconnected
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"}, // B doesn't exist
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B\n  A[A2]"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	issues, ok := resp["issues"].([]interface{})
	if !ok {
		t.Fatal("expected issues array")
	}

	if len(issues) < 1 {
		t.Errorf("expected at least 1 issue, got %d", len(issues))
	}

	ruleIDs := make(map[string]int)
	for _, issue := range issues {
		if issueMap, ok := issue.(map[string]interface{}); ok {
			if ruleID, ok := issueMap["rule_id"].(string); ok {
				ruleIDs[ruleID]++
			}
		}
	}
	t.Logf("rules fired: %v", ruleIDs)
}

func TestAnalyze_MetricsExtendedFields(t *testing.T) {
	diagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "LR",
		Nodes:     []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges:     []model.Edge{{From: "A", To: "B", Type: "arrow"}, {From: "C", To: "B", Type: "arrow"}},
	}
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})
	body, _ := json.Marshal(map[string]string{"code": "graph LR\n  A-->B\n  C-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
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
	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object, got %T", resp["metrics"])
	}
	expected := map[string]int{
		"node_count":              5,
		"edge_count":              2,
		"disconnected_node_count": 1,
		"duplicate_node_count":    1,
		"max_fanin":               2,
		"max_fanout":              1,
	}
	for k, want := range expected {
		got, ok := metrics[k].(float64)
		if !ok || int(got) != want {
			t.Fatalf("expected %s=%d, got %v", k, want, metrics[k])
		}
	}
	if got := metrics["diagram_type"]; got != "flowchart" {
		t.Fatalf("expected diagram_type=flowchart, got %v", got)
	}
	if got := metrics["direction"]; got != "LR" {
		t.Fatalf("expected direction=LR, got %v", got)
	}
	issueCounts, ok := metrics["issue_counts"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected issue_counts object, got %T", metrics["issue_counts"])
	}
	bySeverity := issueCounts["by_severity"].(map[string]interface{})
	if bySeverity["error"] != float64(2) {
		t.Fatalf("expected by_severity.error=2, got %v", bySeverity["error"])
	}
	byRule := issueCounts["by_rule"].(map[string]interface{})
	if byRule["no-duplicate-node-ids"] != float64(1) {
		t.Fatalf("expected no-duplicate-node-ids count=1, got %v", byRule["no-duplicate-node-ids"])
	}
	if byRule["no-disconnected-nodes"] != float64(1) {
		t.Fatalf("expected no-disconnected-nodes count=1, got %v", byRule["no-disconnected-nodes"])
	}
}

// TestAnalyze_LargeDiagram tests handling of large diagrams (500+ nodes) with timing and detailed metrics.
func TestAnalyze_LargeDiagram(t *testing.T) {
	// Create a large diagram with 500 nodes in a chain
	nodes := make([]model.Node, 500)
	edges := make([]model.Edge, 499)
	for i := 0; i < 500; i++ {
		nodes[i] = model.Node{ID: fmt.Sprintf("N%d", i), Label: fmt.Sprintf("Node%d", i)}
		if i > 0 {
			edges[i-1] = model.Edge{From: fmt.Sprintf("N%d", i-1), To: fmt.Sprintf("N%d", i), Type: "arrow"}
		}
	}

	diagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes:     nodes,
		Edges:     edges,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "large diagram"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Time the analysis
	start := time.Now()
	mux.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for large diagram, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatal("expected metrics object")
	}

	// Verify exact node count
	if nodeCount, ok := metrics["node_count"].(float64); ok {
		if int(nodeCount) != 500 {
			t.Errorf("expected 500 nodes, got %d", int(nodeCount))
		}
	} else {
		t.Error("expected node_count in metrics")
	}

	// Verify exact edge count (chain should have exactly 499 edges)
	if edgeCount, ok := metrics["edge_count"].(float64); ok {
		if int(edgeCount) != 499 {
			t.Errorf("expected 499 edges in linear chain, got %d", int(edgeCount))
		}
	} else {
		t.Error("expected edge_count in metrics")
	}

	// Log timing for performance tracking. Keep only a coarse upper bound to reduce CI flakiness.
	t.Logf("Large diagram analysis completed in %v (nodes: 500, edges: 499)", elapsed)
	if elapsed > 5*time.Second {
		t.Fatalf("large diagram analysis exceeded stable upper bound: %v", elapsed)
	}
}

func TestAnalyze_LargeTopologyMetricsAndFindings(t *testing.T) {
	type testCase struct {
		name            string
		diagram         *model.Diagram
		expectedMetrics map[string]int
		expectedRules   map[string]int
		maxDuration     time.Duration
	}

	buildChainDiagram := func(n int) *model.Diagram {
		nodes := make([]model.Node, n)
		edges := make([]model.Edge, 0, n-1)
		for i := 0; i < n; i++ {
			nodes[i] = model.Node{ID: fmt.Sprintf("N%d", i), Label: fmt.Sprintf("Node%d", i)}
			if i > 0 {
				edges = append(edges, model.Edge{From: fmt.Sprintf("N%d", i-1), To: fmt.Sprintf("N%d", i), Type: "arrow"})
			}
		}
		return &model.Diagram{Direction: "TD", Nodes: nodes, Edges: edges}
	}

	buildHighFanoutDiagram := func(fanout int) *model.Diagram {
		nodes := make([]model.Node, 0, fanout+1)
		nodes = append(nodes, model.Node{ID: "HUB", Label: "Hub"})
		edges := make([]model.Edge, 0, fanout)
		for i := 0; i < fanout; i++ {
			nodeID := fmt.Sprintf("L%d", i)
			nodes = append(nodes, model.Node{ID: nodeID, Label: nodeID})
			edges = append(edges, model.Edge{From: "HUB", To: nodeID, Type: "arrow"})
		}
		return &model.Diagram{Direction: "TD", Nodes: nodes, Edges: edges}
	}

	buildHighFaninDiagram := func(sources int) *model.Diagram {
		nodes := make([]model.Node, 0, sources+1)
		nodes = append(nodes, model.Node{ID: "TARGET", Label: "Target"})
		edges := make([]model.Edge, 0, sources)
		for i := 0; i < sources; i++ {
			nodeID := fmt.Sprintf("S%d", i)
			nodes = append(nodes, model.Node{ID: nodeID, Label: nodeID})
			edges = append(edges, model.Edge{From: nodeID, To: "TARGET", Type: "arrow"})
		}
		return &model.Diagram{Direction: "TD", Nodes: nodes, Edges: edges}
	}

	chainNodes := 10000
	isShort := testing.Short()
	if isShort {
		chainNodes = 2000
	}

	tests := []testCase{
		{
			name:    fmt.Sprintf("linear chain (%d nodes)", chainNodes),
			diagram: buildChainDiagram(chainNodes),
			expectedMetrics: map[string]int{
				"node_count": chainNodes,
				"edge_count": chainNodes - 1,
				"max_fanout": 1,
			},
			expectedRules: map[string]int{},
			maxDuration:   8 * time.Second,
		},
		{
			name:    "single hub high fan-out",
			diagram: buildHighFanoutDiagram(6000),
			expectedMetrics: map[string]int{
				"node_count": 6001,
				"edge_count": 6000,
				"max_fanout": 6000,
			},
			expectedRules: map[string]int{"max-fanout": 1},
			maxDuration:   8 * time.Second,
		},
		{
			name:    "high fan-in target node",
			diagram: buildHighFaninDiagram(7000),
			expectedMetrics: map[string]int{
				"node_count": 7001,
				"edge_count": 7000,
				"max_fanout": 1,
			},
			expectedRules: map[string]int{},
			maxDuration:   8 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isShort && tt.name == fmt.Sprintf("linear chain (%d nodes)", chainNodes) {
				t.Skip("skipping longest topology case in short mode")
			}

			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return tt.diagram, nil, nil
			})

			body, _ := json.Marshal(map[string]string{"code": "large topology"})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			start := time.Now()
			mux.ServeHTTP(w, req)
			elapsed := time.Since(start)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			metrics, ok := resp["metrics"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected metrics object, got %T; body=%s", resp["metrics"], w.Body.String())
			}

			for metricKey, want := range tt.expectedMetrics {
				got, ok := metrics[metricKey].(float64)
				if !ok {
					t.Fatalf("expected numeric metric %q, got %T", metricKey, metrics[metricKey])
				}
				if int(got) != want {
					t.Errorf("expected %s=%d, got %d", metricKey, want, int(got))
				}
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatalf("expected issues array, got %T", resp["issues"])
			}

			ruleCounts := make(map[string]int)
			for _, issue := range issues {
				issueMap, ok := issue.(map[string]interface{})
				if !ok {
					t.Fatalf("unexpected issue value type: %T", issue)
				}
				ruleID, _ := issueMap["rule_id"].(string)
				ruleCounts[ruleID]++
			}

			if len(ruleCounts) != len(tt.expectedRules) {
				t.Errorf("expected %d distinct rule findings, got %d (%v)", len(tt.expectedRules), len(ruleCounts), ruleCounts)
			}
			for ruleID, want := range tt.expectedRules {
				if got := ruleCounts[ruleID]; got != want {
					t.Errorf("expected rule %q to report %d findings, got %d", ruleID, want, got)
				}
			}

			if elapsed > tt.maxDuration {
				t.Fatalf("analysis exceeded stable upper bound (%v): %v", tt.maxDuration, elapsed)
			}
		})
	}
}

func TestAnalyze_Stress_ConcurrentMixedPayloads(t *testing.T) {
	t.Parallel()

	validDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
			{ID: "C"},
			{ID: "D"},
			{ID: "E"},
			{ID: "F"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"},
			{From: "B", To: "C", Type: "arrow"},
			{From: "C", To: "D", Type: "arrow"},
			{From: "D", To: "E", Type: "arrow"},
			{From: "E", To: "F", Type: "arrow"},
		},
	}

	highFanoutDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
			{ID: "C"},
			{ID: "D"},
			{ID: "E"},
			{ID: "F"},
			{ID: "G"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"},
			{From: "A", To: "C", Type: "arrow"},
			{From: "A", To: "D", Type: "arrow"},
			{From: "A", To: "E", Type: "arrow"},
			{From: "A", To: "F", Type: "arrow"},
			{From: "A", To: "G", Type: "arrow"},
		},
	}

	syntaxErr := &parser.SyntaxError{Message: "mock syntax error", Line: 1, Column: 1}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		switch {
		case strings.Contains(code, "SYNTAX_ERROR"):
			return nil, syntaxErr, nil
		case strings.Contains(code, "HIGH_FANOUT"):
			return highFanoutDiagram, nil, nil
		default:
			return validDiagram, nil, nil
		}
	})

	makeValidBody := func(i int) []byte {
		body, _ := json.Marshal(map[string]interface{}{
			"code": fmt.Sprintf("graph TD\nA-->B\nB-->C\n%% VALID_%d", i),
		})
		return body
	}
	makeSyntaxErrorBody := func(i int) []byte {
		body, _ := json.Marshal(map[string]interface{}{
			"code": fmt.Sprintf("SYNTAX_ERROR_%d", i),
		})
		return body
	}
	makeHighFanoutBody := func(i int) []byte {
		body, _ := json.Marshal(map[string]interface{}{
			"code": fmt.Sprintf("HIGH_FANOUT_%d", i),
			"config": map[string]interface{}{
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{"limit": 3},
				},
			},
		})
		return body
	}

	type scenario struct {
		name      string
		buildBody func(int) []byte
		assertFn  func(*httptest.ResponseRecorder) error
	}

	scenarios := []scenario{
		{
			name:      "valid",
			buildBody: makeValidBody,
			assertFn: func(w *httptest.ResponseRecorder) error {
				if w.Code != http.StatusOK {
					return fmt.Errorf("valid payload expected 200, got %d", w.Code)
				}
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					return fmt.Errorf("valid payload decode failed: %w", err)
				}
				if valid, ok := resp["valid"].(bool); !ok || !valid {
					return fmt.Errorf("valid payload expected valid=true, got %v", resp["valid"])
				}
				return nil
			},
		},
		{
			name:      "syntax-error",
			buildBody: makeSyntaxErrorBody,
			assertFn: func(w *httptest.ResponseRecorder) error {
				if w.Code != http.StatusOK {
					return fmt.Errorf("syntax-error payload expected 200, got %d", w.Code)
				}
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					return fmt.Errorf("syntax-error payload decode failed: %w", err)
				}
				if valid, ok := resp["valid"].(bool); !ok || valid {
					return fmt.Errorf("syntax-error payload expected valid=false, got %v", resp["valid"])
				}
				if syntaxErrorResp, ok := resp["syntax_error"].(map[string]interface{}); !ok || syntaxErrorResp["message"] == nil {
					return fmt.Errorf("syntax-error payload expected syntax_error object with message, got %v", resp["syntax_error"])
				}
				return nil
			},
		},
		{
			name:      "high-fanout",
			buildBody: makeHighFanoutBody,
			assertFn: func(w *httptest.ResponseRecorder) error {
				if w.Code != http.StatusOK {
					return fmt.Errorf("high-fanout payload expected 200, got %d", w.Code)
				}
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					return fmt.Errorf("high-fanout payload decode failed: %w", err)
				}
				issues, ok := resp["issues"].([]interface{})
				if !ok {
					return fmt.Errorf("high-fanout payload expected issues array, got %T", resp["issues"])
				}
				found := false
				for _, issue := range issues {
					issueMap, ok := issue.(map[string]interface{})
					if !ok {
						continue
					}
					if issueMap["rule_id"] == "max-fanout" {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("high-fanout payload expected max-fanout issue, got %v", issues)
				}
				return nil
			},
		},
	}

	const goroutinesPerScenario = 120
	errCh := make(chan error, len(scenarios)*goroutinesPerScenario)
	var wg sync.WaitGroup

	for _, sc := range scenarios {
		sc := sc
		for i := 0; i < goroutinesPerScenario; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						errCh <- fmt.Errorf("scenario %s[%d] panicked: %v", sc.name, i, r)
					}
				}()

				body := sc.buildBody(i)
				req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()

				mux.ServeHTTP(w, req)

				if err := sc.assertFn(w); err != nil {
					errCh <- fmt.Errorf("scenario %s[%d] response assertion failed: %v; status=%d body=%s", sc.name, i, err, w.Code, w.Body.String())
				}
			}()
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestHealthz_ReturnsOK(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %q", resp["status"])
	}
}

func TestReady_ReturnsReadyWhenDependencyHealthy(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ready" {
		t.Fatalf("expected status=ready, got %q", resp["status"])
	}
}

func TestReady_ReturnsUnavailableWhenDependencyUnhealthy(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{readyError: errors.New("parser script not found")}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "not_ready" {
		t.Fatalf("expected status=not_ready, got %q", resp["status"])
	}
	if resp["error"] == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestAnalyze_Integration_SingleRuleSuppression(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable max-fanout\nA-->B\nA-->C\nA-->D",
		"config": {"rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Valid  bool          `json:"valid"`
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid=true response, got body=%s", w.Body.String())
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected max-fanout issue to be suppressed, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_GlobalSuppression(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable all\nA-->B\nA-->C\nA-->D\nE",
		"config": {"rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected all issues to be suppressed, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_ParserTimeout_Returns500AndHandlerStaysResponsive(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "api-timeout-parser-")
	if err != nil {
		t.Fatalf("failed to create repo temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	scriptPath := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {
  process.stdout.write(JSON.stringify({ valid: true, ast: { type: "flowchart", direction: "TD", nodes: [], edges: [], subgraphs: [], suppressions: [] } }) + "\n");
}, 8000);
`
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write timeout parser script: %v", err)
	}

	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{"code":"graph TD\nA-->B"}`
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser times out, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode timeout response: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %#v", resp["error"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "timed out") {
		t.Fatalf("expected timeout wording in error message, got %q", msg)
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthW := httptest.NewRecorder()
	mux.ServeHTTP(healthW, healthReq)

	if healthW.Code != http.StatusOK {
		t.Fatalf("expected handler to remain responsive after timeout; /healthz got %d", healthW.Code)
	}
}

func TestAnalyze_MalformedConfigObjects_Returns400(t *testing.T) {
	tests := []struct {
		name        string
		config      interface{}
		wantMessage string
		wantPath    string
	}{
		{
			name:        "config must be object",
			config:      true,
			wantMessage: "config must be object",
			wantPath:    "config",
		},
		{
			name: "config rules must be object",
			config: map[string]interface{}{
				"rules": []interface{}{"max-fanout"},
			},
			wantMessage: "config.rules must be object",
			wantPath:    "config.rules",
		},
		{
			name: "flat rule config must be object",
			config: map[string]interface{}{
				"max-fanout": 1,
			},
			wantMessage: "config.max-fanout must be object",
			wantPath:    "config.max-fanout",
		},
		{
			name: "nested rule config must be object",
			config: map[string]interface{}{
				"rules": map[string]interface{}{
					"max-fanout": false,
				},
			},
			wantMessage: "config.rules.max-fanout must be object",
			wantPath:    "config.rules.max-fanout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parserCalled := false
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				parserCalled = true
				return &model.Diagram{}, nil, nil
			})

			body, _ := json.Marshal(map[string]interface{}{
				"code":   "graph TD; A-->B",
				"config": tt.config,
			})

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", w.Code)
			}
			if parserCalled {
				t.Fatal("expected parser not to be called when config validation fails")
			}

			assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", tt.wantMessage, tt.wantPath, nil)
		})
	}
}

func TestAnalyze_InvalidSeverityConfig_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"max-fanout": map[string]interface{}{
				"severity": "warnx",
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called when config validation fails")
	}

	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for severity", "config.max-fanout.severity", nil)
}

func TestAnalyze_InvalidUnknownRuleConfigNested_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"rules": map[string]interface{}{
				"no-cycles": map[string]interface{}{"enabled": false},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called when config validation fails")
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "unknown_rule", "unknown rule: no-cycles", "config.rules.no-cycles", []string{"max-fanout", "no-disconnected-nodes", "no-duplicate-node-ids"})
}

func TestAnalyze_InvalidUnknownRuleConfigFlat_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"no-cycles": map[string]interface{}{"enabled": false},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called when config validation fails")
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "unknown_rule", "unknown rule: no-cycles", "config.no-cycles", []string{"max-fanout", "no-disconnected-nodes", "no-duplicate-node-ids"})
}

func TestAnalyze_InvalidUnknownOptionConfig_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"rules": map[string]interface{}{
				"max-fanout": map[string]interface{}{"unknown": true},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called when config validation fails")
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "unknown_option", "unknown option: unknown", "config.rules.max-fanout.unknown", []string{"enabled", "limit", "severity", "suppression_selectors"})
}

func TestAnalyze_InvalidMaxFanoutLimitConfig_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"rules": map[string]interface{}{
				"max-fanout": map[string]interface{}{"limit": 0},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if parserCalled {
		t.Fatal("expected parser not to be called when config validation fails")
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for limit", "config.rules.max-fanout.limit", nil)
}

func TestAnalyze_DisableRuleViaConfig(t *testing.T) {
	diagram := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A; A",
		"config": map[string]interface{}{
			"rules": map[string]interface{}{
				"no-duplicate-node-ids": map[string]interface{}{"enabled": false},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected issues to be empty when rule disabled, got %#v", resp.Issues)
	}
}

func TestAnalyze_SeverityOverride(t *testing.T) {
	diagram := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A; A",
		"config": map[string]interface{}{
			"no-duplicate-node-ids": map[string]interface{}{"severity": "info"},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) != 1 {
		t.Fatalf("expected one issue, got %#v", resp.Issues)
	}
	if resp.Issues[0].Severity != "info" {
		t.Fatalf("expected severity override to info, got %q", resp.Issues[0].Severity)
	}
}

func TestAnalyze_Integration_NonMatchingSuppressionDoesNotHideIssue(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable no-duplicate-node-ids\nA-->B\nA-->C\nA-->D",
		"config": {"rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) == 0 {
		t.Fatalf("expected non-matching suppression to keep max-fanout issue")
	}
	if resp.Issues[0].RuleID != "max-fanout" {
		t.Fatalf("expected max-fanout issue, got %#v", resp.Issues)
	}
}

func TestAnalyze_ParserConcurrencyLimitReached_Returns503(t *testing.T) {
	start := make(chan struct{})
	release := make(chan struct{})

	mockP := &mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		start <- struct{}{}
		<-release
		return &model.Diagram{}, nil, nil
	}}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(1)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})

	firstReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstW := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(firstW, firstReq)
		close(done)
	}()

	<-start

	secondReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondW := httptest.NewRecorder()
	mux.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when parser concurrency is exhausted, got %d", secondW.Code)
	}
	assertExactErrorResponse(t, secondW.Body.Bytes(), "server_busy", "parser concurrency limit reached; try again")

	close(release)
	<-done
}

func TestAnalyzeBearerAuthMiddleware_RequiresTokenInProduction(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	secured := api.AnalyzeBearerAuthMiddleware("s3cr3t", mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	secured.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when token is missing, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "unauthorized", "missing or invalid bearer token")
}

func TestAnalyzeRateLimitMiddleware_Returns429(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	limited := api.AnalyzeRateLimitMiddleware(api.NewRateLimiter(1, time.Minute), mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})

	firstReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstReq.RemoteAddr = "127.0.0.1:1234"
	firstW := httptest.NewRecorder()
	limited.ServeHTTP(firstW, firstReq)

	if firstW.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", firstW.Code)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondReq.RemoteAddr = "127.0.0.1:5678"
	secondW := httptest.NewRecorder()
	limited.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when request rate is exceeded, got %d", secondW.Code)
	}
	assertExactErrorResponse(t, secondW.Body.Bytes(), "rate_limited", "rate limit exceeded")
}

func TestAnalyzeAuthMiddleware_PrecedesRateLimitQuotaConsumption(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})

	limiter := api.NewRateLimiter(1, time.Minute)
	secured := api.AnalyzeBearerAuthMiddleware("s3cr3t", api.AnalyzeRateLimitMiddleware(limiter, mux))

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})

	unauthorizedReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	unauthorizedReq.Header.Set("Content-Type", "application/json")
	unauthorizedReq.RemoteAddr = "127.0.0.1:1234"
	unauthorizedW := httptest.NewRecorder()
	secured.ServeHTTP(unauthorizedW, unauthorizedReq)

	if unauthorizedW.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized request to be rejected before rate limiting, got %d", unauthorizedW.Code)
	}

	authorizedReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	authorizedReq.Header.Set("Content-Type", "application/json")
	authorizedReq.Header.Set("Authorization", "Bearer s3cr3t")
	authorizedReq.RemoteAddr = "127.0.0.1:5678"
	authorizedW := httptest.NewRecorder()
	secured.ServeHTTP(authorizedW, authorizedReq)

	if authorizedW.Code != http.StatusOK {
		t.Fatalf("expected first authorized request to succeed, got %d", authorizedW.Code)
	}
}

func TestListRules_ResponseShape(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Rules []struct {
			ID                  string                 `json:"id"`
			Severity            string                 `json:"severity"`
			Description         string                 `json:"description"`
			DefaultConfig       map[string]interface{} `json:"default_config"`
			ConfigurableOptions []struct {
				Name        string `json:"name"`
				Type        string `json:"type"`
				Description string `json:"description"`
				Constraints string `json:"constraints"`
			} `json:"configurable_options"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /rules response: %v", err)
	}
	if len(resp.Rules) == 0 {
		t.Fatal("expected non-empty rules list")
	}
	for _, rule := range resp.Rules {
		if rule.ID == "" || rule.Severity == "" || rule.Description == "" {
			t.Fatalf("expected id/severity/description for each rule, got %#v", rule)
		}
		if rule.DefaultConfig == nil {
			t.Fatalf("expected default_config object for %s", rule.ID)
		}
		if rule.ConfigurableOptions == nil {
			t.Fatalf("expected configurable_options array for %s", rule.ID)
		}
	}
}

func TestListRules_ContainsAllBuiltins(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Rules []struct {
			ID string `json:"id"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /rules response: %v", err)
	}

	ids := map[string]struct{}{}
	for _, rule := range resp.Rules {
		ids[rule.ID] = struct{}{}
	}
	for _, builtin := range []string{"no-duplicate-node-ids", "no-disconnected-nodes", "max-fanout"} {
		if _, ok := ids[builtin]; !ok {
			t.Fatalf("expected builtin rule %q in /rules response", builtin)
		}
	}
}

func TestListRules_MetadataConsistencyWithRegistry(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Rules []struct {
			ID                  string                 `json:"id"`
			Severity            string                 `json:"severity"`
			DefaultConfig       map[string]interface{} `json:"default_config"`
			ConfigurableOptions []struct {
				Name string `json:"name"`
			} `json:"configurable_options"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /rules response: %v", err)
	}

	registry := rules.ConfigRegistry()
	if len(resp.Rules) != len(registry) {
		t.Fatalf("expected %d rules from /rules endpoint, got %d", len(registry), len(resp.Rules))
	}
	for _, rule := range resp.Rules {
		meta, ok := registry[rule.ID]
		if !ok {
			t.Fatalf("unexpected rule id %q in /rules response", rule.ID)
		}
		if rule.Severity != meta.Severity {
			t.Fatalf("expected severity %q for %s, got %q", meta.Severity, rule.ID, rule.Severity)
		}
		for key, value := range meta.DefaultConfig {
			got, ok := rule.DefaultConfig[key]
			if !ok {
				t.Fatalf("missing default_config.%s for %s", key, rule.ID)
			}
			if key == "limit" {
				want, ok := value.(int)
				if !ok {
					t.Fatalf("expected int limit default in registry for %s", rule.ID)
				}
				gotNum, ok := got.(float64)
				if !ok || int(gotNum) != want {
					t.Fatalf("expected limit default %d for %s, got %#v", want, rule.ID, got)
				}
			}
		}
		optionNames := map[string]struct{}{}
		for _, option := range rule.ConfigurableOptions {
			optionNames[option.Name] = struct{}{}
		}
		for _, required := range meta.AllowedOptionKeys {
			if _, ok := optionNames[required]; !ok {
				t.Fatalf("expected configurable option %q for %s", required, rule.ID)
			}
		}
	}
}
