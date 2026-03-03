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

func newTestMuxWithRealParserAndEngine(t *testing.T, scriptPath string, e *engine.Engine) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	p, err := parser.New(scriptPath)
	if err != nil {
		t.Skipf("skipping integration test; parser init failed: %v", err)
	}
	if err := p.Ready(); err != nil {
		t.Skipf("skipping integration test; parser not ready: %v", err)
	}
	h := api.NewHandler(p, e)
	h.RegisterRoutes(mux)
	return mux
}

type nextLineProbeRule struct{}

func (nextLineProbeRule) ID() string { return "next-line-probe" }

func (nextLineProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	directiveLine := 2
	targetLine := 3
	return []model.Issue{
		{RuleID: "next-line-probe", Severity: "warning", Message: "directive-line issue", Line: &directiveLine},
		{RuleID: "next-line-probe", Severity: "warning", Message: "target-line issue", Line: &targetLine},
	}
}

type otherProbeRule struct{}

func (otherProbeRule) ID() string { return "other-probe" }

func (otherProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line := 3
	return []model.Issue{{RuleID: "other-probe", Severity: "warning", Message: "other rule issue", Line: &line}}
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
	if len(resp) != 5 {
		t.Fatalf("expected exactly 5 top-level fields, got %d: %v", len(resp), resp)
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

	if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
		t.Fatalf("expected lint-supported=false, got %#v", resp["lint-supported"])
	}
	if syntaxErr, exists := resp["syntax-error"]; !exists || syntaxErr != nil {
		t.Fatalf("expected syntax-error=null, got %#v", resp["syntax-error"])
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
		Valid         bool          `json:"valid"`
		LintSupported bool          `json:"lint-supported"`
		SyntaxError   interface{}   `json:"syntax-error"`
		Issues        []model.Issue `json:"issues"`
		Error         struct {
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
	if resp.LintSupported {
		t.Fatal("expected lint-supported=false")
	}
	if resp.SyntaxError != nil {
		t.Fatalf("expected syntax-error=null, got %#v", resp.SyntaxError)
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

func TestAnalyze_ParserTimeout_Returns504(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: after 2s", parser.ErrTimeout)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 when parser times out, got %d", w.Code)
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
		Valid         bool          `json:"valid"`
		LintSupported bool          `json:"lint-supported"`
		SyntaxError   interface{}   `json:"syntax-error"`
		Issues        []interface{} `json:"issues"`
		Error         struct {
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
	if resp.LintSupported {
		t.Fatal("expected lint-supported=false")
	}
	if resp.SyntaxError != nil {
		t.Fatalf("expected syntax-error=null, got %#v", resp.SyntaxError)
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
	if syntaxErr := resp["syntax-error"]; syntaxErr != nil {
		t.Error("expected syntax-error=null")
	}
	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "flowchart" {
		t.Errorf("expected diagram-type=flowchart, got %v", resp["diagram-type"])
	}
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || !lintSupported {
		t.Errorf("expected lint-supported=true, got %v", resp["lint-supported"])
	}
	if issues, ok := resp["issues"].([]interface{}); !ok {
		t.Error("expected issues array")
	} else if len(issues) != 0 {
		t.Errorf("expected 0 issues for clean diagram, got %d", len(issues))
	}
	if metrics, ok := resp["metrics"].(map[string]interface{}); !ok {
		t.Error("expected metrics object")
	} else {
		if nodeCount, ok := metrics["node-count"].(float64); !ok || nodeCount != 3 {
			t.Errorf("expected node-count=3, got %v", metrics["node-count"])
		}
		if edgeCount, ok := metrics["edge-count"].(float64); !ok || edgeCount != 2 {
			t.Errorf("expected edge-count=2, got %v", metrics["edge-count"])
		}
		if disconnected, ok := metrics["disconnected-node-count"].(float64); !ok || disconnected != 0 {
			t.Errorf("expected disconnected-node-count=0, got %v", metrics["disconnected-node-count"])
		}
		if duplicate, ok := metrics["duplicate-node-count"].(float64); !ok || duplicate != 0 {
			t.Errorf("expected duplicate-node-count=0, got %v", metrics["duplicate-node-count"])
		}
		if maxFanin, ok := metrics["max-fanin"].(float64); !ok || maxFanin != 1 {
			t.Errorf("expected max-fanin=1, got %v", metrics["max-fanin"])
		}
		if maxFanout, ok := metrics["max-fanout"].(float64); !ok || maxFanout != 1 {
			t.Errorf("expected max-fanout=1, got %v", metrics["max-fanout"])
		}
		if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "flowchart" {
			t.Errorf("expected metrics.diagram-type=flowchart, got %v", metrics["diagram-type"])
		}
		if direction, ok := metrics["direction"].(string); !ok || direction != "TD" {
			t.Errorf("expected metrics.direction=TD, got %v", metrics["direction"])
		}
		issueCounts, ok := metrics["issue-counts"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected metrics.issue-counts object, got %T", metrics["issue-counts"])
		}
		if bySeverity, ok := issueCounts["by-severity"].(map[string]interface{}); !ok || len(bySeverity) != 0 {
			t.Errorf("expected empty issue-counts.by-severity, got %v", issueCounts["by-severity"])
		}
		if byRule, ok := issueCounts["by-rule"].(map[string]interface{}); !ok || len(byRule) != 0 {
			t.Errorf("expected empty issue-counts.by-rule, got %v", issueCounts["by-rule"])
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
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
		t.Errorf("expected lint-supported=false for syntax error, got %v", resp["lint-supported"])
	}
	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "unknown" {
		t.Errorf("expected diagram-type=unknown for syntax error without parser hint, got %v", resp["diagram-type"])
	}
	if syntaxErrResp, ok := resp["syntax-error"].(map[string]interface{}); !ok {
		t.Error("expected syntax-error object")
	} else {
		if msg, ok := syntaxErrResp["message"].(string); !ok || msg != "No diagram type detected" {
			t.Errorf("expected error message, got %v", syntaxErrResp["message"])
		}
	}
	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object for syntax error, got %T", resp["metrics"])
	}
	for _, key := range []string{"node-count", "edge-count", "disconnected-node-count", "duplicate-node-count", "max-fanin", "max-fanout"} {
		if got, ok := metrics[key].(float64); !ok || got != 0 {
			t.Fatalf("expected metrics.%s=0 for syntax error, got %v", key, metrics[key])
		}
	}
	if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "unknown" {
		t.Fatalf("expected metrics.diagram-type=unknown, got %v", metrics["diagram-type"])
	}
}

func TestAnalyze_SyntaxError_UsesDetectedDiagramTypeForDefaults(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 10}
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for syntax error, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode syntax error response: %v", err)
	}

	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "flowchart" {
		t.Fatalf("expected syntax error diagram-type=flowchart fallback from input, got %v", resp["diagram-type"])
	}
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || !lintSupported {
		t.Fatalf("expected lint-supported=true for flowchart syntax error fallback, got %v", resp["lint-supported"])
	}
	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object for syntax error, got %T", resp["metrics"])
	}
	if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "flowchart" {
		t.Fatalf("expected metrics.diagram-type=flowchart fallback, got %v", metrics["diagram-type"])
	}
}

func TestAnalyze_UnsupportedDiagramType_ReturnsStructuredError(t *testing.T) {
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

	if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
		t.Fatalf("expected lint-supported=false, got %v", resp["lint-supported"])
	}
	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "sequence" {
		t.Fatalf("expected diagram-type=sequence, got %v", resp["diagram-type"])
	}
	issues, ok := resp["issues"].([]interface{})
	if !ok || len(issues) != 0 {
		t.Fatalf("expected no lint issues, got %#v", resp["issues"])
	}
	errorObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected structured error object, got %#v", resp["error"])
	}
	if code, ok := errorObj["code"].(string); !ok || code != "unsupported_diagram_type" {
		t.Fatalf("expected error.code=unsupported_diagram_type, got %v", errorObj["code"])
	}
	if _, hasPath := errorObj["path"]; hasPath {
		t.Fatalf("did not expect error.path, got %#v", errorObj["path"])
	}

	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object, got %#v", resp["metrics"])
	}
	if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "sequence" {
		t.Fatalf("expected metrics.diagram-type=sequence, got %v", metrics["diagram-type"])
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
		"schema-version": "v1",
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
			if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "max-fanout" {
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

// TestAnalyze_ConfigParsing tests canonical versioned config handling.
func TestAnalyze_ConfigParsing(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
	}{
		{
			name: "canonical versioned format",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules":          map[string]interface{}{"max-fanout": map[string]interface{}{"limit": 2}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Diagram with node A having 3 outgoing edges (violates custom limit of 2)
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
					if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "max-fanout" {
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

func TestAnalyze_ConfigLegacySnakeCaseKeysRejected(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}, nil, nil
	})

	bodyJSON, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A --> B",
		"config": map[string]any{
			"schema_version": "v1",
			"rules": map[string]any{
				"max-fanout": map[string]any{
					"suppression_selectors": []string{"node:A"},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for legacy snake_case config keys in strict mode, got %d body=%s", w.Code, w.Body.String())
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "config.schema-version is required", "config.schema-version", nil)
}

func TestAnalyze_ConfigSchemaVersion_Validation(t *testing.T) {
	t.Run("accepts versioned config", func(t *testing.T) {
		mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
			return &model.Diagram{
				Type:  model.DiagramTypeFlowchart,
				Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
				Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
			}, nil, nil
		})

		body, _ := json.Marshal(map[string]any{
			"code": "graph TD; A-->B; A-->C; A-->D",
			"config": map[string]any{
				"schema-version": "v1",
				"rules": map[string]any{
					"max-fanout": map[string]any{"limit": 2},
				},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for versioned config, got %d", w.Code)
		}
	})

	t.Run("rejects unknown schema version", func(t *testing.T) {
		parserCalled := false
		mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
			parserCalled = true
			return &model.Diagram{}, nil, nil
		})

		body, _ := json.Marshal(map[string]any{
			"code": "graph TD; A-->B",
			"config": map[string]any{
				"schema-version": "v9",
				"rules":          map[string]any{},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unsupported schema version, got %d", w.Code)
		}
		if parserCalled {
			t.Fatal("expected parser not to be called when schema version is invalid")
		}
		assertValidationErrorResponse(t, w.Body.Bytes(), "unsupported_schema_version", "unsupported config schema-version: v9", "config.schema-version", []string{"v1"})
	})

	t.Run("legacy formats are rejected in strict mode", func(t *testing.T) {
		for _, config := range []map[string]any{
			{"max-fanout": map[string]any{"limit": 2}},
			{"rules": map[string]any{"max-fanout": map[string]any{"limit": 2}}},
		} {
			parserCalled := false
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				parserCalled = true
				return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
			})
			body, _ := json.Marshal(map[string]any{"code": "graph TD; A-->B", "config": config})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for legacy config in strict mode, got %d", w.Code)
			}
			if parserCalled {
				t.Fatal("expected parser not to be called when strict config validation fails")
			}
			assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "config.schema-version is required", "config.schema-version", nil)
		}
	})
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
			if ruleID, ok := issueMap["rule-id"].(string); ok {
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
		"node-count":              5,
		"edge-count":              2,
		"disconnected-node-count": 1,
		"duplicate-node-count":    1,
		"max-fanin":               2,
		"max-fanout":              1,
	}
	for k, want := range expected {
		got, ok := metrics[k].(float64)
		if !ok || int(got) != want {
			t.Fatalf("expected %s=%d, got %v", k, want, metrics[k])
		}
	}
	if got := metrics["diagram-type"]; got != "flowchart" {
		t.Fatalf("expected diagram-type=flowchart, got %v", got)
	}
	if got := metrics["direction"]; got != "LR" {
		t.Fatalf("expected direction=LR, got %v", got)
	}
	issueCounts, ok := metrics["issue-counts"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected issue-counts object, got %T", metrics["issue-counts"])
	}
	bySeverity := issueCounts["by-severity"].(map[string]interface{})
	if bySeverity["error"] != float64(2) {
		t.Fatalf("expected by-severity.error=2, got %v", bySeverity["error"])
	}
	byRule := issueCounts["by-rule"].(map[string]interface{})
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
	if nodeCount, ok := metrics["node-count"].(float64); ok {
		if int(nodeCount) != 500 {
			t.Errorf("expected 500 nodes, got %d", int(nodeCount))
		}
	} else {
		t.Error("expected node-count in metrics")
	}

	// Verify exact edge count (chain should have exactly 499 edges)
	if edgeCount, ok := metrics["edge-count"].(float64); ok {
		if int(edgeCount) != 499 {
			t.Errorf("expected 499 edges in linear chain, got %d", int(edgeCount))
		}
	} else {
		t.Error("expected edge-count in metrics")
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
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Direction: "TD", Nodes: nodes, Edges: edges}
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
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Direction: "TD", Nodes: nodes, Edges: edges}
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
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Direction: "TD", Nodes: nodes, Edges: edges}
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
				"node-count": chainNodes,
				"edge-count": chainNodes - 1,
				"max-fanout": 1,
			},
			expectedRules: map[string]int{"max-depth": 1},
			maxDuration:   8 * time.Second,
		},
		{
			name:    "single hub high fan-out",
			diagram: buildHighFanoutDiagram(6000),
			expectedMetrics: map[string]int{
				"node-count": 6001,
				"edge-count": 6000,
				"max-fanout": 6000,
			},
			expectedRules: map[string]int{"max-fanout": 1},
			maxDuration:   8 * time.Second,
		},
		{
			name:    "high fan-in target node",
			diagram: buildHighFaninDiagram(7000),
			expectedMetrics: map[string]int{
				"node-count": 7001,
				"edge-count": 7000,
				"max-fanout": 1,
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
				ruleID, _ := issueMap["rule-id"].(string)
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
				"schema-version": "v1",
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
				if syntaxErrorResp, ok := resp["syntax-error"].(map[string]interface{}); !ok || syntaxErrorResp["message"] == nil {
					return fmt.Errorf("syntax-error payload expected syntax-error object with message, got %v", resp["syntax-error"])
				}
				if metrics, ok := resp["metrics"].(map[string]interface{}); !ok || metrics["diagram-type"] == nil {
					return fmt.Errorf("syntax-error payload expected default metrics with diagram-type, got %v", resp["metrics"])
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
					if issueMap["rule-id"] == "max-fanout" {
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
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{readyError: errors.New("parser is unavailable")}, engine.New())
	h.RegisterRoutes(mux)

	tests := []struct {
		name string
		path string
	}{
		{name: "healthz", path: "/healthz"},
		{name: "health", path: "/health"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			if got := strings.TrimSpace(w.Body.String()); got != `{"status":"ok"}` {
				t.Fatalf("expected body %q, got %q", `{"status":"ok"}`, got)
			}
		})
	}
}

func TestMetrics_ReturnsNotImplementedWhenExporterMissing(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", w.Code)
	}

	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", got)
	}
}

func TestMetrics_ExporterExposesPrometheusText(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{}, engine.New())

	h.SetMetricsHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("# HELP merm8_http_requests_total test\nmerm8_http_requests_total 1\n"))
	}))
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("expected text/plain content-type, got %q", got)
	}

	body := w.Body.String()
	if !strings.Contains(body, "merm8_http_requests_total") {
		t.Fatalf("expected metrics payload to include merm8_http_requests_total, got %q", body)
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
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
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
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
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

func TestAnalyze_Integration_IgnoreNextLineSuppressesOnlyTargetLineForMatchingRule(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParserAndEngine(t, scriptPath, engine.NewWithRules(nextLineProbeRule{}, otherProbeRule{}))

	body := `{
		"code": "graph TD\n%% merm8-ignore-next-line next-line-probe\nA-->B"
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
	if len(resp.Issues) != 2 {
		t.Fatalf("expected 2 remaining issues, got %#v", resp.Issues)
	}

	foundDirectiveLine := false
	foundOtherRule := false
	for _, issue := range resp.Issues {
		if issue.RuleID == "next-line-probe" && issue.Line != nil && *issue.Line == 2 {
			foundDirectiveLine = true
		}
		if issue.RuleID == "other-probe" {
			foundOtherRule = true
		}
		if issue.RuleID == "next-line-probe" && issue.Line != nil && *issue.Line == 3 {
			t.Fatalf("expected next-line suppression to hide matching target-line issue, got %#v", resp.Issues)
		}
	}
	if !foundDirectiveLine {
		t.Fatalf("expected directive-line issue to remain; next-line suppression must not apply to directive line")
	}
	if !foundOtherRule {
		t.Fatalf("expected non-matching rule issue to remain, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_ParserTimeout_Returns504AndHandlerStaysResponsive(t *testing.T) {
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

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 when parser times out, got %d", w.Code)
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
				"schema-version": "v1",
				"rules":          []interface{}{"max-fanout"},
			},
			wantMessage: "config.rules must be object",
			wantPath:    "config.rules",
		},
		{
			name: "rule config must be object",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"max-fanout": 1,
				},
			},
			wantMessage: "config.rules.max-fanout must be object",
			wantPath:    "config.rules.max-fanout",
		},
		{
			name: "nested rule config must be object",
			config: map[string]interface{}{
				"schema-version": "v1",
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
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"max-fanout": map[string]interface{}{
					"severity": "warnx",
				},
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

	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for severity", "config.rules.max-fanout.severity", nil)
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
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"no-cycles-v2": map[string]interface{}{"enabled": false},
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
	assertValidationErrorResponse(t, w.Body.Bytes(), "unknown_rule", "unknown rule: no-cycles-v2", "config.rules.no-cycles-v2", []string{"max-depth", "max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids"})
}

func TestAnalyze_InvalidUnknownRuleConfigWithoutSchemaVersion_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"no-cycles-v2": map[string]interface{}{"enabled": false},
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
	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "config.schema-version is required", "config.schema-version", nil)
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
			"schema-version": "v1",
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
	assertValidationErrorResponse(t, w.Body.Bytes(), "unknown_option", "unknown option: unknown", "config.rules.max-fanout.unknown", []string{"enabled", "limit", "severity", "suppression-selectors"})
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
			"schema-version": "v1",
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
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A; A",
		"config": map[string]interface{}{
			"schema-version": "v1",
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
	diagram := &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A; A",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"no-duplicate-node-ids": map[string]interface{}{"severity": "info"},
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
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
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

func TestRuleConfigSchema_ResponseShape(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/rules/schema", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Schema map[string]any `json:"schema"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /rules/schema response: %v", err)
	}
	if resp.Schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id, got %#v", resp.Schema["$schema"])
	}

	required, ok := resp.Schema["required"].([]any)
	if !ok || len(required) != 2 || required[0] != "schema-version" || required[1] != "rules" {
		t.Fatalf("expected canonical required fields [schema-version rules], got %#v", resp.Schema["required"])
	}

	rulesSchema := resp.Schema["properties"].(map[string]any)["rules"].(map[string]any)
	rulesProps := rulesSchema["properties"].(map[string]any)
	maxFanout := rulesProps["max-fanout"].(map[string]any)
	maxFanoutProps := maxFanout["properties"].(map[string]any)
	if got := maxFanoutProps["limit"].(map[string]any)["minimum"]; got != float64(1) {
		t.Fatalf("expected max-fanout.limit minimum=1, got %#v", got)
	}
	severity := maxFanoutProps["severity"].(map[string]any)
	levels := severity["enum"].([]any)
	if len(levels) != 3 || levels[0] != "error" || levels[1] != "warning" || levels[2] != "info" {
		t.Fatalf("unexpected severity enum: %#v", levels)
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
			DefaultConfig       map[string]interface{} `json:"default-config"`
			ConfigurableOptions []struct {
				Name        string `json:"name"`
				Type        string `json:"type"`
				Description string `json:"description"`
				Constraints string `json:"constraints"`
			} `json:"configurable-options"`
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
			t.Fatalf("expected default-config object for %s", rule.ID)
		}
		if rule.ConfigurableOptions == nil {
			t.Fatalf("expected configurable-options array for %s", rule.ID)
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
	for _, builtin := range []string{"no-duplicate-node-ids", "no-disconnected-nodes", "max-fanout", "no-cycles", "max-depth"} {
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
			DefaultConfig       map[string]interface{} `json:"default-config"`
			ConfigurableOptions []struct {
				Name string `json:"name"`
			} `json:"configurable-options"`
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
				t.Fatalf("missing default-config.%s for %s", key, rule.ID)
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

func TestAnalyze_InvalidMaxDepthLimitConfig_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->B",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"max-depth": map[string]interface{}{"limit": 0},
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
	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for limit", "config.rules.max-depth.limit", nil)
}

func TestAnalyze_InvalidNoCyclesAllowSelfLoopType_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{}, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A-->A",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"no-cycles": map[string]interface{}{"allow-self-loop": "yes"},
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
	assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for allow-self-loop", "config.rules.no-cycles.allow-self-loop", nil)
}
