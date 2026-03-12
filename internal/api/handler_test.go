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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/output/sarif"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// mockParser is a test double for ParserInterface.
type mockParser struct {
	diagram         *model.Diagram
	syntaxErr       *parser.SyntaxError
	parseError      error
	parseFunc       func(string) (*model.Diagram, *parser.SyntaxError, error)
	readyError      error
	versionInfo     *parser.VersionInfo
	versionErr      error
	parseWithConfig func(string, parser.Config) (*model.Diagram, *parser.SyntaxError, error)
	parserConfig    *parser.Config
}

type mockParserWithoutConfig struct {
	parseFunc func(string) (*model.Diagram, *parser.SyntaxError, error)
}

func (m *mockParserWithoutConfig) Parse(code string) (*model.Diagram, *parser.SyntaxError, error) {
	if m.parseFunc != nil {
		return m.parseFunc(code)
	}
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func (m *mockParser) Parse(code string) (*model.Diagram, *parser.SyntaxError, error) {
	if m.parseFunc != nil {
		return m.parseFunc(code)
	}
	return m.diagram, m.syntaxErr, m.parseError
}

func (m *mockParser) ParseWithConfig(code string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
	if m.parseWithConfig != nil {
		return m.parseWithConfig(code, cfg)
	}
	return m.Parse(code)
}

func (m *mockParser) ParserConfig() parser.Config {
	if m.parserConfig != nil {
		return *m.parserConfig
	}
	return parser.DefaultConfig()
}

func (m *mockParser) Ready() error {
	return m.readyError
}

func (m *mockParser) VersionInfo() (*parser.VersionInfo, error) {
	if m.versionErr != nil {
		return nil, m.versionErr
	}
	if m.versionInfo == nil {
		return &parser.VersionInfo{}, nil
	}
	return m.versionInfo, nil
}

type captureLogger struct {
	mu       sync.Mutex
	warnings []string
}

func (l *captureLogger) Info(string, ...any)  {}
func (l *captureLogger) Error(string, ...any) {}
func (l *captureLogger) Warn(msg string, fields ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warnings = append(l.warnings, msg+" "+fmt.Sprint(fields...))
}

func (l *captureLogger) warningText() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.warnings, "\n")
}

// newTestMux creates a test HTTP server backed by a handler using a mock parser.
func newTestMux(mockParseFn func(string) (*model.Diagram, *parser.SyntaxError, error)) *http.ServeMux {
	mux, _ := newTestMuxWithHandler(mockParseFn)
	return mux
}

func newTestMuxWithHandler(mockParseFn func(string) (*model.Diagram, *parser.SyntaxError, error)) (*http.ServeMux, *api.Handler) {
	mux := http.NewServeMux()
	mockP := &mockParser{
		parseFunc: mockParseFn,
	}
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	return mux, h
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

func (nextLineProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

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

func (otherProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (otherProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line := 3
	return []model.Issue{{RuleID: "other-probe", Severity: "warning", Message: "other rule issue", Line: &line}}
}

type metricsConditionalRuleA struct{}

func (metricsConditionalRuleA) ID() string { return "custom/test/metrics-conditional-a" }

func (metricsConditionalRuleA) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (metricsConditionalRuleA) Run(d *model.Diagram, _ rules.Config) []model.Issue {
	if d.Direction != "BT" {
		return nil
	}
	line1 := 2
	line2 := 3
	return []model.Issue{
		{RuleID: "custom/test/metrics-conditional-a", Severity: "warning", Message: "first conditional warning", Line: &line1},
		{RuleID: "custom/test/metrics-conditional-a", Severity: "error", Message: "conditional error", Line: &line2},
	}
}

type metricsConditionalRuleB struct{}

func (metricsConditionalRuleB) ID() string { return "custom/test/metrics-conditional-b" }

func (metricsConditionalRuleB) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (metricsConditionalRuleB) Run(d *model.Diagram, _ rules.Config) []model.Issue {
	if d.Direction != "BT" {
		return nil
	}
	line := 4
	return []model.Issue{{RuleID: "custom/test/metrics-conditional-b", Severity: "warning", Message: "second conditional warning", Line: &line}}
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
	// Response should have 6 required fields; error.details is optional
	if len(resp) < 6 {
		t.Fatalf("expected at least 6 top-level fields, got %d: %v", len(resp), resp)
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

	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object, got %#v", resp["metrics"])
	}
	if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "unknown" {
		t.Fatalf("expected metrics.diagram-type=unknown, got %#v", metrics["diagram-type"])
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
	// error.details is optional and may be present for some error types
}

func assertValidationErrorResponse(t *testing.T, body []byte, wantCode, wantMessage, wantPath string, wantSupported []string) {
	t.Helper()

	var resp struct {
		Valid         bool                   `json:"valid"`
		LintSupported bool                   `json:"lint-supported"`
		SyntaxError   interface{}            `json:"syntax-error"`
		Issues        []model.Issue          `json:"issues"`
		Metrics       map[string]interface{} `json:"metrics"`
		Error         struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details struct {
				Path      string   `json:"path"`
				Supported []string `json:"supported"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("failed to decode raw error response: %v", err)
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
	if resp.Metrics == nil {
		t.Fatal("expected metrics object")
	}
	if got := resp.Metrics["diagram-type"]; got != "unknown" {
		t.Fatalf("expected metrics.diagram-type=unknown, got %#v", got)
	}
	if resp.Error.Code != wantCode {
		t.Fatalf("expected error.code=%q, got %q", wantCode, resp.Error.Code)
	}
	if resp.Error.Message != wantMessage {
		t.Fatalf("expected error.message=%q, got %q", wantMessage, resp.Error.Message)
	}
	if resp.Error.Details.Path != wantPath {
		t.Fatalf("expected error.details.path=%q, got %q", wantPath, resp.Error.Details.Path)
	}
	if len(wantSupported) == 0 {
		if len(resp.Error.Details.Supported) != 0 {
			t.Fatalf("expected empty supported list, got %#v", resp.Error.Details.Supported)
		}
		return
	}
	if strings.Join(resp.Error.Details.Supported, ",") != strings.Join(wantSupported, ",") {
		t.Fatalf("expected supported=%v, got %v", wantSupported, resp.Error.Details.Supported)
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

func TestAnalyze_AcceptsSingleJSONObject(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAnalyze_RejectsTrailingContentAfterJSONObject(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"} {"extra":true}`))
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

func TestAnalyzeV1_ParserTimeout_Returns504AndErrorCode(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: after 1s", parser.ErrTimeout)
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
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
		Valid         bool                   `json:"valid"`
		LintSupported bool                   `json:"lint-supported"`
		SyntaxError   interface{}            `json:"syntax-error"`
		Issues        []interface{}          `json:"issues"`
		Metrics       map[string]interface{} `json:"metrics"`
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
	if got := resp.Metrics["diagram-type"]; got != "unknown" {
		t.Fatalf("expected metrics.diagram-type=unknown, got %#v", got)
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
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
		t.Fatalf("expected lint-supported=false for syntax error (regardless of diagram type), got %v", resp["lint-supported"])
	}
	metrics, ok := resp["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object for syntax error, got %T", resp["metrics"])
	}
	if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != "flowchart" {
		t.Fatalf("expected metrics.diagram-type=flowchart fallback, got %v", metrics["diagram-type"])
	}
}

func hintCodesFromResponse(t *testing.T, resp map[string]interface{}) []string {
	t.Helper()
	rawHints, ok := resp["hints"].([]interface{})
	if !ok {
		t.Fatalf("expected hints array, got %#v", resp["hints"])
	}
	codes := make([]string, 0, len(rawHints))
	for _, raw := range rawHints {
		hint, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected hint object, got %#v", raw)
		}
		code, ok := hint["code"].(string)
		if !ok || code == "" {
			t.Fatalf("expected non-empty hint code, got %#v", hint["code"])
		}
		codes = append(codes, code)
	}
	return codes
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func syntaxErrorResponseForEndpoint(t *testing.T, endpoint, payload, contentType string, syntaxErr *parser.SyntaxError) map[string]interface{} {
	t.Helper()
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	var req *http.Request
	if endpoint == "/v1/analyze" {
		body, err := json.Marshal(map[string]string{"code": payload})
		if err != nil {
			t.Fatalf("marshal analyze body: %v", err)
		}
		req = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(payload))
		req.Header.Set("Content-Type", contentType)
	}

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for %s, got %d", endpoint, w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response for %s: %v", endpoint, err)
	}
	return resp
}

func assertHelpSuggestionContract(t *testing.T, resp map[string]interface{}, wantPresent bool) {
	t.Helper()

	raw, exists := resp["help-suggestion"]
	if !wantPresent {
		if exists {
			t.Fatalf("expected help-suggestion to be omitted, got %#v", raw)
		}
		return
	}
	if !exists {
		t.Fatal("expected help-suggestion key to be present")
	}
	helpSuggestion, ok := raw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected help-suggestion object, got %#v", raw)
	}
	for _, key := range []string{"title", "explanation", "wrong-example", "correct-example", "doc-link", "fix-action"} {
		val, ok := helpSuggestion[key].(string)
		if !ok || val == "" {
			t.Fatalf("expected help-suggestion.%s non-empty string, got %#v", key, helpSuggestion[key])
		}
	}
}

func assertHintCodePresentForAnalyzeAndRaw(t *testing.T, syntaxErr *parser.SyntaxError, payload, hintCode string) {
	t.Helper()
	for _, endpoint := range []string{"/v1/analyze", "/v1/analyze/raw"} {
		resp := syntaxErrorResponseForEndpoint(t, endpoint, payload, "text/plain", syntaxErr)
		hintCodes := hintCodesFromResponse(t, resp)
		if !containsString(hintCodes, hintCode) {
			t.Fatalf("expected %s hint for %s, got %v", hintCode, endpoint, hintCodes)
		}
	}
}

func TestAnalyze_SyntaxError_UnsupportedDiagramTypeHintsAndHelp(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		hintCode     string
		helpContains string
	}{
		{name: "gantt", code: "gantt\n  title Delivery", hintCode: "unsupported_diagram_type_gantt", helpContains: "gantt"},
		{name: "pie", code: "pie\n  title Revenue", hintCode: "unsupported_diagram_type_pie", helpContains: "pie"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			syntaxErr := &parser.SyntaxError{Message: "No diagram type detected", Line: 1, Column: 1}
			for _, endpoint := range []string{"/v1/analyze", "/v1/analyze/raw"} {
				resp := syntaxErrorResponseForEndpoint(t, endpoint, tt.code, "text/plain", syntaxErr)
				hintsRaw := resp["hints"].([]interface{})
				var selected map[string]interface{}
				for _, raw := range hintsRaw {
					h := raw.(map[string]interface{})
					if code, _ := h["code"].(string); code == tt.hintCode {
						selected = h
						break
					}
				}
				if selected == nil {
					t.Fatalf("expected hint %s for %s, got %#v", tt.hintCode, endpoint, hintsRaw)
				}
				if msg, _ := selected["message"].(string); !strings.Contains(strings.ToLower(msg), "currently unavailable") {
					t.Fatalf("expected unavailable message for %s/%s, got %q", endpoint, tt.hintCode, msg)
				}
				appliesTo, ok := selected["applies-to"].(map[string]interface{})
				if !ok {
					t.Fatalf("expected applies-to for %s/%s", endpoint, tt.hintCode)
				}
				if line, ok := appliesTo["line"].(float64); !ok || line != 1 {
					t.Fatalf("expected applies-to.line=1 for %s/%s, got %#v", endpoint, tt.hintCode, appliesTo["line"])
				}

				help, ok := resp["help-suggestion"].(map[string]interface{})
				if !ok || help == nil {
					t.Fatalf("expected help-suggestion for %s/%s", endpoint, tt.hintCode)
				}
				title, _ := help["title"].(string)
				if !strings.Contains(strings.ToLower(title), tt.helpContains) {
					t.Fatalf("expected help title to mention %q for %s, got %q", tt.helpContains, endpoint, title)
				}
				explanation, _ := help["explanation"].(string)
				if !strings.Contains(strings.ToLower(explanation), "does not support") {
					t.Fatalf("expected specific help explanation for %s, got %q", endpoint, explanation)
				}
			}
		})
	}
}

// TestAnalyze_SyntaxError_SuggestionsGraphvizDetection tests suggestions for Graphviz syntax.
func TestAnalyze_SyntaxError_SuggestionsGraphvizDetection(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    0,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "digraph G {\n  A -> B\n}"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	suggestions, ok := resp["suggestions"].([]interface{})
	if !ok {
		t.Fatalf("expected suggestions array, got %#v", resp["suggestions"])
	}
	if len(suggestions) == 0 {
		t.Errorf("expected at least one suggestion for Graphviz syntax")
	}

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "graphviz_syntax_detected") {
		t.Errorf("expected graphviz_syntax_detected hint code, got %v", hintCodes)
	}
	if len(suggestions) > 0 {
		firstHint := resp["hints"].([]interface{})[0].(map[string]interface{})
		if msg, _ := firstHint["message"].(string); suggestions[0] != msg {
			t.Errorf("expected suggestions to be derived from hints, first suggestion=%q first hint message=%q", suggestions[0], msg)
		}
	}
}

// TestAnalyze_SyntaxError_SuggestionsTabDetection tests suggestions for tab indentation.
func TestAnalyze_SyntaxError_SuggestionsTabDetection(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "Unexpected token",
		Line:    2,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	codeWithTab := "flowchart TD\n\tA --> B"
	body, _ := json.Marshal(map[string]string{"code": codeWithTab})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "tab_indentation_detected") {
		t.Errorf("expected tab_indentation_detected hint code, got %v", hintCodes)
	}
}

// TestAnalyze_SyntaxError_SuggestionsArrowSyntax tests suggestions for arrow syntax.
func TestAnalyze_SyntaxError_SuggestionsArrowSyntax(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "Unexpected token",
		Line:    2,
		Column:  5,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	codeWithWrongArrow := "flowchart TD\n  A -> B"
	body, _ := json.Marshal(map[string]string{"code": codeWithWrongArrow})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "flowchart_arrow_operator_detected") {
		t.Errorf("expected flowchart_arrow_operator_detected hint code, got %v", hintCodes)
	}
}

// TestAnalyze_SyntaxError_SuggestionsMissingDiagramType tests suggestions for missing diagram type.
func TestAnalyze_SyntaxError_SuggestionsMissingDiagramType(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    0,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	unclearCode := "A --> B"
	body, _ := json.Marshal(map[string]string{"code": unclearCode})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "missing_diagram_type_keyword") {
		t.Errorf("expected missing_diagram_type_keyword hint code, got %v", hintCodes)
	}
}

func TestAnalyze_SyntaxError_Detectors_MarkdownFence(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected token", Line: 1, Column: 1}
	assertHintCodePresentForAnalyzeAndRaw(t, syntaxErr, "```mermaid\nflowchart TD\n  A --> B\n```", "markdown_fence_detected")

	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", "```\nflowchart TD\n  A --> B\n```", "text/plain", syntaxErr)
	assertHelpSuggestionContract(t, resp, true)
}

func TestAnalyze_SyntaxError_Detectors_StateVariantMigration(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "State syntax parse error", Line: 2, Column: 3}
	payload := "stateDiagram\n  state idle"
	assertHintCodePresentForAnalyzeAndRaw(t, syntaxErr, payload, "state_diagram_variant_migration")

	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", payload, "text/plain", syntaxErr)
	assertHelpSuggestionContract(t, resp, true)
}

func TestAnalyze_SyntaxError_Detectors_SmartPunctuation(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 4}
	payload := "flowchart TD\n  A —> B\n  B[“Quoted”]"
	assertHintCodePresentForAnalyzeAndRaw(t, syntaxErr, payload, "smart_punctuation_detected")

	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", payload, "text/plain", syntaxErr)
	assertHelpSuggestionContract(t, resp, true)
}

func TestAnalyze_SyntaxError_Detectors_FlowchartReservedEnd(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 1}
	payload := "flowchart TD\nend\nA --> B"
	assertHintCodePresentForAnalyzeAndRaw(t, syntaxErr, payload, "flowchart_reserved_end_keyword")

	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", payload, "text/plain", syntaxErr)
	assertHelpSuggestionContract(t, resp, true)
}

func TestAnalyze_SyntaxError_Detectors_MalformedLabelBrackets(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 8}
	payload := "flowchart TD\n  A[Start --> B[End]"
	assertHintCodePresentForAnalyzeAndRaw(t, syntaxErr, payload, "malformed_label_brackets")

	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", payload, "text/plain", syntaxErr)
	assertHelpSuggestionContract(t, resp, true)
}

func TestAnalyze_SyntaxError_HintsSortedByConfidence(t *testing.T) {
	syntaxErr := &parser.SyntaxError{Message: "Unexpected state syntax", Line: 2, Column: 4}
	payload := "```mermaid\nstateDiagram\n\tA —> B[Unclosed\n```"
	resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", payload, "text/plain", syntaxErr)
	hintsRaw := resp["hints"].([]interface{})
	if len(hintsRaw) < 2 {
		t.Fatalf("expected multiple hints, got %d", len(hintsRaw))
	}
	prev := 2.0
	for _, raw := range hintsRaw {
		hint := raw.(map[string]interface{})
		confidence, ok := hint["confidence"].(float64)
		if !ok {
			t.Fatalf("expected confidence number, got %#v", hint["confidence"])
		}
		if confidence > prev {
			t.Fatalf("expected non-increasing confidence ordering, got %v then %v", prev, confidence)
		}
		prev = confidence
	}
}

// TestAnalyzeHelp_Returns200 tests that /analyze/help endpoint returns proper help data.

func hintByCode(t *testing.T, resp map[string]interface{}, code string) map[string]interface{} {
	t.Helper()
	hintsRaw, ok := resp["hints"].([]interface{})
	if !ok {
		t.Fatalf("expected hints array, got %#v", resp["hints"])
	}
	for _, raw := range hintsRaw {
		hint, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if hintCode, _ := hint["code"].(string); hintCode == code {
			return hint
		}
	}
	t.Fatalf("expected hint code %s in %#v", code, hintsRaw)
	return nil
}

func TestAnalyze_SyntaxError_GenericFallbackContextAndBounds(t *testing.T) {
	tests := []struct {
		name                string
		syntaxErr           *parser.SyntaxError
		payload             string
		wantHintContains    []string
		wantHintNotContains []string
		wantFixContains     []string
		wantFixNotContains  []string
	}{
		{
			name:                "line and column are zero",
			syntaxErr:           &parser.SyntaxError{Message: "Unexpected token", Line: 0, Column: 0},
			payload:             "flowchart TD\n  A --> B",
			wantHintContains:    []string{"Check for incomplete edge definitions"},
			wantHintNotContains: []string{"Check near column"},
			wantFixContains:     []string{"Review the line near the reported syntax-error"},
			wantFixNotContains:  []string{"Check near column"},
		},
		{
			name:             "column out of range is clamped and excerpt included",
			syntaxErr:        &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 999},
			payload:          "flowchart TD\n  A? B",
			wantHintContains: []string{"Check near column 6 on line 2", "`  A? B`"},
			wantFixContains:  []string{"Check near column 6 on line 2", "`  A? B`"},
		},
		{
			name:                "empty line clamps column to zero",
			syntaxErr:           &parser.SyntaxError{Message: "Unexpected token", Line: 2, Column: 999},
			payload:             "flowchart TD\n",
			wantHintContains:    []string{"Check for incomplete edge definitions"},
			wantHintNotContains: []string{"Check near column"},
			wantFixContains:     []string{"Review the line near the reported syntax-error"},
			wantFixNotContains:  []string{"Check near column"},
		},
		{
			name:                "line out of range stays stable",
			syntaxErr:           &parser.SyntaxError{Message: "Unexpected token", Line: 99, Column: 3},
			payload:             "flowchart TD\n  A --> B",
			wantHintContains:    []string{"Check for incomplete edge definitions"},
			wantHintNotContains: []string{"Check near column"},
			wantFixContains:     []string{"Review the line near the reported syntax-error"},
			wantFixNotContains:  []string{"Check near column"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp := syntaxErrorResponseForEndpoint(t, "/v1/analyze", tt.payload, "text/plain", tt.syntaxErr)
			hint := hintByCode(t, resp, "generic_syntax_error")
			msg, ok := hint["message"].(string)
			if !ok {
				t.Fatalf("expected generic hint message string, got %#v", hint["message"])
			}
			for _, want := range tt.wantHintContains {
				if !strings.Contains(msg, want) {
					t.Fatalf("expected generic hint message to include %q, got %q", want, msg)
				}
			}
			for _, avoid := range tt.wantHintNotContains {
				if strings.Contains(msg, avoid) {
					t.Fatalf("expected generic hint message to not include %q, got %q", avoid, msg)
				}
			}

			helpRaw, ok := resp["help-suggestion"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected help-suggestion object, got %#v", resp["help-suggestion"])
			}
			fixAction, ok := helpRaw["fix-action"].(string)
			if !ok {
				t.Fatalf("expected fix-action string, got %#v", helpRaw["fix-action"])
			}
			for _, want := range tt.wantFixContains {
				if !strings.Contains(fixAction, want) {
					t.Fatalf("expected fix-action to include %q, got %q", want, fixAction)
				}
			}
			for _, avoid := range tt.wantFixNotContains {
				if strings.Contains(fixAction, avoid) {
					t.Fatalf("expected fix-action to not include %q, got %q", avoid, fixAction)
				}
			}
		})
	}
}

func TestAnalyzeHelp_Returns200(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/analyze/help", nil)
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
	if _, hasTypes := resp["diagram-types"]; !hasTypes {
		t.Errorf("expected diagram-types field")
	}
	if _, hasErrors := resp["common-errors"]; !hasErrors {
		t.Errorf("expected common-errors field")
	}
	if _, hasArrows := resp["arrow-syntax"]; !hasArrows {
		t.Errorf("expected arrow-syntax field")
	}
	if _, hasResources := resp["resources"]; !hasResources {
		t.Errorf("expected resources field")
	}

	// Verify diagram-types contains examples
	if types, ok := resp["diagram-types"].(map[string]interface{}); ok {
		if len(types) == 0 {
			t.Errorf("expected diagram-types to have content")
		}
		if flowchart, ok := types["flowchart"]; ok {
			if fcMap, ok := flowchart.(map[string]interface{}); ok {
				if _, hasDesc := fcMap["description"]; !hasDesc {
					t.Errorf("expected description in flowchart template")
				}
				if _, hasExample := fcMap["example"]; !hasExample {
					t.Errorf("expected example in flowchart template")
				}
			}
		}
	}
}

// TestAnalyzeRaw_SyntaxError_IncludesSuggestions tests that /v1/analyze/raw endpoint includes suggestions for syntax errors.
func TestAnalyzeRaw_SyntaxError_IncludesSuggestions(t *testing.T) {
	syntaxErr := &parser.SyntaxError{
		Message: "No diagram type detected",
		Line:    1,
		Column:  0,
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, syntaxErr, nil
	})

	// Test /v1/analyze/raw endpoint with Graphviz syntax (common mistake)
	codeWithGraphviz := "digraph G { A -> B }"
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(codeWithGraphviz))
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

	// Verify suggestions are present and actionable
	suggestions, ok := resp["suggestions"].([]interface{})
	if !ok {
		t.Fatalf("expected suggestions array, got %#v", resp["suggestions"])
	}
	if len(suggestions) == 0 {
		t.Errorf("expected suggestions for /raw/ endpoint with Graphviz syntax")
	}

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "graphviz_syntax_detected") {
		t.Errorf("expected graphviz_syntax_detected hint code in /raw/ response, got %v", hintCodes)
	}

	// Verify valid=false
	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Errorf("expected valid=false for /raw/ with syntax error, got %v", resp["valid"])
	}

	// Verify syntax-error details are present
	if syntaxErrMap, ok := resp["syntax-error"].(map[string]interface{}); !ok || syntaxErrMap == nil {
		t.Errorf("expected syntax-error object in /raw/ response")
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
	if valid, ok := resp["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid=true for parsed diagram, got %v", resp["valid"])
	}
	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "sequence" {
		t.Fatalf("expected diagram-type=sequence, got %v", resp["diagram-type"])
	}
	issues, ok := resp["issues"].([]interface{})
	if !ok {
		t.Fatalf("expected issues array, got %#v", resp["issues"])
	}
	if len(issues) != 1 {
		t.Fatalf("expected one unsupported-diagram-type issue, got %#v", issues)
	}
	issueMap, ok := issues[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected issue object, got %#v", issues[0])
	}
	if ruleID, ok := issueMap["rule-id"].(string); !ok || ruleID != "unsupported-diagram-type" {
		t.Fatalf("expected rule-id=unsupported-diagram-type, got %#v", issueMap["rule-id"])
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
	for _, key := range []string{
		"node-count",
		"edge-count",
		"disconnected-node-count",
		"duplicate-node-count",
		"max-fanin",
		"max-fanout",
	} {
		if got, ok := metrics[key].(float64); !ok || got != 0 {
			t.Fatalf("expected metrics.%s=0, got %#v", key, metrics[key])
		}
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

func TestAnalyze_ConfigLegacySnakeCaseKeysAcceptedWithWarning(t *testing.T) {
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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for legacy snake_case config keys in phase 1, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Deprecation"); got != "true" {
		t.Fatalf("expected Deprecation header true, got %q", got)
	}
	if got := w.Header().Get("Warning"); got == "" {
		t.Fatal("expected Warning header to be present")
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	warnings, ok := resp["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected warnings array in response, got %#v", resp["warnings"])
	}
}

func TestAnalyze_LegacyConfigShapesWarnAndStillApplyConfig(t *testing.T) {
	diagram := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
		},
	}

	tests := []struct {
		name   string
		config map[string]any
	}{
		{
			name: "flat legacy config",
			config: map[string]any{
				"max-fanout": map[string]any{"limit": 1},
			},
		},
		{
			name: "nested legacy config",
			config: map[string]any{
				"rules": map[string]any{
					"max-fanout": map[string]any{"limit": 1},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var parseCalls atomic.Int32
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				parseCalls.Add(1)
				return diagram, nil, nil
			})

			bodyJSON, _ := json.Marshal(map[string]any{
				"code":   "graph TD\n  A --> B\n  A --> C",
				"config": tt.config,
			})

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(bodyJSON))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d body=%s", tt.name, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("expected Deprecation header true for %s, got %q", tt.name, got)
			}
			if got := w.Header().Get("Warning"); got == "" {
				t.Fatalf("expected Warning header to be present for %s", tt.name)
			}
			if parseCalls.Load() == 0 {
				t.Fatalf("expected parser to run for %s", tt.name)
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response for %s: %v", tt.name, err)
			}
			warnings, ok := resp["warnings"].([]any)
			if !ok || len(warnings) == 0 {
				t.Fatalf("expected non-empty warnings array for %s, got %#v", tt.name, resp["warnings"])
			}

			issues, ok := resp["issues"].([]any)
			if !ok {
				t.Fatalf("expected issues array for %s, got %T", tt.name, resp["issues"])
			}
			if len(issues) != 1 {
				t.Fatalf("expected exactly 1 issue with max-fanout.limit=1 for %s, got %d (%#v)", tt.name, len(issues), issues)
			}
			issue, ok := issues[0].(map[string]any)
			if !ok || issue["rule-id"] != "max-fanout" {
				t.Fatalf("expected max-fanout issue for %s, got %#v", tt.name, issues[0])
			}
		})
	}
}

func TestAnalyze_ConfigCanonicalFormatNoDeprecationWarnings(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}, nil, nil
	})

	bodyJSON, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A --> B",
		"config": map[string]any{
			"schema-version": "v1",
			"rules": map[string]any{
				"max-fanout": map[string]any{
					"suppression-selectors": []string{"node:A"},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for canonical config, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Deprecation"); got != "" {
		t.Fatalf("expected no Deprecation header, got %q", got)
	}
	if got := w.Header().Get("Warning"); got != "" {
		t.Fatalf("expected no Warning header, got %q", got)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, exists := resp["warnings"]; exists {
		t.Fatalf("expected warnings to be absent for canonical format, got %#v", resp["warnings"])
	}
	if _, exists := resp["meta"]; exists {
		t.Fatalf("expected meta warnings to be absent for canonical format, got %#v", resp["meta"])
	}
}

func TestAnalyze_LegacyConfigWarningsIncludeStructuredMetadataAndLogHint(t *testing.T) {
	mux := http.NewServeMux()
	log := &captureLogger{}
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}, nil, nil
	}}, engine.New())
	h.SetLogger(log)
	h.RegisterRoutes(mux)

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

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for legacy config, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Values("Warning"); len(got) == 0 {
		t.Fatal("expected Warning headers to be present")
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	meta, ok := resp["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta object in response, got %#v", resp["meta"])
	}
	metaWarnings, ok := meta["warnings"].([]any)
	if !ok || len(metaWarnings) == 0 {
		t.Fatalf("expected structured meta warnings in response, got %#v", meta["warnings"])
	}
	firstWarning, ok := metaWarnings[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first warning object, got %#v", metaWarnings[0])
	}
	if firstWarning["replacement"] == "" {
		t.Fatalf("expected replacement example in structured warning, got %#v", firstWarning)
	}

	logText := log.warningText()
	if !strings.Contains(logText, "schema-version") || !strings.Contains(logText, "Example") {
		t.Fatalf("expected migration hint in warning logs, got %s", logText)
	}
}

func TestAnalyze_ConfigLegacySnakeCaseKeysRejectedWhenPhaseFlips(t *testing.T) {
	mux, h := newTestMuxWithHandler(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})
	h.SetStrictConfigSchema(true)

	bodyJSON, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A --> B",
		"config": map[string]any{
			"schema_version": "v1",
			"rules":          map[string]any{},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 in strict phase, got %d body=%s", w.Code, w.Body.String())
	}
	assertValidationErrorResponse(t, w.Body.Bytes(), "deprecated_config_format", "config.schema_version is deprecated; use config.schema-version", "config.schema_version", nil)
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

	t.Run("accepts versioned config without rules and normalizes to empty rules", func(t *testing.T) {
		mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		})

		body := []byte(`{"code":"graph TD; A-->B","config":{"schema-version":"v1"}}`)
		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for schema-version without rules, got %d body=%s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if valid, ok := resp["valid"].(bool); !ok || !valid {
			t.Fatalf("expected valid=true response, got %#v", resp["valid"])
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
			mux, h := newTestMuxWithHandler(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				parserCalled = true
				return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
			})
			h.SetStrictConfigSchema(true)
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
			assertValidationErrorResponse(t, w.Body.Bytes(), "deprecated_config_format", "legacy unversioned config shape is deprecated; use config.schema-version and config.rules", "config", nil)
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

func TestAnalyze_MetricsDisconnectedNodeUnionIncludesSourceAnalysisOnlyNodes(t *testing.T) {
	diagram := &model.Diagram{
		Type:                model.DiagramTypeFlowchart,
		Direction:           "TD",
		Nodes:               []model.Node{{ID: "A"}, {ID: "B"}},
		Edges:               []model.Edge{{From: "A", To: "B", Type: "arrow"}},
		DisconnectedNodeIDs: []string{"orphan", "orphan", "B"},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": `graph TD
  A-->B
  orphan`})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	metrics, ok := resp["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", resp["metrics"])
	}

	if got := metrics["disconnected-node-count"]; got != float64(1) {
		t.Fatalf("expected disconnected-node-count=1 (only 'orphan', B is connected), got %v", got)
	}
}

func TestAnalyze_MetricsDuplicateNodeUnionIncludesSourceAnalysisOnlyNodes(t *testing.T) {
	diagram := &model.Diagram{
		Type:             model.DiagramTypeFlowchart,
		Direction:        "TD",
		Nodes:            []model.Node{{ID: "A"}, {ID: "B"}},
		Edges:            []model.Edge{{From: "A", To: "B", Type: "arrow"}},
		DuplicateNodeIDs: []string{"ghost", "ghost"},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": `graph TD
  A-->B
  ghost`})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	metrics, ok := resp["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", resp["metrics"])
	}

	if got := metrics["duplicate-node-count"]; got != float64(1) {
		t.Fatalf("expected duplicate-node-count=1 (only 'ghost' from DuplicateNodeIDs), got %v", got)
	}
}

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

func readInternalAnalyzeMetrics(t *testing.T, mux *http.ServeMux) map[string]map[string]float64 {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/internal/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /internal/metrics 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got map[string]map[string]float64
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode /internal/metrics response: %v", err)
	}
	return got
}

func readHealthMetricsResponse(t *testing.T, mux *http.ServeMux) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/health/metrics", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /v1/health/metrics 200, got %d body=%s", w.Code, w.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode /v1/health/metrics response: %v", err)
	}
	return got
}

func TestHealthMetrics_ConcurrentCounterUpdatesPreserveTotals(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		switch code {
		case "VALID":
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		case "SYNTAX":
			return nil, &parser.SyntaxError{Message: "bad syntax", Line: 1, Column: 1}, nil
		case "TIMEOUT":
			return nil, nil, parser.ErrTimeout
		case "SUBPROCESS":
			return nil, nil, parser.ErrSubprocess
		case "DECODE":
			return nil, nil, parser.ErrDecode
		case "CONTRACT":
			return nil, nil, parser.ErrContract
		default:
			return nil, nil, errors.New("unexpected parser crash")
		}
	}}, engine.New())
	h.RegisterRoutes(mux)

	outcomes := []string{"VALID", "SYNTAX", "TIMEOUT", "SUBPROCESS", "DECODE", "CONTRACT", "INTERNAL"}
	const goroutines = 12
	const callsPerGoroutine = 120

	errCh := make(chan error, goroutines*callsPerGoroutine)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		start := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				code := outcomes[(start+j)%len(outcomes)]
				body := []byte(fmt.Sprintf(`{"code":%q}`, code))
				req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				if w.Code == http.StatusMethodNotAllowed {
					errCh <- fmt.Errorf("analyze request unexpectedly returned 405")
					return
				}
			}
		}()
	}

	const metricReads = 800
	for i := 0; i < metricReads; i++ {
		resp := readHealthMetricsResponse(t, mux)

		total := uint64(resp["total-requests"].(float64))
		success := uint64(resp["successful-analyses"].(map[string]any)["total"].(float64))
		failed := uint64(resp["failed-analyses"].(map[string]any)["total"].(float64))

		if failed > total {
			t.Fatalf("failed.total should never exceed total-requests, got failed=%d total=%d", failed, total)
		}
		if total != success+failed {
			t.Fatalf("expected total-requests == successful-analyses.total + failed-analyses.total, got total=%d success=%d failed=%d", total, success, failed)
		}
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	resp := readHealthMetricsResponse(t, mux)
	total := uint64(resp["total-requests"].(float64))
	success := uint64(resp["successful-analyses"].(map[string]any)["total"].(float64))
	failed := uint64(resp["failed-analyses"].(map[string]any)["total"].(float64))

	if failed > total {
		t.Fatalf("final failed.total should never exceed total-requests, got failed=%d total=%d", failed, total)
	}
	if total != success+failed {
		t.Fatalf("final total invariant violated: total=%d success=%d failed=%d", total, success, failed)
	}
}

func TestInternalMetrics_AnalyzeOutcomeCounters(t *testing.T) {
	parseErrByCode := map[string]error{
		"TIMEOUT":    parser.ErrTimeout,
		"SUBPROCESS": parser.ErrSubprocess,
		"DECODE":     parser.ErrDecode,
		"CONTRACT":   parser.ErrContract,
		"INTERNAL":   errors.New("unexpected parser crash"),
	}

	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		switch code {
		case "VALID":
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		case "SYNTAX":
			return nil, &parser.SyntaxError{Message: "bad syntax", Line: 1, Column: 1}, nil
		default:
			if err, ok := parseErrByCode[code]; ok {
				return nil, nil, err
			}
			return nil, nil, errors.New("unmapped parse scenario")
		}
	}}, engine.New())
	h.RegisterRoutes(mux)

	before := readInternalAnalyzeMetrics(t, mux)

	testCases := []struct {
		code       string
		wantStatus int
	}{
		{code: "VALID", wantStatus: http.StatusOK},
		{code: "SYNTAX", wantStatus: http.StatusOK},
		{code: "TIMEOUT", wantStatus: http.StatusGatewayTimeout},
		{code: "SUBPROCESS", wantStatus: http.StatusInternalServerError},
		{code: "DECODE", wantStatus: http.StatusInternalServerError},
		{code: "CONTRACT", wantStatus: http.StatusInternalServerError},
		{code: "INTERNAL", wantStatus: http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		body := []byte(fmt.Sprintf(`{"code":%q}`, tc.code))
		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != tc.wantStatus {
			t.Fatalf("code=%s expected status=%d, got %d body=%s", tc.code, tc.wantStatus, w.Code, w.Body.String())
		}
	}

	after := readInternalAnalyzeMetrics(t, mux)

	assertDelta := func(section, key string, want float64) {
		t.Helper()
		got := after[section][key] - before[section][key]
		if got != want {
			t.Fatalf("expected delta %s.%s=%v, got %v (before=%v after=%v)", section, key, want, got, before[section][key], after[section][key])
		}
	}

	assertDelta("analyze", "valid_success", 1)
	assertDelta("analyze", "syntax_error", 1)
	assertDelta("analyze", "other", 0)
	assertDelta("parser", "timeout", 1)
	assertDelta("parser", "subprocess", 1)
	assertDelta("parser", "decode", 1)
	assertDelta("parser", "contract", 1)
	assertDelta("parser", "internal", 1)
	assertDelta("parser", "other", 0)
}

func TestAnalyzeOutcomeOther_IncludedInInternalAndHealthMetrics(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(_ string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	}}, engine.New())
	h.RegisterRoutes(mux)

	beforeInternal := readInternalAnalyzeMetrics(t, mux)
	beforeHealth := readHealthMetricsResponse(t, mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid JSON request to return 400, got %d body=%s", w.Code, w.Body.String())
	}

	afterInternal := readInternalAnalyzeMetrics(t, mux)
	if got := afterInternal["analyze"]["other"] - beforeInternal["analyze"]["other"]; got != 1 {
		t.Fatalf("expected analyze.other delta=1, got %v", got)
	}
	if got := afterInternal["parser"]["other"] - beforeInternal["parser"]["other"]; got != 1 {
		t.Fatalf("expected parser.other delta=1, got %v", got)
	}

	afterHealth := readHealthMetricsResponse(t, mux)
	failedBefore := beforeHealth["failed-analyses"].(map[string]any)
	failedAfter := afterHealth["failed-analyses"].(map[string]any)
	metricValue := func(values map[string]any, key string) float64 {
		t.Helper()
		value, ok := values[key]
		if !ok {
			return 0
		}
		asFloat, ok := value.(float64)
		if !ok {
			t.Fatalf("expected %s to be float64, got %T", key, value)
		}
		return asFloat
	}

	if got := metricValue(failedAfter, "other") - metricValue(failedBefore, "other"); got != 1 {
		t.Fatalf("expected failed-analyses.other delta=1, got %v", got)
	}
	if got := afterHealth["total-requests"].(float64) - beforeHealth["total-requests"].(float64); got != 1 {
		t.Fatalf("expected total-requests delta=1, got %v", got)
	}
	if got := failedAfter["total"].(float64) - failedBefore["total"].(float64); got != 1 {
		t.Fatalf("expected failed-analyses.total delta=1, got %v", got)
	}
}

func TestDiagramTypes_ReturnsParserAndLintSupport(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/diagram-types", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		ParserRecognized []string `json:"parser-recognized"`
		LintSupported    []string `json:"lint-supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /diagram-types response: %v", err)
	}

	wantParser := []string{"flowchart", "sequence", "class", "er", "state"}
	if !reflect.DeepEqual(resp.ParserRecognized, wantParser) {
		t.Fatalf("expected parser-recognized=%v, got %v", wantParser, resp.ParserRecognized)
	}

	wantLint := []string{"flowchart"}
	if !reflect.DeepEqual(resp.LintSupported, wantLint) {
		t.Fatalf("expected lint-supported=%v, got %v", wantLint, resp.LintSupported)
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
		_, _ = w.Write([]byte("# HELP request_total test\nrequest_total 1\n"))
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
	if !strings.Contains(body, "request_total") {
		t.Fatalf("expected metrics payload to include request_total, got %q", body)
	}
}

func TestVersion_ReturnsBuildAndParserMetadata(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{versionInfo: &parser.VersionInfo{ParserVersion: "1.0.0", MermaidVersion: "11.12.3"}}, engine.New())
	h.SetServiceVersion("2.3.4")
	h.SetBuildMetadata("abc1234", "2026-03-04T00:00:00Z")
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected application/json content-type, got %q", got)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	required := []string{"version", "build-commit", "build-time", "parser-version", "mermaid-version"}
	for _, key := range required {
		value, ok := payload[key].(string)
		if !ok || value == "" {
			t.Fatalf("expected non-empty string field %q, got %#v", key, payload[key])
		}
	}
}

func TestInfo_ReturnsServiceAndParserMetadata(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{versionInfo: &parser.VersionInfo{ParserVersion: "1.0.0", MermaidVersion: "11.12.3"}}, engine.New())
	h.SetServiceVersion("2.3.4")
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /info response: %v", err)
	}
	if resp["service-version"] != "2.3.4" {
		t.Fatalf("expected service-version=2.3.4, got %#v", resp["service-version"])
	}
	if resp["parser-version"] != "1.0.0" {
		t.Fatalf("expected parser-version=1.0.0, got %#v", resp["parser-version"])
	}
	if resp["mermaid-version"] != "11.12.3" {
		t.Fatalf("expected mermaid-version=11.12.3, got %#v", resp["mermaid-version"])
	}
	parserRecognized, ok := resp["parser-recognized"].([]any)
	if !ok || len(parserRecognized) == 0 {
		t.Fatalf("expected parser-recognized array, got %#v", resp["parser-recognized"])
	}
	lintSupported, ok := resp["lint-supported"].([]any)
	if !ok || len(lintSupported) == 0 {
		t.Fatalf("expected lint-supported array, got %#v", resp["lint-supported"])
	}
	if supportedRules, ok := resp["supported-rules"].([]any); !ok || len(supportedRules) == 0 {
		t.Fatalf("expected supported-rules array, got %#v", resp["supported-rules"])
	}
	if supportedRuleIDs, ok := resp["supported-rule-ids"].([]any); !ok || len(supportedRuleIDs) == 0 {
		t.Fatalf("expected supported-rule-ids array, got %#v", resp["supported-rule-ids"])
	}

	// Verify snake_case aliases are NOT present (v1.1.0 removed them)
	if resp["service_version"] != nil {
		t.Fatalf("unexpected snake_case service_version alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["parser_version"] != nil {
		t.Fatalf("unexpected snake_case parser_version alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["mermaid_version"] != nil {
		t.Fatalf("unexpected snake_case mermaid_version alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["parser_recognized"] != nil {
		t.Fatalf("unexpected snake_case parser_recognized alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["lint_supported"] != nil {
		t.Fatalf("unexpected snake_case lint_supported alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["supported_rules"] != nil {
		t.Fatalf("unexpected snake_case supported_rules alias in response (v1.1.0 removed snake_case fields)")
	}
	if resp["supported_rule_ids"] != nil {
		t.Fatalf("unexpected snake_case supported_rule_ids alias in response (v1.1.0 removed snake_case fields)")
	}
}

func TestInfo_IncludesEngineRegisteredSupportedRuleIDs(t *testing.T) {
	mux := http.NewServeMux()
	eng := engine.New()
	h := api.NewHandler(&mockParser{}, eng)
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/info", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		SupportedRuleIDs []string `json:"supported-rule-ids"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode /v1/info response: %v", err)
	}

	want := make([]string, 0, len(eng.KnownRuleIDs()))
	for ruleID := range eng.KnownRuleIDs() {
		want = append(want, ruleID)
	}
	sort.Strings(want)

	if !reflect.DeepEqual(resp.SupportedRuleIDs, want) {
		t.Fatalf("expected supported-rule-ids=%v, got %v", want, resp.SupportedRuleIDs)
	}
}

func TestReady_OptionallyIncludesVersionMetadata(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{versionInfo: &parser.VersionInfo{ParserVersion: "1.0.0", MermaidVersion: "11.12.3"}}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ready" {
		t.Fatalf("expected status=ready, got %q", resp["status"])
	}
	if resp["parser-version"] != "1.0.0" {
		t.Fatalf("expected parser-version=1.0.0, got %q", resp["parser-version"])
	}
	if resp["mermaid-version"] != "11.12.3" {
		t.Fatalf("expected mermaid-version=11.12.3, got %q", resp["mermaid-version"])
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

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "not_ready" {
		t.Fatalf("expected status=not_ready, got %#v", resp["status"])
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %#v", resp["error"])
	}
	if errObj["code"] != "not_ready" {
		t.Fatalf("expected error.code=not_ready, got %#v", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); msg == "" {
		t.Fatal("expected non-empty error.message")
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

func TestAnalyze_Integration_UnsupportedDiagramTypes(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	tests := []struct {
		name         string
		code         string
		expectedType string
	}{
		{
			name:         "class diagram",
			code:         "classDiagram\nClass01 <|-- AveryLongClass : Cool",
			expectedType: "class",
		},
		{
			name:         "sequence diagram",
			code:         "sequenceDiagram\nAlice->>Bob: Hi",
			expectedType: "sequence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"code": tt.code})
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if valid, ok := resp["valid"].(bool); !ok || !valid {
				t.Fatalf("expected valid=true for parsed diagrams, got %#v", resp["valid"])
			}
			if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
				t.Fatalf("expected lint-supported=false, got %#v", resp["lint-supported"])
			}
			if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != tt.expectedType {
				t.Fatalf("expected diagram-type=%s, got %#v", tt.expectedType, resp["diagram-type"])
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatalf("expected issues array, got %#v", resp["issues"])
			}

			foundUnsupportedIssue := false
			for _, issue := range issues {
				issueMap, ok := issue.(map[string]interface{})
				if !ok {
					continue
				}
				if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "unsupported-diagram-type" {
					foundUnsupportedIssue = true
					break
				}
			}
			if !foundUnsupportedIssue {
				t.Fatalf("expected issues to contain rule-id=unsupported-diagram-type, got %#v", issues)
			}

			metrics, ok := resp["metrics"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected metrics object, got %#v", resp["metrics"])
			}
			if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != tt.expectedType {
				t.Fatalf("expected metrics.diagram-type=%s, got %#v", tt.expectedType, metrics["diagram-type"])
			}
			for _, key := range []string{
				"node-count",
				"edge-count",
				"disconnected-node-count",
				"duplicate-node-count",
				"max-fanin",
				"max-fanout",
			} {
				if got, ok := metrics[key].(float64); !ok || got != 0 {
					t.Fatalf("expected metrics.%s=0, got %#v", key, metrics[key])
				}
			}
		})
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
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_timeout", "parser timed out while validating Mermaid code")

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
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
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

	// With graceful degradation, unknown rules return 200 with warnings
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for graceful degradation of unknown rule, got %d", w.Code)
	}
	if !parserCalled {
		t.Fatal("expected parser to be called even with unknown rules (graceful degradation)")
	}

	// Verify warnings about unknown rule are present
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	warnings, ok := resp["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Errorf("expected warnings array for unknown rule no-cycles-v2, got: %#v", resp)
	}
}

func TestAnalyze_InvalidUnknownRuleConfigWithoutSchemaVersion_Returns400(t *testing.T) {
	parserCalled := false
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		parserCalled = true
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
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

	// With graceful degradation, unknown rules return 200 with warnings
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for graceful degradation of unknown rule, got %d", w.Code)
	}
	if !parserCalled {
		t.Fatal("expected parser to be called even with unknown rules (graceful degradation)")
	}

	// Verify warnings about unknown rule are present
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	warnings, ok := resp["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Errorf("expected warnings array for unknown rule no-cycles-v2, got: %#v", resp)
	}
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

func TestAnalyze_SeverityOverride_NormalizesCaseAndWhitespace(t *testing.T) {
	diagram := &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}}, Edges: []model.Edge{{From: "A", To: "B"}}}
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]interface{}{
		"code": "graph TD; A; A",
		"config": map[string]interface{}{
			"schema-version": "v1",
			"rules": map[string]interface{}{
				"no-duplicate-node-ids": map[string]interface{}{"severity": "  Warning  "},
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
	if resp.Issues[0].Severity != "warning" {
		t.Fatalf("expected normalized severity warning, got %q", resp.Issues[0].Severity)
	}
}

func TestAnalyze_WarnSeverityAliasRejected_Returns400(t *testing.T) {
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
					"severity": "warn",
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
	if got := secondW.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After header value 1, got %q", got)
	}
	assertExactErrorResponse(t, secondW.Body.Bytes(), "server_busy", "parser concurrency limit reached; try again")

	close(release)
	<-done
}

func TestAnalyzeSARIF_ParserConcurrencyLimitReached_Returns503WithRetryAfter(t *testing.T) {
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

	firstReq := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstW := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(firstW, firstReq)
		close(done)
	}()

	<-start

	secondReq := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondW := httptest.NewRecorder()
	mux.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when parser concurrency is exhausted, got %d", secondW.Code)
	}
	if got := secondW.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After header value 1, got %q", got)
	}

	var report sarif.Report
	if err := json.Unmarshal(secondW.Body.Bytes(), &report); err != nil {
		t.Fatalf("failed to decode SARIF response: %v", err)
	}
	if len(report.Runs) == 0 || len(report.Runs[0].Invocations) == 0 {
		t.Fatalf("expected SARIF invocations with error details, got %#v", report)
	}
	if got := report.Runs[0].Invocations[0].Properties["error-code"]; got != "server_busy" {
		t.Fatalf("expected SARIF error-code=server_busy, got %q", got)
	}

	close(release)
	<-done
}

func TestAnalyzeSARIF_AcceptsSingleJSONObject(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/sarif", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAnalyzeSARIF_RejectsTrailingContentAfterJSONObject(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/sarif", strings.NewReader(`{"code":"graph TD;A-->B"} trailing`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var report sarif.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatalf("failed to decode SARIF response: %v", err)
	}
	if len(report.Runs) == 0 || len(report.Runs[0].Invocations) == 0 {
		t.Fatalf("expected SARIF invocations with error details, got %#v", report)
	}
	if got := report.Runs[0].Invocations[0].Properties["error-code"]; got != "invalid_json" {
		t.Fatalf("expected SARIF error-code=invalid_json, got %q", got)
	}
}

func TestAnalyze_ParserConcurrencyLimit_HighConcurrencyContention(t *testing.T) {
	const (
		limit      = 2
		totalCalls = 12
	)

	start := make(chan struct{})
	release := make(chan struct{})
	entered := make(chan struct{}, totalCalls)

	var inFlight int32
	var peakInFlight int32

	mockP := &mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			peak := atomic.LoadInt32(&peakInFlight)
			if current <= peak || atomic.CompareAndSwapInt32(&peakInFlight, peak, current) {
				break
			}
		}

		entered <- struct{}{}
		<-release
		atomic.AddInt32(&inFlight, -1)
		return &model.Diagram{}, nil, nil
	}}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(limit)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})

	type result struct {
		status    int
		errorCode string
	}
	results := make(chan result, totalCalls)

	var wg sync.WaitGroup
	for i := 0; i < totalCalls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			res := result{status: w.Code}
			if w.Code != http.StatusOK {
				var payload struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
					t.Errorf("failed to decode response: %v", err)
				} else {
					res.errorCode = payload.Error.Code
				}
			}
			results <- res
		}()
	}

	close(start)

	for i := 0; i < limit; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for admitted parse call %d", i+1)
		}
	}

	nonAdmitted := totalCalls - limit
	busyCount := 0
	for i := 0; i < nonAdmitted; i++ {
		select {
		case res := <-results:
			if res.status != http.StatusServiceUnavailable {
				t.Fatalf("expected queued overflow request to return 503, got %d", res.status)
			}
			if res.errorCode != "server_busy" {
				t.Fatalf("expected error.code=server_busy, got %q", res.errorCode)
			}
			busyCount++
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for overflow request result %d", i+1)
		}
	}

	if got := int(atomic.LoadInt32(&peakInFlight)); got > limit {
		t.Fatalf("peak parser in-flight calls exceeded limit: got %d want <= %d", got, limit)
	}
	if busyCount != nonAdmitted {
		t.Fatalf("expected %d server_busy responses, got %d", nonAdmitted, busyCount)
	}

	close(release)

	admittedOK := 0
	for admittedOK < limit {
		select {
		case res := <-results:
			if res.status != http.StatusOK {
				t.Fatalf("expected admitted request to complete with 200, got %d", res.status)
			}
			admittedOK++
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for released admitted requests")
		}
	}

	wg.Wait()

	postReleaseReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	postReleaseReq.Header.Set("Content-Type", "application/json")
	postReleaseW := httptest.NewRecorder()
	mux.ServeHTTP(postReleaseW, postReleaseReq)
	if postReleaseW.Code != http.StatusOK {
		t.Fatalf("expected handler to remain responsive after release, got %d", postReleaseW.Code)
	}
}

func TestAnalyze_ParserConcurrencyLimit_RuntimeUpdates_DoNotCreateParallelLimiters(t *testing.T) {
	const (
		limit      = 2
		totalCalls = 80
	)

	var inFlight int32
	var peakInFlight int32

	mockP := &mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			peak := atomic.LoadInt32(&peakInFlight)
			if current <= peak || atomic.CompareAndSwapInt32(&peakInFlight, peak, current) {
				break
			}
		}

		time.Sleep(25 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		return &model.Diagram{}, nil, nil
	}}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(limit)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})

	start := make(chan struct{})
	var requestsWG sync.WaitGroup
	for i := 0; i < totalCalls; i++ {
		requestsWG.Add(1)
		go func() {
			defer requestsWG.Done()
			<-start

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
				t.Errorf("expected 200 or 503 under contention, got %d", w.Code)
			}
		}()
	}

	updatesDone := make(chan struct{})
	go func() {
		defer close(updatesDone)
		<-start
		for i := 0; i < 200; i++ {
			h.SetParserConcurrencyLimit(limit)
		}
	}()

	close(start)
	requestsWG.Wait()
	<-updatesDone

	if got := int(atomic.LoadInt32(&peakInFlight)); got > limit {
		t.Fatalf("peak parser in-flight calls exceeded limit during runtime updates: got %d want <= %d", got, limit)
	}
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

func TestRateLimiterCheck_ConcurrentDecisionMetadata(t *testing.T) {
	const limit = 25
	limiter := api.NewRateLimiter(limit, time.Hour)
	const total = 300

	type decision struct {
		allowed   bool
		remaining int
		resetUnix int64
		limit     int
	}

	decisions := make([]decision, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			allowed, remaining, resetUnix, reportedLimit := limiter.Check("127.0.0.1")
			decisions[i] = decision{allowed: allowed, remaining: remaining, resetUnix: resetUnix, limit: reportedLimit}
		}(i)
	}
	wg.Wait()

	allowedCount := 0
	var baselineReset int64
	seenRemaining := make(map[int]int, limit)
	for i, d := range decisions {
		if d.limit != limit {
			t.Fatalf("decision %d expected limit %d, got %d", i, limit, d.limit)
		}
		if d.remaining < 0 {
			t.Fatalf("decision %d returned negative remaining %d", i, d.remaining)
		}
		if d.remaining > limit-1 {
			t.Fatalf("decision %d returned remaining %d above maximum %d", i, d.remaining, limit-1)
		}
		if baselineReset == 0 {
			baselineReset = d.resetUnix
		} else if d.resetUnix != baselineReset {
			t.Fatalf("expected stable reset in one window, got %d and %d", baselineReset, d.resetUnix)
		}
		if d.allowed {
			allowedCount++
			seenRemaining[d.remaining]++
		} else if d.remaining != 0 {
			t.Fatalf("denied decision should return remaining=0, got %d", d.remaining)
		}
	}

	if allowedCount != limit {
		t.Fatalf("expected exactly %d allowed decisions, got %d", limit, allowedCount)
	}
	for want := 0; want < limit; want++ {
		if seenRemaining[want] != 1 {
			t.Fatalf("expected remaining=%d exactly once across allowed decisions, got %d", want, seenRemaining[want])
		}
	}
}

func TestAnalyzeRateLimitMiddleware_ConcurrentHeadersRemainNonNegativeAndMonotonic(t *testing.T) {
	const limit = 20
	const total = 150

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	limited := api.AnalyzeRateLimitMiddleware(api.NewRateLimiter(limit, time.Hour), mux)
	body := []byte(`{"code":"graph TD\n  A-->B"}`)

	type headerResult struct {
		status    int
		remaining int
		resetUnix int64
		limit     int
	}

	results := make([]headerResult, total)
	errs := make([]error, total)
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			limited.ServeHTTP(w, req)

			remaining, err := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
			if err != nil {
				errs[i] = fmt.Errorf("failed to parse X-RateLimit-Remaining: %v", err)
				return
			}
			resetUnix, err := strconv.ParseInt(w.Header().Get("X-RateLimit-Reset"), 10, 64)
			if err != nil {
				errs[i] = fmt.Errorf("failed to parse X-RateLimit-Reset: %v", err)
				return
			}
			reportedLimit, err := strconv.Atoi(w.Header().Get("X-RateLimit-Limit"))
			if err != nil {
				errs[i] = fmt.Errorf("failed to parse X-RateLimit-Limit: %v", err)
				return
			}

			results[i] = headerResult{status: w.Code, remaining: remaining, resetUnix: resetUnix, limit: reportedLimit}
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	allowedCount := 0
	seenRemaining := make(map[int]int, limit)
	var baselineReset int64
	for i, r := range results {
		if r.limit != limit {
			t.Fatalf("result %d expected limit header %d, got %d", i, limit, r.limit)
		}
		if r.remaining < 0 {
			t.Fatalf("result %d had negative remaining header %d", i, r.remaining)
		}
		if baselineReset == 0 {
			baselineReset = r.resetUnix
		} else if r.resetUnix != baselineReset {
			t.Fatalf("expected one reset timestamp in one window, got %d and %d", baselineReset, r.resetUnix)
		}

		if r.status == http.StatusOK {
			allowedCount++
			seenRemaining[r.remaining]++
			if r.remaining > limit-1 {
				t.Fatalf("allowed result %d has remaining %d above max %d", i, r.remaining, limit-1)
			}
			continue
		}
		if r.status != http.StatusTooManyRequests {
			t.Fatalf("unexpected status %d for result %d", r.status, i)
		}
		if r.remaining != 0 {
			t.Fatalf("denied result %d should have remaining=0, got %d", i, r.remaining)
		}
	}

	if allowedCount != limit {
		t.Fatalf("expected %d successful requests, got %d", limit, allowedCount)
	}
	for want := 0; want < limit; want++ {
		if seenRemaining[want] != 1 {
			t.Fatalf("expected allowed headers to include remaining=%d exactly once, got %d", want, seenRemaining[want])
		}
	}
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
	if resp.Schema == nil {
		t.Fatal("expected schema object")
	}

	if got := resp.Schema["$schema"]; got != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("expected draft schema id marker, got %#v", got)
	}
	if _, ok := resp.Schema["title"].(string); !ok {
		t.Fatalf("expected title marker, got %#v", resp.Schema["title"])
	}
	if _, ok := resp.Schema["oneOf"].([]any); !ok {
		t.Fatalf("expected oneOf marker, got %#v", resp.Schema["oneOf"])
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
			State               string                 `json:"state"`
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
		if rule.ID == "" || rule.State == "" || rule.Severity == "" || rule.Description == "" {
			t.Fatalf("expected id/state/severity/description for each rule, got %#v", rule)
		}
		if rule.State != "implemented" && rule.State != "planned" {
			t.Fatalf("expected rule state to be implemented|planned, got %q for %s", rule.State, rule.ID)
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
			State               string                 `json:"state"`
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

	metadata := rules.ListRuleMetadata()
	registry := map[string]rules.RuleMetadata{}
	for _, meta := range metadata {
		registry[meta.ID] = meta
	}
	if len(resp.Rules) != len(registry) {
		t.Fatalf("expected %d rules from /rules endpoint, got %d", len(registry), len(resp.Rules))
	}
	for _, rule := range resp.Rules {
		meta, ok := registry[rule.ID]
		if !ok {
			t.Fatalf("unexpected rule id %q in /rules response", rule.ID)
		}
		if rule.State != meta.State {
			t.Fatalf("expected state %q for %s, got %q", meta.State, rule.ID, rule.State)
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

func TestAnalyze_SequenceDiagramSupportedWhenRulesRegistered(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeSequence}, nil, nil
	}}, engine.NewWithRules(sequenceProbeRule{}))
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"code": "sequenceDiagram\nAlice->>Bob: Hi"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || !lintSupported {
		t.Fatalf("expected lint-supported=true, got %#v", resp["lint-supported"])
	}
	if _, hasError := resp["error"]; hasError {
		t.Fatalf("did not expect error payload, got %#v", resp["error"])
	}
	issues, ok := resp["issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("expected one issue from sequence rule, got %#v", resp["issues"])
	}
	issue, ok := issues[0].(map[string]any)
	if !ok || issue["rule-id"] != "sequence-probe" {
		t.Fatalf("expected sequence-probe issue, got %#v", issues[0])
	}
}

func TestAnalyzeSARIF_SequenceDiagramSupportedWhenRulesRegistered(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeSequence}, nil, nil
	}}, engine.NewWithRules(sequenceProbeRule{}))
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"code": "sequenceDiagram\nAlice->>Bob: Hi"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/sarif+json") {
		t.Fatalf("expected SARIF content type, got %q", ct)
	}

	var sarifResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &sarifResp); err != nil {
		t.Fatalf("decode sarif: %v", err)
	}
	runs, ok := sarifResp["runs"].([]any)
	if !ok || len(runs) == 0 {
		t.Fatalf("expected runs in SARIF response, got %#v", sarifResp["runs"])
	}
	results, ok := runs[0].(map[string]any)["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected one SARIF result from sequence rule, got %#v", runs[0].(map[string]any)["results"])
	}
	result := results[0].(map[string]any)
	if result["ruleId"] != "sequence-probe" {
		t.Fatalf("expected sequence-probe SARIF result, got %#v", result["ruleId"])
	}
}

func TestAnalyzeSARIF_ReturnsSARIFForValidAnalysis(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	}}, engine.NewWithRules(sarifProbeRule{}))
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"code": "graph TD\nA-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/sarif+json") {
		t.Fatalf("expected SARIF content type, got %q", ct)
	}

	var sarifResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &sarifResp); err != nil {
		t.Fatalf("decode sarif: %v", err)
	}
	if sarifResp["version"] != "2.1.0" {
		t.Fatalf("unexpected SARIF version: %#v", sarifResp["version"])
	}
	runs := sarifResp["runs"].([]any)
	results := runs[0].(map[string]any)["results"].([]any)
	if len(results) == 0 {
		t.Fatal("expected non-empty sarif results")
	}
	first := results[0].(map[string]any)
	if first["ruleId"] == "" {
		t.Fatalf("expected ruleId propagation in SARIF result: %#v", first)
	}
	if first["ruleId"] != "sarif-probe" {
		t.Fatalf("expected ruleId to remain unchanged, got %#v", first["ruleId"])
	}
	if first["level"] != "warning" {
		t.Fatalf("expected warning severity mapping, got %#v", first["level"])
	}
	msg := first["message"].(map[string]any)
	if msg["text"] != "probe" {
		t.Fatalf("expected message to remain unchanged, got %#v", msg["text"])
	}
	fps, ok := first["partialFingerprints"].(map[string]any)
	if !ok {
		t.Fatalf("expected partialFingerprints in SARIF result: %#v", first)
	}
	if fps["issueFingerprint"] == "" {
		t.Fatalf("expected fingerprint propagation in SARIF result, got %#v", fps)
	}
	locs, ok := first["locations"].([]any)
	if ok && len(locs) > 0 {
		region := locs[0].(map[string]any)["physicalLocation"].(map[string]any)["region"].(map[string]any)
		if region["startLine"] == nil {
			t.Fatalf("expected startLine mapping in SARIF region: %#v", region)
		}
	}
}

func TestAnalyzeSARIF_SeverityMapping(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	}}, engine.NewWithRules(severityProbeRule{}))
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]any{"code": "graph TD\nA-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var sarifResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &sarifResp); err != nil {
		t.Fatalf("decode sarif: %v", err)
	}
	results := sarifResp["runs"].([]any)[0].(map[string]any)["results"].([]any)
	seen := map[string]struct{}{}
	for _, item := range results {
		level := item.(map[string]any)["level"].(string)
		seen[level] = struct{}{}
	}
	for _, want := range []string{"error", "warning", "note"} {
		if _, ok := seen[want]; !ok {
			t.Fatalf("expected to see level %q, got %#v", want, seen)
		}
	}
}

func TestAnalyzeSARIF_NilURLDoesNotPanic(t *testing.T) {
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	}}, engine.NewWithRules(sarifProbeRule{}))

	body, _ := json.Marshal(map[string]any{"code": "graph TD\nA-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	req.URL = nil
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AnalyzeSARIF(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/sarif+json") {
		t.Fatalf("expected SARIF content type, got %q", ct)
	}
}

type sequenceProbeRule struct{}

func (sequenceProbeRule) ID() string { return "sequence-probe" }

func (sequenceProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilySequence}
}

func (sequenceProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	return []model.Issue{{RuleID: "sequence-probe", Severity: "warning", Message: "sequence probe"}}
}

type sarifProbeRule struct{}

func (sarifProbeRule) ID() string { return "sarif-probe" }

func (sarifProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}
func (sarifProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line, col := 4, 9
	return []model.Issue{{RuleID: "sarif-probe", Severity: "warning", Message: "probe", Line: &line, Column: &col}}
}

type severityProbeRule struct{}

func (severityProbeRule) ID() string { return "severity-probe" }

func (severityProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}
func (severityProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	return []model.Issue{
		{RuleID: "r1", Severity: "error", Message: "e"},
		{RuleID: "r2", Severity: "warning", Message: "w"},
		{RuleID: "r3", Severity: "info", Message: "i"},
	}
}

func TestAnalyze_ConfigSuppressionSelectors_ContractBehavior(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		lineA := 2
		lineD := 5
		return &model.Diagram{
			Type: model.DiagramTypeFlowchart,
			Nodes: []model.Node{
				{ID: "A", Line: &lineA}, {ID: "B"}, {ID: "C"},
				{ID: "D", Line: &lineD}, {ID: "E"}, {ID: "F"},
			},
			Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "D", To: "E"}, {From: "D", To: "F"}},
		}, nil, nil
	})

	tests := []struct {
		name            string
		selectors       []string
		wantMaxFanoutAt []float64
	}{
		{name: "no selectors includes all findings", selectors: nil, wantMaxFanoutAt: []float64{2, 5}},
		{name: "node selector excludes matching finding", selectors: []string{"node:A"}, wantMaxFanoutAt: []float64{5}},
		{name: "rule selector excludes all findings for the rule", selectors: []string{"rule:max-fanout"}, wantMaxFanoutAt: nil},
		{name: "negation re-includes excluded finding", selectors: []string{"rule:max-fanout", "!node:D"}, wantMaxFanoutAt: []float64{5}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ruleConfig := map[string]any{"limit": 1}
			if tc.selectors != nil {
				ruleConfig["suppression-selectors"] = tc.selectors
			}

			body, _ := json.Marshal(map[string]any{
				"code": "graph TD\nA-->B\nA-->C\nD-->E\nD-->F",
				"config": map[string]any{
					"schema-version": "v1",
					"rules":          map[string]any{"max-fanout": ruleConfig},
				},
			})

			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			issues, ok := resp["issues"].([]any)
			if !ok {
				t.Fatalf("expected issues array, got %T", resp["issues"])
			}

			var gotMaxFanoutAt []float64
			for _, rawIssue := range issues {
				issue, ok := rawIssue.(map[string]any)
				if !ok || issue["rule-id"] != "max-fanout" {
					continue
				}
				if line, ok := issue["line"].(float64); ok {
					gotMaxFanoutAt = append(gotMaxFanoutAt, line)
				}
			}

			if !reflect.DeepEqual(gotMaxFanoutAt, tc.wantMaxFanoutAt) {
				t.Fatalf("unexpected max-fanout lines: got %v want %v (issues=%#v)", gotMaxFanoutAt, tc.wantMaxFanoutAt, issues)
			}
		})
	}
}

func TestAnalyze_MetricsIssueCountsReflectUnsuppressedIssuesOnly(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		lineA := 2
		lineD := 3
		return &model.Diagram{
			Type: model.DiagramTypeFlowchart,
			Nodes: []model.Node{
				{ID: "A", Line: &lineA}, {ID: "B"}, {ID: "C"},
				{ID: "D", Line: &lineD}, {ID: "E"}, {ID: "F"},
			},
			Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "D", To: "E"}, {From: "D", To: "F"}},
		}, nil, nil
	})

	body, _ := json.Marshal(map[string]any{
		"code": "graph TD\nA-->B\nA-->C\nD-->E\nD-->F",
		"config": map[string]any{
			"schema-version": "v1",
			"rules": map[string]any{
				"max-fanout": map[string]any{
					"limit":                 1,
					"suppression-selectors": []string{"node:A"},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	issues, ok := resp["issues"].([]any)
	if !ok {
		t.Fatalf("expected issues array, got %T", resp["issues"])
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly one unsuppressed issue, got %d (%#v)", len(issues), issues)
	}

	metrics, ok := resp["metrics"].(map[string]any)
	if !ok {
		t.Fatalf("expected metrics object, got %T", resp["metrics"])
	}
	issueCounts, ok := metrics["issue-counts"].(map[string]any)
	if !ok {
		t.Fatalf("expected issue-counts object, got %T", metrics["issue-counts"])
	}

	bySeverity, ok := issueCounts["by-severity"].(map[string]any)
	if !ok {
		t.Fatalf("expected by-severity object, got %T", issueCounts["by-severity"])
	}
	if bySeverity["warning"] != float64(1) {
		t.Fatalf("expected by-severity.warning=1 after suppression, got %v", bySeverity["warning"])
	}

	byRule, ok := issueCounts["by-rule"].(map[string]any)
	if !ok {
		t.Fatalf("expected by-rule object, got %T", issueCounts["by-rule"])
	}
	if byRule["max-fanout"] != float64(1) {
		t.Fatalf("expected by-rule.max-fanout=1 after suppression, got %v", byRule["max-fanout"])
	}
}

func TestAnalyze_ConfigSuppressionSelectors_MalformedRejected(t *testing.T) {
	t.Run("rejects malformed selectors", func(t *testing.T) {
		mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
			return &model.Diagram{
				Type:  model.DiagramTypeFlowchart,
				Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
				Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
			}, nil, nil
		})

		body, _ := json.Marshal(map[string]any{
			"code": "graph TD\nA-->B\nA-->C",
			"config": map[string]any{
				"schema-version": "v1",
				"rules": map[string]any{
					"max-fanout": map[string]any{
						"limit":                 1,
						"suppression-selectors": []string{"node", "subgraph:"},
					},
				},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
		assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for suppression-selectors", "config.rules.max-fanout.suppression-selectors", nil)
	})

	t.Run("rejects unknown selector prefixes", func(t *testing.T) {
		mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
			return &model.Diagram{
				Type:  model.DiagramTypeFlowchart,
				Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
				Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
			}, nil, nil
		})

		body, _ := json.Marshal(map[string]any{
			"code": "graph TD\nA-->B\nA-->C",
			"config": map[string]any{
				"schema-version": "v1",
				"rules": map[string]any{
					"max-fanout": map[string]any{
						"limit":                 1,
						"suppression-selectors": []string{"unknown:A"},
					},
				},
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
		}
		assertValidationErrorResponse(t, w.Body.Bytes(), "invalid_option", "invalid option value for suppression-selectors", "config.rules.max-fanout.suppression-selectors", nil)
	})
}

func TestAnalyze_RequestIDHeaderPropagation(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})
	handler := api.RequestIDMiddleware(mux)

	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "req-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-Id"); got != "req-123" {
		t.Fatalf("expected propagated request id, got %q", got)
	}
}

func TestRegisterRoutes_V1CanonicalAndLegacyAliases(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	for _, tc := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodPost, path: "/v1/analyze", body: `{"code":"graph TD;A-->B"}`, want: http.StatusOK},
		{method: http.MethodPost, path: "/analyze", body: `{"code":"graph TD;A-->B"}`, want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/rules", want: http.StatusOK},
		{method: http.MethodGet, path: "/rules", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/rules/schema", want: http.StatusOK},
		{method: http.MethodGet, path: "/rules/schema", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/spec", want: http.StatusOK},
		{method: http.MethodGet, path: "/spec", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/docs", want: http.StatusOK},
		{method: http.MethodGet, path: "/docs", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/benchmark.html", want: http.StatusNotFound},
		{method: http.MethodGet, path: "/benchmark.html", want: http.StatusNotFound},
		{method: http.MethodGet, path: "/v1/healthz", want: http.StatusOK},
		{method: http.MethodGet, path: "/healthz", want: http.StatusOK},
		{method: http.MethodGet, path: "/", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/ready", want: http.StatusOK},
		{method: http.MethodGet, path: "/ready", want: http.StatusOK},
		{method: http.MethodGet, path: "/v1/version", want: http.StatusOK},
		{method: http.MethodGet, path: "/version", want: http.StatusOK},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var reqBody *strings.Reader
			if tc.body != "" {
				reqBody = strings.NewReader(tc.body)
			} else {
				reqBody = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, reqBody)
			if tc.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d body=%s", tc.want, w.Code, w.Body.String())
			}
		})
	}

}

func TestServeBenchmark_Returns200WhenFileExists(t *testing.T) {
	t.Setenv("MERM8_BENCHMARK_HTML_PATH", filepath.Join(t.TempDir(), "benchmark.html"))

	benchmarkPath := os.Getenv("MERM8_BENCHMARK_HTML_PATH")
	if err := os.WriteFile(benchmarkPath, []byte("<html><body>benchmark ok</body></html>"), 0o644); err != nil {
		t.Fatalf("failed to write benchmark html fixture: %v", err)
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/benchmark.html", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html content type, got %q", got)
	}
	if got := w.Header().Get("X-Merm8-Benchmark-Status"); got != "generated" {
		t.Fatalf("expected generated benchmark status header, got %q", got)
	}
	if got := w.Body.String(); got != "<html><body>benchmark ok</body></html>" {
		t.Fatalf("unexpected benchmark body: %q", got)
	}
}

func TestServeBenchmark_ReturnsPlaceholderStatusForPlaceholderContent(t *testing.T) {
	t.Setenv("MERM8_BENCHMARK_HTML_PATH", filepath.Join(t.TempDir(), "benchmark.html"))

	benchmarkPath := os.Getenv("MERM8_BENCHMARK_HTML_PATH")
	placeholder := "<html><body>benchmark.html was not pre-generated for this deployment</body></html>"
	if err := os.WriteFile(benchmarkPath, []byte(placeholder), 0o644); err != nil {
		t.Fatalf("failed to write benchmark html fixture: %v", err)
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/benchmark.html", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Merm8-Benchmark-Status"); got != "placeholder" {
		t.Fatalf("expected placeholder benchmark status header, got %q", got)
	}
}

func TestServeBenchmark_Returns404WhenFileMissing(t *testing.T) {
	t.Setenv("MERM8_BENCHMARK_HTML_PATH", filepath.Join(t.TempDir(), "missing-benchmark.html"))

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/benchmark.html", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON response body, got decode error: %v", err)
	}
	if payload["error"] == "" {
		t.Fatalf("expected non-empty error message, got payload=%v", payload)
	}
}

func TestServeBenchmark_DoesNotInterfereWithHealthEndpoints(t *testing.T) {
	t.Setenv("MERM8_BENCHMARK_HTML_PATH", filepath.Join(t.TempDir(), "missing-benchmark.html"))

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	healthReq := httptest.NewRequest(http.MethodGet, "/", nil)
	healthW := httptest.NewRecorder()
	mux.ServeHTTP(healthW, healthReq)
	if healthW.Code != http.StatusOK {
		t.Fatalf("expected GET / to remain healthy with 200, got %d", healthW.Code)
	}

	healthzReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzW := httptest.NewRecorder()
	mux.ServeHTTP(healthzW, healthzReq)
	if healthzW.Code != http.StatusOK {
		t.Fatalf("expected GET /healthz to remain healthy with 200, got %d", healthzW.Code)
	}

	benchmarkReq := httptest.NewRequest(http.MethodGet, "/benchmark.html", nil)
	benchmarkW := httptest.NewRecorder()
	mux.ServeHTTP(benchmarkW, benchmarkReq)
	if benchmarkW.Code != http.StatusNotFound {
		t.Fatalf("expected benchmark endpoint 404 for missing file, got %d", benchmarkW.Code)
	}
}

func TestLegacyAnalyzeAliases_WithLegacyConfigEmitsDeprecationHeaders(t *testing.T) {
	mux, h := newTestMuxWithHandler(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})
	h.SetStrictConfigSchema(false)

	// Test with legacy snake_case config format (should emit Deprecation headers)
	legacyConfigTests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "analyze with legacy config", method: http.MethodPost, path: "/analyze", body: `{"code":"graph TD;A-->B","config":{"schema_version":"v1","rules":{}}}`},
		{name: "analyze sarif with legacy config", method: http.MethodPost, path: "/analyze/sarif", body: `{"code":"graph TD;A-->B","config":{"schema_version":"v1","rules":{}}}`},
	}

	for _, tc := range legacyConfigTests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Legacy config format should emit Deprecation header
			if got := w.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("expected Deprecation header true for legacy config on %s, got %q", tc.path, got)
			}
			if got := w.Header().Get("Warning"); got == "" {
				t.Fatalf("expected Warning header to be present for legacy config on %s", tc.path)
			}
		})
	}

	// Test with canonical config format (should NOT emit Deprecation headers)
	canonicalConfigTests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "analyze with canonical config", method: http.MethodPost, path: "/analyze", body: `{"code":"graph TD;A-->B","config":{"schema-version":"v1","rules":{}}}`},
		{name: "analyze sarif with canonical config", method: http.MethodPost, path: "/analyze/sarif", body: `{"code":"graph TD;A-->B","config":{"schema-version":"v1","rules":{}}}`},
	}

	for _, tc := range canonicalConfigTests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// Canonical config format should NOT emit Deprecation header
			if got := w.Header().Get("Deprecation"); got != "" {
				t.Fatalf("expected no Deprecation header for canonical config on %s, got %q", tc.path, got)
			}
		})
	}

	// Test /analyze/raw and /analyze/help with no config (should NOT emit Deprecation headers)
	noConfigTests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "analyze raw", method: http.MethodPost, path: "/analyze/raw", body: `graph TD
A-->B`},
		{name: "analyze help", method: http.MethodGet, path: "/analyze/help"},
	}

	for _, tc := range noConfigTests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.method == http.MethodPost {
				req.Header.Set("Content-Type", "text/plain")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			// No config = no deprecation warnings
			if got := w.Header().Get("Deprecation"); got != "" {
				t.Fatalf("expected no Deprecation header for %s, got %q", tc.path, got)
			}
		})
	}
}

func TestCanonicalAnalyzeRoutes_DoNotEmitLegacyDeprecationHeaders(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "v1 analyze", method: http.MethodPost, path: "/v1/analyze", body: `{"code":"graph TD;A-->B"}`},
		{name: "v1 analyze raw", method: http.MethodPost, path: "/v1/analyze/raw", body: `graph TD
A-->B`},
		{name: "v1 analyze sarif", method: http.MethodPost, path: "/v1/analyze/sarif", body: `{"code":"graph TD;A-->B"}`},
		{name: "v1 analyze help", method: http.MethodGet, path: "/v1/analyze/help"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.method == http.MethodPost && tc.path != "/v1/analyze/raw" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got := w.Header().Get("Sunset"); got != "" {
				t.Fatalf("expected no Sunset header on canonical routes, got %q", got)
			}
			if got := w.Header().Get("Link"); got != "" {
				t.Fatalf("expected no Link header on canonical routes, got %q", got)
			}
		})
	}
}

func TestV1AnalyseAliases_Integration_EmitDeprecationWarningAndMatchCanonicalResponse(t *testing.T) {
	mux := newTestMuxWithRealParser(t, getParserScriptPath(t))

	tests := []struct {
		name          string
		canonicalPath string
		aliasPath     string
		body          string
		contentType   string
	}{
		{name: "json analyze", canonicalPath: "/v1/analyze", aliasPath: "/v1/analyse", body: `{"code":"graph TD;A-->B"}`, contentType: "application/json"},
		{name: "raw analyze", canonicalPath: "/v1/analyze/raw", aliasPath: "/v1/analyse/raw", body: "graph TD\nA-->B", contentType: "text/plain"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canonicalReq := httptest.NewRequest(http.MethodPost, tc.canonicalPath, strings.NewReader(tc.body))
			canonicalReq.Header.Set("Content-Type", tc.contentType)
			canonicalW := httptest.NewRecorder()
			mux.ServeHTTP(canonicalW, canonicalReq)

			if canonicalW.Code != http.StatusOK {
				t.Fatalf("canonical route expected 200, got %d body=%s", canonicalW.Code, canonicalW.Body.String())
			}

			aliasReq := httptest.NewRequest(http.MethodPost, tc.aliasPath, strings.NewReader(tc.body))
			aliasReq.Header.Set("Content-Type", tc.contentType)
			aliasW := httptest.NewRecorder()
			mux.ServeHTTP(aliasW, aliasReq)

			if aliasW.Code != http.StatusOK {
				t.Fatalf("alias route expected 200, got %d body=%s", aliasW.Code, aliasW.Body.String())
			}
			var canonicalResp map[string]any
			if err := json.Unmarshal(canonicalW.Body.Bytes(), &canonicalResp); err != nil {
				t.Fatalf("failed to decode canonical response: %v", err)
			}
			var aliasResp map[string]any
			if err := json.Unmarshal(aliasW.Body.Bytes(), &aliasResp); err != nil {
				t.Fatalf("failed to decode alias response: %v", err)
			}
			delete(canonicalResp, "timestamp")
			delete(aliasResp, "timestamp")
			if !reflect.DeepEqual(canonicalResp, aliasResp) {
				t.Fatalf("expected alias response body to match canonical body (excluding timestamp)\ncanonical=%s\nalias=%s", canonicalW.Body.String(), aliasW.Body.String())
			}
			if got := canonicalW.Header().Get("Warning"); got != "" {
				t.Fatalf("expected no Warning header on canonical route, got %q", got)
			}
			if got := aliasW.Header().Get("Warning"); got == "" {
				t.Fatal("expected Warning header on /v1/analyse* alias route")
			}
			if got := aliasW.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("expected Deprecation=true on alias route, got %q", got)
			}
		})
	}
}

func TestAnalyzeBearerAuthMiddleware_RequiresTokenOnProtectedAnalyzeRoutes(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	secured := api.AnalyzeBearerAuthMiddleware("s3cr3t", mux)

	for _, path := range []string{"/analyze/raw", "/v1/analyze/raw", "/v1/analyse/raw", "/analyze/sarif", "/v1/analyze/sarif", "/v1/analyse"} {
		t.Run(path, func(t *testing.T) {
			var body string
			contentType := "application/json"
			if strings.Contains(path, "/raw") {
				body = "graph TD\nA-->B"
				contentType = "text/plain"
			} else {
				body = `{"code":"graph TD;A-->B"}`
			}

			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			w := httptest.NewRecorder()
			secured.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401 when token is missing for %s, got %d", path, w.Code)
			}
		})
	}
}

func TestAnalyzeRateLimitMiddleware_AppliesOnProtectedAnalyzeRoutes(t *testing.T) {
	for _, path := range []string{"/analyze/raw", "/v1/analyze/raw", "/v1/analyse/raw", "/analyze/sarif", "/v1/analyze/sarif", "/v1/analyse"} {
		t.Run(path, func(t *testing.T) {
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return &model.Diagram{}, nil, nil
			})
			limited := api.AnalyzeRateLimitMiddleware(api.NewRateLimiter(1, time.Minute), mux)

			var body string
			contentType := "application/json"
			if strings.Contains(path, "/raw") {
				body = "graph TD\nA-->B"
				contentType = "text/plain"
			} else {
				body = `{"code":"graph TD;A-->B"}`
			}

			firstReq := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			firstReq.Header.Set("Content-Type", contentType)
			firstReq.RemoteAddr = "127.0.0.1:1234"
			firstW := httptest.NewRecorder()
			limited.ServeHTTP(firstW, firstReq)

			if firstW.Code != http.StatusOK {
				t.Fatalf("expected first request to pass for %s, got %d", path, firstW.Code)
			}

			secondReq := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			secondReq.Header.Set("Content-Type", contentType)
			secondReq.RemoteAddr = "127.0.0.1:5678"
			secondW := httptest.NewRecorder()
			limited.ServeHTTP(secondW, secondReq)

			if secondW.Code != http.StatusTooManyRequests {
				t.Fatalf("expected 429 when request rate is exceeded for %s, got %d", path, secondW.Code)
			}
		})
	}
}

func TestAnalyzeBearerAuthMiddleware_DoesNotProtectAnalyzeHelpRoute(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	secured := api.AnalyzeBearerAuthMiddleware("s3cr3t", mux)

	req := httptest.NewRequest(http.MethodPost, "/analyze/help", nil)
	w := httptest.NewRecorder()
	secured.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected help route to remain unprotected and return 405 for unsupported POST, got %d", w.Code)
	}
}

func TestAnalyzeRateLimitMiddleware_DoesNotProtectAnalyzeHelpRoute(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	limited := api.AnalyzeRateLimitMiddleware(api.NewRateLimiter(1, time.Minute), mux)

	firstReq := httptest.NewRequest(http.MethodPost, "/analyze/help", nil)
	firstReq.RemoteAddr = "127.0.0.1:1234"
	firstW := httptest.NewRecorder()
	limited.ServeHTTP(firstW, firstReq)

	if firstW.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected help route to remain unprotected and return 405 for unsupported POST, got %d", firstW.Code)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/analyze/help", nil)
	secondReq.RemoteAddr = "127.0.0.1:5678"
	secondW := httptest.NewRecorder()
	limited.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected help route to remain unprotected and return 405 for unsupported POST, got %d", secondW.Code)
	}
}

func TestAnalyzeBearerAuthMiddleware_RequiresTokenOnV1Endpoint(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	secured := api.AnalyzeBearerAuthMiddleware("s3cr3t", mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	secured.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when token is missing, got %d", w.Code)
	}
}

func TestAnalyzeRateLimitMiddleware_AppliesOnV1Endpoint(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{}, nil, nil
	})
	limited := api.AnalyzeRateLimitMiddleware(api.NewRateLimiter(1, time.Minute), mux)

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	firstReq.RemoteAddr = "127.0.0.1:1234"
	firstW := httptest.NewRecorder()
	limited.ServeHTTP(firstW, firstReq)

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	secondReq.Header.Set("Content-Type", "application/json")
	secondReq.RemoteAddr = "127.0.0.1:5678"
	secondW := httptest.NewRecorder()
	limited.ServeHTTP(secondW, secondReq)

	if secondW.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 when request rate is exceeded, got %d", secondW.Code)
	}
}

func TestRuleAdvertisement_OnlyImplementedRulesExposedAndConfigurable(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	}}, engine.NewWithRules(rules.NoDuplicateNodeIDs{}, rules.MaxFanout{}))
	h.RegisterRoutes(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/rules", nil)
	listW := httptest.NewRecorder()
	mux.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200 from /rules, got %d", listW.Code)
	}

	var listResp struct {
		Rules []struct {
			ID string `json:"id"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode /rules response: %v", err)
	}

	got := map[string]struct{}{}
	for _, rule := range listResp.Rules {
		got[rule.ID] = struct{}{}
	}
	for _, id := range []string{"no-duplicate-node-ids", "max-fanout"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("expected implemented rule %q in /rules response", id)
		}
	}
	for _, id := range []string{"no-cycles", "max-depth", "no-disconnected-nodes", "class-no-orphan-classes", "er-no-isolated-entities", "sequence-max-participants", "state-no-unreachable-states"} {
		if _, ok := got[id]; !ok {
			t.Fatalf("expected metadata rule %q in /rules response", id)
		}
	}

	schemaReq := httptest.NewRequest(http.MethodGet, "/rules/schema", nil)
	schemaW := httptest.NewRecorder()
	mux.ServeHTTP(schemaW, schemaReq)
	if schemaW.Code != http.StatusOK {
		t.Fatalf("expected 200 from /rules/schema, got %d", schemaW.Code)
	}
	var schemaResp struct {
		Schema map[string]any `json:"schema"`
	}
	if err := json.Unmarshal(schemaW.Body.Bytes(), &schemaResp); err != nil {
		t.Fatalf("failed to decode /rules/schema response: %v", err)
	}
	oneOf := schemaResp.Schema["oneOf"].([]any)
	flat := oneOf[0].(map[string]any)
	ruleProps := flat["properties"].(map[string]any)
	if _, ok := ruleProps["max-fanout"]; !ok {
		t.Fatal("expected max-fanout in schema properties")
	}
	if _, ok := ruleProps["no-cycles"]; ok {
		t.Fatal("did not expect no-cycles in schema properties")
	}

	body, _ := json.Marshal(map[string]any{
		"code": "flowchart TD\nA-->B",
		"config": map[string]any{
			"schema-version": "v1",
			"rules": map[string]any{
				"no-cycles": map[string]any{"enabled": false},
			},
		},
	})
	analyzeReq := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	analyzeReq.Header.Set("Content-Type", "application/json")
	analyzeW := httptest.NewRecorder()
	mux.ServeHTTP(analyzeW, analyzeReq)
	// With graceful degradation, unimplemented rules return 200 with warnings
	if analyzeW.Code != http.StatusOK {
		t.Fatalf("expected 200 for graceful degradation of unimplemented rule, got %d body=%s", analyzeW.Code, analyzeW.Body.String())
	}

	// Verify warnings about unknown rule are present
	var resp map[string]any
	if err := json.Unmarshal(analyzeW.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	warnings, ok := resp["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Errorf("expected warnings array for unimplemented rule no-cycles, got: %#v", resp)
	}
}

func TestAnalyze_IntegrationParserTimeoutErrorDetails(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-timeout.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {}, 10000);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 1 * time.Second, NodeMaxOldSpaceMB: 512})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusGatewayTimeout)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "parser_timeout" {
		t.Fatalf("error.code=%v want parser_timeout", errObj["code"])
	}
	details := errObj["details"].(map[string]any)
	if _, ok := details["suggestion"]; !ok {
		t.Fatalf("expected suggestion in error.details")
	}
	if details["limit"] != "1s" {
		t.Fatalf("limit=%v want 1s", details["limit"])
	}
}

func TestAnalyze_IntegrationParserMemoryLimitErrorDetails(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-memory.mjs")
	scriptBody := `#!/usr/bin/env node
process.stderr.write("FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\\n");
process.stdout.write(JSON.stringify({valid:false,error:{message:"internal parser error: oom",line:0,column:0}}));
process.exit(1);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 5 * time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	code := "graph TD; A-->B"
	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusInternalServerError)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "parser_memory_limit" {
		t.Fatalf("error.code=%v want parser_memory_limit", errObj["code"])
	}
	details := errObj["details"].(map[string]any)
	if details["limit"] != "256 MiB" {
		t.Fatalf("limit=%v want 256 MiB", details["limit"])
	}
	if _, ok := details["observed_size"]; !ok {
		t.Fatalf("expected observed_size in error.details")
	}
}

func TestAnalyze_ParserOverridesAcceptedAndPropagated(t *testing.T) {
	var captured parser.Config
	mockP := &mockParser{
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"timeout_seconds":8,"max_old_space_mb":768}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if captured.Timeout != 8*time.Second {
		t.Fatalf("captured timeout=%s want=8s", captured.Timeout)
	}
	if captured.NodeMaxOldSpaceMB != 768 {
		t.Fatalf("captured memory=%d want=768", captured.NodeMaxOldSpaceMB)
	}
}

func TestAnalyze_SourceEnhancementCapabilityEnabledForDefaultRules(t *testing.T) {
	var captured parser.Config
	mockP := &mockParser{
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if !captured.NeedSourceEnhancement {
		t.Fatal("expected NeedSourceEnhancement=true when source-dependent rules are enabled by default")
	}
}

func TestAnalyze_SourceEnhancementCapabilityPreservesParserSourceEnhancementSetting(t *testing.T) {
	sourceEnhancementValue := false
	baseCfg := parser.Config{Timeout: 5 * time.Second, NodeMaxOldSpaceMB: 512, SourceEnhancement: &sourceEnhancementValue}
	var captured parser.Config
	mockP := &mockParser{
		parserConfig: &baseCfg,
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if captured.SourceEnhancement == nil || *captured.SourceEnhancement {
		t.Fatalf("expected SourceEnhancement=false to be preserved from parser config, got %+v", captured.SourceEnhancement)
	}
	if !captured.NeedSourceEnhancement {
		t.Fatal("expected NeedSourceEnhancement=true when source-dependent rules are enabled by default")
	}
}

func TestAnalyze_SourceEnhancementCapabilityDisabledWhenSourceRulesAreDisabled(t *testing.T) {
	var captured parser.Config
	mockP := &mockParser{
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	payload := `{"code":"graph TD; A-->B","config":{"schema-version":"v1","rules":{"no-disconnected-nodes":{"enabled":false},"no-duplicate-node-ids":{"enabled":false}}}}`
	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if captured.NeedSourceEnhancement {
		t.Fatal("expected NeedSourceEnhancement=false when source-dependent rules are disabled")
	}
}

func TestAnalyze_ParserOverridePreservesParserInstanceMemoryWhenOnlyTimeoutProvided(t *testing.T) {
	baseCfg := parser.Config{Timeout: 11 * time.Second, NodeMaxOldSpaceMB: 768}
	var captured parser.Config
	mockP := &mockParser{
		parserConfig: &baseCfg,
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"timeout_seconds":8}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if captured.Timeout != 8*time.Second {
		t.Fatalf("captured timeout=%s want=8s", captured.Timeout)
	}
	if captured.NodeMaxOldSpaceMB != 768 {
		t.Fatalf("captured memory=%d want=768", captured.NodeMaxOldSpaceMB)
	}
}

func TestAnalyze_ParserOverridePreservesParserInstanceTimeoutWhenOnlyMemoryProvided(t *testing.T) {
	baseCfg := parser.Config{Timeout: 9 * time.Second, NodeMaxOldSpaceMB: 768}
	var captured parser.Config
	mockP := &mockParser{
		parserConfig: &baseCfg,
		parseWithConfig: func(_ string, cfg parser.Config) (*model.Diagram, *parser.SyntaxError, error) {
			captured = cfg
			return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
		},
	}
	mux := http.NewServeMux()
	h := api.NewHandler(mockP, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"max_old_space_mb":1024}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusOK)
	}
	if captured.Timeout != 9*time.Second {
		t.Fatalf("captured timeout=%s want=9s", captured.Timeout)
	}
	if captured.NodeMaxOldSpaceMB != 1024 {
		t.Fatalf("captured memory=%d want=1024", captured.NodeMaxOldSpaceMB)
	}
}

func TestAnalyze_InvalidParserOverrideValidation(t *testing.T) {
	cases := []string{
		`{"code":"graph TD; A-->B","parser":{"timeout_seconds":0}}`,
		`{"code":"graph TD; A-->B","parser":{"timeout_seconds":61}}`,
		`{"code":"graph TD; A-->B","parser":{"max_old_space_mb":127}}`,
		`{"code":"graph TD; A-->B","parser":{"max_old_space_mb":4097}}`,
	}

	for _, payload := range cases {
		t.Run(payload, func(t *testing.T) {
			mux := newTestMux(func(string) (*model.Diagram, *parser.SyntaxError, error) {
				return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(payload))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusBadRequest)
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			errObj := body["error"].(map[string]any)
			if errObj["code"] != "invalid_option" {
				t.Fatalf("error.code=%v want invalid_option", errObj["code"])
			}
		})
	}
}

func TestAnalyze_ParserOverridesRejectedWhenParserLacksPerRequestConfig(t *testing.T) {
	mux := http.NewServeMux()
	h := api.NewHandler(&mockParserWithoutConfig{}, engine.New())
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"timeout_seconds":8}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusBadRequest)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "invalid_option" {
		t.Fatalf("error.code=%v want invalid_option", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "per-request parser settings are unsupported") {
		t.Fatalf("error.message=%q want contains unsupported parser settings", msg)
	}
}

func TestAnalyze_ParserOverrideTimeoutBehavior(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-timeout-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-timeout-override.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {
	process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[],edges:[],subgraphs:[],suppressions:[]}}));
}, 2000);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 5 * time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"timeout_seconds":1}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusGatewayTimeout)
	}
}

func TestAnalyzeRaw_MalformedJSONContentType_FallbackHint(t *testing.T) {
	var capturedCode string
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		capturedCode = code
		return nil, &parser.SyntaxError{Message: "Unexpected token", Line: 1, Column: 1}, nil
	})

	body := `{"code": "graph TD
A-->B"`
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if capturedCode != body {
		t.Fatalf("expected parser input to fall back to raw body, got %q", capturedCode)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	hintCodes := hintCodesFromResponse(t, resp)
	if !containsString(hintCodes, "raw_json_decode_failed_fallback_to_text") {
		t.Fatalf("expected raw_json_decode_failed_fallback_to_text hint code, got %v", hintCodes)
	}
}

func TestAnalyzeRaw_JSONContentTypeRawTextFallback_NoDecodeHint(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "plain mermaid",
			body: "graph TD\n  A-->B",
		},
		{
			name: "markdown fenced mermaid",
			body: "```mermaid\ngraph TD\n  A-->B\n```",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var capturedCode string
			mux := newTestMux(func(input string) (*model.Diagram, *parser.SyntaxError, error) {
				capturedCode = input
				return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}
			if capturedCode != tc.body {
				t.Fatalf("expected parser to receive raw body as-is, got %q", capturedCode)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if _, hasHints := resp["hints"]; hasHints {
				t.Fatalf("expected no request-level hints for raw text fallback, got %v", resp["hints"])
			}
		})
	}
}

func TestAnalyzeRaw_TextPlain_ParsesAsRawWithoutFallbackHint(t *testing.T) {
	const code = "graph TD\n  A-->B"
	var capturedCode string
	mux := newTestMux(func(input string) (*model.Diagram, *parser.SyntaxError, error) {
		capturedCode = input
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(code))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedCode != code {
		t.Fatalf("expected parser to receive plain-text body as-is, got %q", capturedCode)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if _, hasHints := resp["hints"]; hasHints {
		t.Fatalf("expected no request-level hints for plain text body, got %v", resp["hints"])
	}
}

func TestAnalyzeRaw_ValidJSONWithCode_UsesCodeField(t *testing.T) {
	const code = "graph LR\n  X-->Y"
	var capturedCode string
	mux := newTestMux(func(input string) (*model.Diagram, *parser.SyntaxError, error) {
		capturedCode = input
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	body, _ := json.Marshal(map[string]any{"code": code})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedCode != code {
		t.Fatalf("expected parser to receive code field value, got %q", capturedCode)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if _, hasHints := resp["hints"]; hasHints {
		t.Fatalf("expected no request-level hints for valid JSON body, got %v", resp["hints"])
	}
}

func TestAnalyzeRaw_ValidJSONMissingCode_ReturnsMissingCode(t *testing.T) {
	mux := newTestMux(func(input string) (*model.Diagram, *parser.SyntaxError, error) {
		t.Fatalf("parser should not be called when JSON code is missing, got %q", input)
		return nil, nil, nil
	})

	body, _ := json.Marshal(map[string]any{"options": map[string]any{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "missing_code", "field 'code' is required")
}

func TestAnalyzeRaw_InvalidJSONTextishBody_FallbackStillWorks(t *testing.T) {
	const body = "{graph TD\n  A-->B"
	var capturedCode string
	mux := newTestMux(func(input string) (*model.Diagram, *parser.SyntaxError, error) {
		capturedCode = input
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if capturedCode != body {
		t.Fatalf("expected parser to receive raw body fallback, got %q", capturedCode)
	}
}

// TestAnalyzeRaw_ValidDiagram_PlainText tests /analyze/raw with raw mermaid text.
func TestAnalyzeRaw_ValidDiagram_PlainText(t *testing.T) {
	validDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A", Label: "Start"},
			{ID: "B", Label: "End"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B", Type: "arrow"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return validDiagram, nil, nil
	})

	rawCode := "graph TD\n  A[Start] --> B[End]"
	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", strings.NewReader(rawCode))
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

	if valid, ok := resp["valid"].(bool); !ok || !valid {
		t.Error("expected valid=true")
	}
	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "flowchart" {
		t.Errorf("expected diagram-type=flowchart, got %v", resp["diagram-type"])
	}
}

// TestAnalyzeRaw_ValidDiagram_JSON tests /analyze/raw with JSON auto-detection.
func TestAnalyzeRaw_ValidDiagram_JSON(t *testing.T) {
	validDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "LR",
		Nodes: []model.Node{
			{ID: "X", Label: "Input"},
			{ID: "Y", Label: "Output"},
		},
		Edges: []model.Edge{
			{From: "X", To: "Y", Type: "arrow"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		if strings.Contains(code, "graph LR") {
			return validDiagram, nil, nil
		}
		return nil, nil, errors.New("unexpected code")
	})

	jsonPayload, _ := json.Marshal(map[string]string{"code": "graph LR\n  X[Input] --> Y[Output]"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", bytes.NewReader(jsonPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if valid, ok := resp["valid"].(bool); !ok || !valid {
		t.Error("expected valid=true")
	}
}

func TestAnalyzeRaw_ConfigOverrideChangesOutput(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"}, {ID: "B"}, {ID: "C"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	baseBody, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A-->B\n  A-->C",
	})
	baseReq := httptest.NewRequest(http.MethodPost, "/analyze/raw", bytes.NewReader(baseBody))
	baseReq.Header.Set("Content-Type", "application/json")
	baseW := httptest.NewRecorder()
	mux.ServeHTTP(baseW, baseReq)
	if baseW.Code != http.StatusOK {
		t.Fatalf("expected 200 for baseline request, got %d body=%s", baseW.Code, baseW.Body.String())
	}

	var baseResp map[string]any
	if err := json.Unmarshal(baseW.Body.Bytes(), &baseResp); err != nil {
		t.Fatalf("decode baseline response: %v", err)
	}
	baseIssues, ok := baseResp["issues"].([]any)
	if !ok {
		t.Fatalf("expected baseline issues array, got %T", baseResp["issues"])
	}
	if len(baseIssues) != 0 {
		t.Fatalf("expected no baseline issues with default config, got %#v", baseIssues)
	}

	overrideBody, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A-->B\n  A-->C",
		"config": map[string]any{
			"schema-version": "v1",
			"rules": map[string]any{
				"max-fanout": map[string]any{"limit": 1},
			},
		},
	})
	overrideReq := httptest.NewRequest(http.MethodPost, "/analyze/raw", bytes.NewReader(overrideBody))
	overrideReq.Header.Set("Content-Type", "application/json")
	overrideW := httptest.NewRecorder()
	mux.ServeHTTP(overrideW, overrideReq)
	if overrideW.Code != http.StatusOK {
		t.Fatalf("expected 200 for override request, got %d body=%s", overrideW.Code, overrideW.Body.String())
	}

	var overrideResp map[string]any
	if err := json.Unmarshal(overrideW.Body.Bytes(), &overrideResp); err != nil {
		t.Fatalf("decode override response: %v", err)
	}
	overrideIssues, ok := overrideResp["issues"].([]any)
	if !ok {
		t.Fatalf("expected override issues array, got %T", overrideResp["issues"])
	}
	if len(overrideIssues) != 1 {
		t.Fatalf("expected exactly one issue with max-fanout.limit=1, got %#v", overrideIssues)
	}
	overrideIssue, ok := overrideIssues[0].(map[string]any)
	if !ok || overrideIssue["rule-id"] != "max-fanout" {
		t.Fatalf("expected max-fanout issue, got %#v", overrideIssues[0])
	}
}

func TestAnalyze_ConfigConsistencyAcrossAnalyzeRawAndSARIF(t *testing.T) {
	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"}, {ID: "B"}, {ID: "C"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]any{
		"code": "graph TD\n  A-->B\n  A-->C",
		"config": map[string]any{
			"schema-version": "v1",
			"rules": map[string]any{
				"max-fanout": map[string]any{"limit": 1},
			},
		},
	})

	for _, endpoint := range []string{"/analyze", "/analyze/raw", "/analyze/sarif"} {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 for %s, got %d body=%s", endpoint, w.Code, w.Body.String())
			}

			if endpoint == "/analyze/sarif" {
				var report map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
					t.Fatalf("decode sarif report: %v", err)
				}
				runs := report["runs"].([]any)
				results := runs[0].(map[string]any)["results"].([]any)
				if len(results) != 1 {
					t.Fatalf("expected exactly one SARIF result, got %#v", results)
				}
				if got := results[0].(map[string]any)["ruleId"]; got != "max-fanout" {
					t.Fatalf("expected SARIF ruleId max-fanout, got %#v", got)
				}
				return
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			issues, ok := resp["issues"].([]any)
			if !ok {
				t.Fatalf("expected issues array, got %T", resp["issues"])
			}
			if len(issues) != 1 {
				t.Fatalf("expected exactly one issue for %s, got %#v", endpoint, issues)
			}
			if got := issues[0].(map[string]any)["rule-id"]; got != "max-fanout" {
				t.Fatalf("expected rule-id max-fanout for %s, got %#v", endpoint, got)
			}
		})
	}
}

// TestAnalyzeRaw_EmptyRequest tests /analyze/raw with empty body.
func TestAnalyzeRaw_EmptyRequest(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", strings.NewReader(""))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if errDetail, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("expected error field in response")
	} else if code, ok := errDetail["code"].(string); !ok || code != "missing_code" {
		t.Errorf("expected error.code=missing_code, got %v", errDetail["code"])
	}
}

// TestAnalyzeRaw_RequestBodyTooLarge tests /analyze/raw with oversized body.
func TestAnalyzeRaw_RequestBodyTooLarge(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	// Create a body larger than 1 MiB
	largeBody := strings.Repeat("a", 1<<20+1)
	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", strings.NewReader(largeBody))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if errDetail, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("expected error field in response")
	} else if code, ok := errDetail["code"].(string); !ok || code != "request_too_large" {
		t.Errorf("expected error.code=request_too_large, got %v", errDetail["code"])
	}
}

// TestAnalyzeRaw_SyntaxError tests /analyze/raw with syntax errors.
func TestAnalyzeRaw_SyntaxError(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, &parser.SyntaxError{
			Message: "Unexpected token",
			Line:    1,
			Column:  5,
		}, nil
	})

	rawCode := "invalid mermaid code"
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader(rawCode))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Error("expected valid=false for syntax error")
	}

	if syntaxErr, ok := resp["syntax-error"].(map[string]interface{}); !ok {
		t.Error("expected syntax-error field in response")
	} else {
		if msg, ok := syntaxErr["message"].(string); !ok || msg != "Unexpected token" {
			t.Errorf("expected message=Unexpected token, got %v", syntaxErr["message"])
		}
		if line, ok := syntaxErr["line"].(float64); !ok || line != 1 {
			t.Errorf("expected line=1, got %v", syntaxErr["line"])
		}
		if col, ok := syntaxErr["column"].(float64); !ok || col != 5 {
			t.Errorf("expected column=5, got %v", syntaxErr["column"])
		}
	}
}

// TestAnalyzeRaw_SequenceDiagram tests /analyze/raw with sequence diagram text.
func TestAnalyzeRaw_SequenceDiagram(t *testing.T) {
	sequenceDiagram := &model.Diagram{
		Type: model.DiagramTypeSequence,
		Nodes: []model.Node{
			{ID: "Alice", Label: "Alice"},
			{ID: "Bob", Label: "Bob"},
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		if strings.HasPrefix(code, "sequenceDiagram") {
			return sequenceDiagram, nil, nil
		}
		return nil, nil, errors.New("wrong diagram type")
	})

	rawCode := "sequenceDiagram\n  Alice ->> Bob: Hello"
	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", strings.NewReader(rawCode))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != "sequence" {
		t.Errorf("expected diagram-type=sequence, got %v", resp["diagram-type"])
	}
	if lintSupported, ok := resp["lint-supported"].(bool); !ok || lintSupported {
		t.Errorf("expected lint-supported=false for sequence diagram, got %v", resp["lint-supported"])
	}
}

// TestAnalyzeRaw_ParserTimeout tests /analyze/raw with timeout error.
func TestAnalyzeRaw_ParserTimeout(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, parser.ErrTimeout
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze/raw", strings.NewReader("graph TD; A-->B"))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if errDetail, ok := resp["error"].(map[string]interface{}); !ok {
		t.Error("expected error field in response")
	} else if code, ok := errDetail["code"].(string); !ok || code != "parser_timeout" {
		t.Errorf("expected error.code=parser_timeout, got %v", errDetail["code"])
	}
}

// TestAnalyze_DuplicateNodeDetection tests that duplicate node IDs are detected and reported correctly.
func TestAnalyze_DuplicateNodeDetection(t *testing.T) {
	tests := []struct {
		name              string
		nodes             []model.Node
		edges             []model.Edge
		wantDuplicateNode string
		wantSubgraphID    string
		description       string
	}{
		{
			name: "simple duplicate nodes",
			nodes: []model.Node{
				{ID: "A", Label: "First A"},
				{ID: "B", Label: "B"},
				{ID: "A", Label: "Second A"},
			},
			edges: []model.Edge{
				{From: "A", To: "B"},
			},
			wantDuplicateNode: "A",
			wantSubgraphID:    "",
			description:       "Duplicate node IDs at root level",
		},
		{
			name: "duplicate in nested subgraph",
			nodes: []model.Node{
				{ID: "A", Label: "Root A"},
				{ID: "SG1_A", Label: "Subgraph A"},
				{ID: "A", Label: "Another Root A"},
			},
			edges:             []model.Edge{},
			wantDuplicateNode: "A",
			wantSubgraphID:    "",
			description:       "Multiple duplicates at root level including one in subgraph context",
		},
		{
			name: "multiple different duplicates",
			nodes: []model.Node{
				{ID: "A", Label: "First A"},
				{ID: "A", Label: "Second A"},
				{ID: "B", Label: "First B"},
				{ID: "B", Label: "Second B"},
			},
			edges:             []model.Edge{},
			wantDuplicateNode: "", // Should have both A and B duplicates
			wantSubgraphID:    "",
			description:       "Multiple different node IDs are duplicated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram := &model.Diagram{
				Type:      model.DiagramTypeFlowchart,
				Direction: "TD",
				Nodes:     tt.nodes,
				Edges:     tt.edges,
			}

			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return diagram, nil, nil
			})

			body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B"})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatal("expected issues array in response")
			}

			// Find duplicate node issues
			var duplicateIssues []map[string]interface{}
			for _, issue := range issues {
				if issueMap, ok := issue.(map[string]interface{}); ok {
					if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "no-duplicate-node-ids" {
						duplicateIssues = append(duplicateIssues, issueMap)
					}
				}
			}

			if len(duplicateIssues) == 0 {
				t.Errorf("expected at least one no-duplicate-node-ids issue, got none")
				return
			}

			// For single duplicate tests, verify specific duplicate
			if tt.wantDuplicateNode != "" {
				found := false
				for _, issue := range duplicateIssues {
					if msg, ok := issue["message"].(string); ok && strings.Contains(msg, tt.wantDuplicateNode) {
						found = true
						// Verify issue has message
						if !strings.HasPrefix(msg, "duplicate node ID:") {
							t.Errorf("expected message format 'duplicate node ID: X', got %q", msg)
						}
						// Verify severity
						if severity, ok := issue["severity"].(string); !ok || severity != "error" {
							t.Errorf("expected severity=error, got %v", issue["severity"])
						}
						break
					}
				}
				if !found {
					t.Errorf("expected duplicate issue for node %q, got: %v", tt.wantDuplicateNode, duplicateIssues)
				}
			} else {
				// For multiple duplicate tests, just verify we got the right number
				if tt.name == "multiple different duplicates" && len(duplicateIssues) < 2 {
					t.Errorf("expected at least 2 duplicate issues, got %d", len(duplicateIssues))
				}
			}
		})
	}
}

// TestAnalyze_FanoutLimitExceedance tests that nodes exceeding the max-fanout limit are detected.
func TestAnalyze_FanoutLimitExceedance(t *testing.T) {
	tests := []struct {
		name            string
		edgeCount       int
		configLimit     *int
		shouldHaveIssue bool
		description     string
	}{
		{
			name:            "default limit (5) with 6 edges",
			edgeCount:       6,
			configLimit:     nil,
			shouldHaveIssue: true,
			description:     "Node exceeds default fanout limit of 5",
		},
		{
			name:            "default limit (5) with 4 edges",
			edgeCount:       4,
			configLimit:     nil,
			shouldHaveIssue: false,
			description:     "Node within default fanout limit",
		},
		{
			name:            "default limit (5) with exactly 5 edges",
			edgeCount:       5,
			configLimit:     nil,
			shouldHaveIssue: false,
			description:     "Node at exactly the default limit",
		},
		{
			name:            "custom limit (10) with 6 edges",
			edgeCount:       6,
			configLimit:     intPtr(10),
			shouldHaveIssue: false,
			description:     "Node within custom higher limit",
		},
		{
			name:            "custom limit (3) with 6 edges",
			edgeCount:       6,
			configLimit:     intPtr(3),
			shouldHaveIssue: true,
			description:     "Node exceeds custom lower limit",
		},
		{
			name:            "custom limit (3) with 3 edges",
			edgeCount:       3,
			configLimit:     intPtr(3),
			shouldHaveIssue: false,
			description:     "Node at custom limit boundary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create diagram with node A having N outgoing edges
			nodes := []model.Node{{ID: "A", Label: "A"}}
			edges := []model.Edge{}
			for i := 1; i <= tt.edgeCount; i++ {
				nodeID := fmt.Sprintf("N%d", i)
				nodes = append(nodes, model.Node{ID: nodeID, Label: nodeID})
				edges = append(edges, model.Edge{From: "A", To: nodeID, Type: "arrow"})
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

			// Build request config
			reqBody := map[string]interface{}{
				"code": "graph TD\n  A-->B",
			}
			if tt.configLimit != nil {
				reqBody["config"] = map[string]interface{}{
					"schema-version": "v1",
					"rules": map[string]interface{}{
						"max-fanout": map[string]interface{}{
							"limit": *tt.configLimit,
						},
					},
				}
			}

			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatal("expected issues array in response")
			}

			// Find max-fanout issues
			var fanoutIssues []map[string]interface{}
			for _, issue := range issues {
				if issueMap, ok := issue.(map[string]interface{}); ok {
					if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "max-fanout" {
						fanoutIssues = append(fanoutIssues, issueMap)
					}
				}
			}

			if tt.shouldHaveIssue {
				if len(fanoutIssues) == 0 {
					t.Errorf("expected max-fanout issue with config limit %v and %d edges", tt.configLimit, tt.edgeCount)
				} else {
					// Verify severity is "warning"
					if severity, ok := fanoutIssues[0]["severity"].(string); !ok || severity != "warning" {
						t.Errorf("expected severity=warning for max-fanout, got %v", fanoutIssues[0]["severity"])
					}
				}
			} else {
				if len(fanoutIssues) > 0 {
					t.Errorf("expected no max-fanout issue with config limit %v and %d edges, but got: %v", tt.configLimit, tt.edgeCount, fanoutIssues)
				}
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

// TestAnalyze_UnknownRuleAndOptionValidation tests that config validation detects unknown rules and options.
func TestAnalyze_UnknownRuleAndOptionValidation(t *testing.T) {
	tests := []struct {
		name            string
		config          map[string]interface{}
		expectedCode    string
		expectedStatus  int
		expectedPath    string
		expectedMessage string
		description     string
	}{
		{
			name: "unknown rule in versioned config",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"fake-rule": map[string]interface{}{},
				},
			},
			expectedCode:    "unknown_rule",
			expectedStatus:  http.StatusOK, // Graceful degradation - unknown rules return 200
			expectedPath:    "config.rules.fake-rule",
			expectedMessage: "unknown rule: fake-rule",
			description:     "Config with unknown rule should gracefully skip with warnings",
		},
		{
			name: "unknown option in versioned config",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{
						"threshold": 5,
					},
				},
			},
			expectedCode:    "unknown_option",
			expectedStatus:  http.StatusBadRequest, // Invalid options still return 400
			expectedPath:    "config.rules.max-fanout.threshold",
			expectedMessage: "unknown option: threshold",
			description:     "Config with unknown option should return error code unknown_option",
		},
		{
			name: "unknown rule in legacy config",
			config: map[string]interface{}{
				"unknown-rule": map[string]interface{}{},
			},
			expectedCode:    "unknown_rule",
			expectedStatus:  http.StatusOK, // Graceful degradation - unknown rules return 200
			expectedPath:    "config.unknown-rule",
			expectedMessage: "unknown rule: unknown-rule",
			description:     "Legacy config with unknown rule should gracefully skip with warnings",
		},
		{
			name: "unknown option in legacy config",
			config: map[string]interface{}{
				"max-fanout": map[string]interface{}{
					"max_value": 10,
				},
			},
			expectedCode:    "unknown_option",
			expectedStatus:  http.StatusBadRequest, // Invalid options still return 400
			expectedPath:    "config.max-fanout.max_value",
			expectedMessage: "unknown option: max_value",
			description:     "Legacy config with unknown option should also be caught",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return &model.Diagram{
					Type:  model.DiagramTypeFlowchart,
					Nodes: []model.Node{{ID: "A"}},
					Edges: []model.Edge{},
				}, nil, nil
			})

			body, _ := json.Marshal(map[string]interface{}{
				"code":   "graph TD\n  A-->B",
				"config": tt.config,
			})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Fatalf("expected %d for invalid config, got %d: %s", tt.expectedStatus, w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// For graceful degradation (200) with unknown rules, check warnings
			if tt.expectedStatus == http.StatusOK {
				warnings, ok := resp["warnings"].([]interface{})
				if !ok || len(warnings) == 0 {
					t.Fatalf("expected warnings for unknown rule, got: %#v", resp)
				}
				return
			}

			// For strict validation (400), check error fields
			validField := resp["valid"].(bool)
			if validField {
				t.Fatalf("expected valid=false for error response, got %v", validField)
			}

			errObj := resp["error"].(map[string]interface{})
			if errObj["code"] != tt.expectedCode {
				t.Errorf("expected error.code=%q, got %q", tt.expectedCode, errObj["code"])
			}

			// Verify message
			if errObj["message"] != tt.expectedMessage {
				t.Errorf("expected error.message=%q, got %q", tt.expectedMessage, errObj["message"])
			}

			// Verify details path
			details := errObj["details"].(map[string]interface{})
			if details["path"] != tt.expectedPath {
				t.Errorf("expected error.details.path=%q, got %q", tt.expectedPath, details["path"])
			}

			// Verify supported list exists (should contain list of known rules/options)
			supported := details["supported"].([]interface{})
			if len(supported) == 0 {
				t.Errorf("expected Supported list to be populated, got empty")
			} else {
				t.Logf("Supported options/rules: %v", supported)
			}
		})
	}
}

// TestAnalyze_SuppressionValidation tests suppression selectors targeting rules.
func TestAnalyze_SuppressionValidation(t *testing.T) {
	tests := []struct {
		name                 string
		nodes                []model.Node
		edges                []model.Edge
		suppressionSelectors []string
		shouldHaveIssue      bool
		description          string
	}{
		{
			name: "suppression for existing rule and matching node",
			nodes: []model.Node{
				{ID: "A", Label: "A"},
				{ID: "B", Label: "B"},
				{ID: "C", Label: "C"},
				{ID: "D", Label: "D"},
				{ID: "E", Label: "E"},
				{ID: "F", Label: "F"},
				{ID: "G", Label: "G"},
			},
			edges: []model.Edge{
				{From: "A", To: "B"},
				{From: "A", To: "C"},
				{From: "A", To: "D"},
				{From: "A", To: "E"},
				{From: "A", To: "F"},
				{From: "A", To: "G"},
			},
			suppressionSelectors: []string{"node:A", "rule:max-fanout"},
			shouldHaveIssue:      false,
			description:          "Suppression targeting max-fanout on node A should suppress the issue",
		},
		{
			name: "suppression for non-existent rule (silently ignored)",
			nodes: []model.Node{
				{ID: "A", Label: "A"},
				{ID: "B", Label: "B"},
				{ID: "C", Label: "C"},
				{ID: "D", Label: "D"},
				{ID: "E", Label: "E"},
				{ID: "F", Label: "F"},
				{ID: "G", Label: "G"},
			},
			edges: []model.Edge{
				{From: "A", To: "B"},
				{From: "A", To: "C"},
				{From: "A", To: "D"},
				{From: "A", To: "E"},
				{From: "A", To: "F"},
				{From: "A", To: "G"},
			},
			suppressionSelectors: []string{"rule:fake-rule-that-does-not-exist"},
			shouldHaveIssue:      true,
			description:          "Suppression for unknown rule should be silently ignored, issue should still appear",
		},
		{
			name: "multiple suppressions mix valid and invalid",
			nodes: []model.Node{
				{ID: "A", Label: "A"},
				{ID: "B", Label: "B"},
				{ID: "C", Label: "C"},
				{ID: "D", Label: "D"},
				{ID: "E", Label: "E"},
				{ID: "F", Label: "F"},
				{ID: "G", Label: "G"},
			},
			edges: []model.Edge{
				{From: "A", To: "B"},
				{From: "A", To: "C"},
				{From: "A", To: "D"},
				{From: "A", To: "E"},
				{From: "A", To: "F"},
				{From: "A", To: "G"},
			},
			suppressionSelectors: []string{"rule:fake-rule", "rule:max-fanout"},
			shouldHaveIssue:      false,
			description:          "Valid suppression should work even with invalid ones present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram := &model.Diagram{
				Type:      model.DiagramTypeFlowchart,
				Direction: "TD",
				Nodes:     tt.nodes,
				Edges:     tt.edges,
			}

			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return diagram, nil, nil
			})

			config := map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{
						"limit":                 5,
						"suppression-selectors": tt.suppressionSelectors,
					},
				},
			}

			body, _ := json.Marshal(map[string]interface{}{
				"code":   "graph TD\n  A-->B",
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
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatal("expected issues array in response")
			}

			// Find max-fanout issues
			var fanoutIssues []map[string]interface{}
			for _, issue := range issues {
				if issueMap, ok := issue.(map[string]interface{}); ok {
					if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "max-fanout" {
						fanoutIssues = append(fanoutIssues, issueMap)
					}
				}
			}

			if tt.shouldHaveIssue {
				if len(fanoutIssues) == 0 {
					t.Errorf("expected max-fanout issue(s), got none")
				}
			} else {
				if len(fanoutIssues) > 0 {
					t.Errorf("expected no max-fanout issue (should be suppressed), but got: %v", fanoutIssues)
				}
			}
		})
	}
}

// TestAnalyze_DeprecationWarnings tests that deprecated config formats and fields produce deprecation warnings.
func TestAnalyze_DeprecationWarnings(t *testing.T) {
	tests := []struct {
		name               string
		config             map[string]interface{}
		expectedDeprecated bool
		description        string
	}{
		{
			name: "legacy unversioned config shape",
			config: map[string]interface{}{
				"max-fanout": map[string]interface{}{"limit": 2},
			},
			expectedDeprecated: true,
			description:        "Unversioned (flat) config format should produce deprecation warning",
		},
		{
			name: "legacy snake_case option key",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{
						"suppression_selectors": []string{"node:A"},
					},
				},
			},
			expectedDeprecated: true,
			description:        "snake_case option keys should produce deprecation warning (should use kebab-case)",
		},
		{
			name: "canonical versioned config",
			config: map[string]interface{}{
				"schema-version": "v1",
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{
						"limit": 2,
					},
				},
			},
			expectedDeprecated: false,
			description:        "Canonical versioned config should not produce deprecation warning",
		},
		{
			name: "nested legacy config (without schema-version)",
			config: map[string]interface{}{
				"rules": map[string]interface{}{
					"max-fanout": map[string]interface{}{"limit": 2},
				},
			},
			expectedDeprecated: true,
			description:        "Legacy nested config without schema-version should produce deprecation warning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram := &model.Diagram{
				Type:  model.DiagramTypeFlowchart,
				Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
				Edges: []model.Edge{{From: "A", To: "B"}},
			}

			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return diagram, nil, nil
			})

			body, _ := json.Marshal(map[string]interface{}{
				"code":   "graph TD\n  A --> B",
				"config": tt.config,
			})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			// Check Deprecation header
			hasDeprecation := w.Header().Get("Deprecation") == "true"
			hasWarningHeader := w.Header().Get("Warning") != ""

			if tt.expectedDeprecated {
				if !hasDeprecation {
					t.Errorf("expected Deprecation header=true, got %q", w.Header().Get("Deprecation"))
				}
				if !hasWarningHeader {
					t.Errorf("expected Warning header to be present, got %q", w.Header().Get("Warning"))
				}
			} else {
				if hasDeprecation {
					t.Errorf("expected no Deprecation header for canonical config, got %q", w.Header().Get("Deprecation"))
				}
			}

			// Check warnings in response body
			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			warnings, ok := resp["warnings"].([]interface{})
			if tt.expectedDeprecated {
				if !ok || len(warnings) == 0 {
					t.Errorf("expected warnings array in response for deprecated config, got %v", resp["warnings"])
				} else {
					// Verify warnings are not duplicated (each warning should appear once)
					warningMsgs := make([]string, 0, len(warnings))
					for _, w := range warnings {
						if warnMap, ok := w.(map[string]interface{}); ok {
							if msg, ok := warnMap["message"].(string); ok {
								warningMsgs = append(warningMsgs, msg)
							}
						}
					}
					// Check for duplicates
					seen := make(map[string]int)
					for _, msg := range warningMsgs {
						seen[msg]++
					}
					for msg, count := range seen {
						if count > 1 {
							t.Errorf("warning %q appears %d times (expected to be deduplicated)", msg, count)
						}
					}
				}
			} else {
				if ok && len(warnings) > 0 {
					t.Errorf("expected no warnings for canonical config, got %v", warnings)
				}
			}
		})
	}
}

// TestAnalyze_SyntaxErrorResilience tests handling of various syntax errors in mermaid diagrams.
func TestAnalyze_SyntaxErrorResilience(t *testing.T) {
	tests := []struct {
		name           string
		syntaxErr      *parser.SyntaxError
		expectedCode   string
		expectedMetric string
		description    string
	}{
		{
			name: "graphviz arrow syntax error",
			syntaxErr: &parser.SyntaxError{
				Message: "unsupported arrow syntax: -->",
				Line:    2,
				Column:  5,
			},
			expectedCode:   "lint_supported",
			expectedMetric: "unknown",
			description:    "Graphviz-style arrows should be caught as syntax errors",
		},
		{
			name: "malformed YAML front matter",
			syntaxErr: &parser.SyntaxError{
				Message: "invalid YAML front matter",
				Line:    1,
				Column:  0,
			},
			expectedCode:   "lint_supported",
			expectedMetric: "unknown",
			description:    "Malformed YAML delimiters should error",
		},
		{
			name: "misaligned indentation",
			syntaxErr: &parser.SyntaxError{
				Message: "unexpected indentation",
				Line:    3,
				Column:  0,
			},
			expectedCode:   "lint_supported",
			expectedMetric: "unknown",
			description:    "Tab/indentation misalignment should be caught",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
				return nil, tt.syntaxErr, nil
			})

			body, _ := json.Marshal(map[string]string{"code": "invalid code"})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200 for syntax error (linting still supported), got %d", w.Code)
			}

			var resp struct {
				Valid       bool                   `json:"valid"`
				SyntaxError *parser.SyntaxError    `json:"syntax-error"`
				Metrics     map[string]interface{} `json:"metrics"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			if resp.Valid {
				t.Errorf("expected valid=false for syntax error, got true")
			}

			if resp.SyntaxError == nil {
				t.Errorf("expected syntax-error object in response")
			} else {
				if resp.SyntaxError.Message != tt.syntaxErr.Message {
					t.Errorf("expected syntax error message %q, got %q", tt.syntaxErr.Message, resp.SyntaxError.Message)
				}
			}

			// Verify diagram-type metric is "unknown" when parse fails
			if diagramType, ok := resp.Metrics["diagram-type"].(string); !ok || diagramType != tt.expectedMetric {
				t.Errorf("expected metrics.diagram-type=%q, got %v", tt.expectedMetric, resp.Metrics["diagram-type"])
			}
		})
	}
}

// TestAnalyze_ParserMemoryLimitExceeded tests handling of parser memory limit errors.
func TestAnalyze_ParserMemoryLimitExceeded(t *testing.T) {
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, fmt.Errorf("%w: exceeded max memory", parser.ErrMemoryLimit)
	})

	body, _ := json.Marshal(map[string]string{"code": "very large diagram"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for memory limit error, got %d", w.Code)
	}

	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error.Code != "parser_memory_limit" {
		t.Errorf("expected error.code=parser_memory_limit, got %q", resp.Error.Code)
	}
}

// TestAnalyze_ConcurrentRequests verifies deterministic outcomes for concurrent requests.
func TestAnalyze_ConcurrentRequests(t *testing.T) {
	const parserLimit = 2

	entered := make(chan struct{}, parserLimit)
	release := make(chan struct{})

	mockP := &mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		entered <- struct{}{}
		<-release
		return &model.Diagram{
			Type:  model.DiagramTypeFlowchart,
			Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
			Edges: []model.Edge{{From: "A", To: "B"}},
		}, nil, nil
	}}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(parserLimit)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A --> B"})

	start := make(chan struct{})
	type result struct {
		status int
		retry  string
	}
	results := make(chan result, parserLimit+1)

	for i := 0; i < parserLimit+1; i++ {
		go func() {
			<-start
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			results <- result{status: w.Code, retry: w.Header().Get("Retry-After")}
		}()
	}

	close(start)

	for i := 0; i < parserLimit; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for admitted parse call %d", i+1)
		}
	}

	busy := <-results
	if busy.status != http.StatusServiceUnavailable {
		t.Fatalf("expected overflow request to return 503, got %d", busy.status)
	}
	if busy.retry != "1" {
		t.Fatalf("expected Retry-After header value 1 on busy response, got %q", busy.retry)
	}

	close(release)

	successes := 0
	for successes < parserLimit {
		res := <-results
		if res.status != http.StatusOK {
			t.Fatalf("expected admitted request to complete with 200, got %d", res.status)
		}
		successes++
	}
}

func TestAnalyze_ConcurrentRequestsWithStrictConfigToggling(t *testing.T) {
	mux, h := newTestMuxWithHandler(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
	})

	requestBody, err := json.Marshal(map[string]any{
		"code": "graph TD\n  A --> B",
		"config": map[string]any{
			"schema-version": "v1",
			"rules":          map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	const toggleIterations = 600
	const requestGoroutines = 8
	const requestsPerGoroutine = 120

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < toggleIterations; i++ {
			h.SetStrictConfigSchema(i%2 == 0)
		}
	}()

	errCh := make(chan error, requestGoroutines*requestsPerGoroutine)
	for i := 0; i < requestGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(requestBody))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					errCh <- fmt.Errorf("unexpected status %d body=%s", w.Code, w.Body.String())
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// TestAnalyze_SARIFOutputFormat tests SARIF output format and structure.
func TestAnalyze_SARIFOutputFormat(t *testing.T) {
	// Diagram with violations
	diagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{
			{ID: "A", Label: "A"},
			{ID: "A", Label: "Duplicate A"},
			{ID: "B", Label: "B"},
			{ID: "C", Label: "C"},
			{ID: "D", Label: "D"},
			{ID: "E", Label: "E"},
			{ID: "F", Label: "F"},
			{ID: "G", Label: "G"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
			{From: "A", To: "D"},
			{From: "A", To: "E"},
			{From: "A", To: "F"},
			{From: "A", To: "G"},
			// C is disconnected, B has only incoming edge
		},
	}

	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return diagram, nil, nil
	})

	body, _ := json.Marshal(map[string]string{"code": "graph TD\n  A-->B\n  A-->C"})
	req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SARIF endpoint, got %d: %s", w.Code, w.Body.String())
	}

	var sarifOutput sarif.Report
	if err := json.Unmarshal(w.Body.Bytes(), &sarifOutput); err != nil {
		t.Fatalf("failed to unmarshal SARIF response: %v", err)
	}

	// Verify basic SARIF structure
	if sarifOutput.Version != "2.1.0" {
		t.Errorf("expected SARIF version 2.1.0, got %q", sarifOutput.Version)
	}

	if len(sarifOutput.Runs) == 0 {
		t.Fatalf("expected non-empty runs array in SARIF output")
	}

	type finding struct {
		ruleID  string
		level   string
		message string
		line    int
		column  int
		hasLoc  bool
	}

	toFinding := func(result sarif.Result) finding {
		f := finding{
			ruleID:  result.RuleID,
			level:   result.Level,
			message: result.Message.Text,
		}
		if len(result.Locations) > 0 {
			region := result.Locations[0].PhysicalLocation.Region
			f.hasLoc = region != nil
			if region != nil {
				f.line = region.StartLine
				f.column = region.StartColumn
			}
		}
		return f
	}

	got := make([]finding, 0, len(sarifOutput.Runs[0].Results))
	for _, result := range sarifOutput.Runs[0].Results {
		got = append(got, toFinding(result))
	}

	want := []finding{
		{
			ruleID:  "max-fanout",
			level:   "warning",
			message: "node \"A\" has fanout 6, exceeding limit of 5",
			hasLoc:  false,
		},
		{
			ruleID:  "no-duplicate-node-ids",
			level:   "error",
			message: "duplicate node ID: A",
			hasLoc:  false,
		},
	}

	sort.Slice(got, func(i, j int) bool {
		if got[i].ruleID != got[j].ruleID {
			return got[i].ruleID < got[j].ruleID
		}
		if got[i].level != got[j].level {
			return got[i].level < got[j].level
		}
		if got[i].message != got[j].message {
			return got[i].message < got[j].message
		}
		if got[i].hasLoc != got[j].hasLoc {
			return !got[i].hasLoc
		}
		if got[i].line != got[j].line {
			return got[i].line < got[j].line
		}
		return got[i].column < got[j].column
	})
	sort.Slice(want, func(i, j int) bool {
		if want[i].ruleID != want[j].ruleID {
			return want[i].ruleID < want[j].ruleID
		}
		if want[i].level != want[j].level {
			return want[i].level < want[j].level
		}
		if want[i].message != want[j].message {
			return want[i].message < want[j].message
		}
		if want[i].hasLoc != want[j].hasLoc {
			return !want[i].hasLoc
		}
		if want[i].line != want[j].line {
			return want[i].line < want[j].line
		}
		return want[i].column < want[j].column
	})

	if !reflect.DeepEqual(got, want) {
		t.Errorf("unexpected SARIF findings (-want +got)\nwant: %#v\n got: %#v", want, got)
	}
}

// TestAnalyze_MetricsTracking tests that metrics are correctly tracked for analyze requests.
func TestAnalyze_MetricsTracking(t *testing.T) {
	cleanDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes:     []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges:     []model.Edge{{From: "A", To: "B"}, {From: "B", To: "C"}},
	}

	violationsDiagram := &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "BT",
		Nodes:     []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges:     []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
	}

	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{parseFunc: func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		switch code {
		case "clean":
			return cleanDiagram, nil, nil
		case "violations":
			return violationsDiagram, nil, nil
		case "parser-error":
			return nil, nil, parser.ErrSubprocess
		case "internal-error":
			return nil, nil, nil
		default:
			t.Fatalf("unexpected test code input to parser mock: %q", code)
			return nil, nil, errors.New("unexpected test code input")
		}
	}}, engine.NewWithRules(metricsConditionalRuleA{}, metricsConditionalRuleB{}))
	h.RegisterRoutes(mux)

	tests := []struct {
		name              string
		code              string
		expectStatus      int
		expectIssueCount  int
		expectNodeCount   float64
		expectEdgeCount   float64
		expectDiagramType string
		expectBySeverity  map[string]float64
		expectByRule      map[string]float64
		expectErrorCode   string
	}{
		{
			name:              "clean diagram",
			code:              "clean",
			expectStatus:      http.StatusOK,
			expectIssueCount:  0,
			expectNodeCount:   3,
			expectEdgeCount:   2,
			expectDiagramType: "flowchart",
			expectBySeverity:  map[string]float64{},
			expectByRule:      map[string]float64{},
		},
		{
			name:              "diagram with violations",
			code:              "violations",
			expectStatus:      http.StatusOK,
			expectIssueCount:  3,
			expectNodeCount:   4,
			expectEdgeCount:   3,
			expectDiagramType: "flowchart",
			expectBySeverity: map[string]float64{
				"warning": 2,
				"error":   1,
			},
			expectByRule: map[string]float64{
				"custom/test/metrics-conditional-a": 2,
				"custom/test/metrics-conditional-b": 1,
			},
		},
		{
			name:              "parser error",
			code:              "parser-error",
			expectStatus:      http.StatusInternalServerError,
			expectIssueCount:  0,
			expectNodeCount:   0,
			expectEdgeCount:   0,
			expectDiagramType: "unknown",
			expectBySeverity:  map[string]float64{},
			expectByRule:      map[string]float64{},
			expectErrorCode:   "parser_subprocess_error",
		},
		{
			name:              "internal error",
			code:              "internal-error",
			expectStatus:      http.StatusInternalServerError,
			expectIssueCount:  0,
			expectNodeCount:   0,
			expectEdgeCount:   0,
			expectDiagramType: "unknown",
			expectBySeverity:  map[string]float64{},
			expectByRule:      map[string]float64{},
			expectErrorCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"code": tt.code})
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tt.expectStatus {
				t.Fatalf("expected %d, got %d", tt.expectStatus, w.Code)
			}

			var resp map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			issues, ok := resp["issues"].([]any)
			if !ok {
				t.Fatalf("expected issues array, got %T", resp["issues"])
			}
			if len(issues) != tt.expectIssueCount {
				t.Errorf("expected %d issues, got %d", tt.expectIssueCount, len(issues))
			}

			metrics, ok := resp["metrics"].(map[string]any)
			if !ok {
				t.Fatalf("expected metrics object, got %T", resp["metrics"])
			}
			if got := metrics["node-count"]; got != tt.expectNodeCount {
				t.Errorf("expected metrics.node-count=%v, got %v", tt.expectNodeCount, got)
			}
			if got := metrics["edge-count"]; got != tt.expectEdgeCount {
				t.Errorf("expected metrics.edge-count=%v, got %v", tt.expectEdgeCount, got)
			}
			if got := metrics["diagram-type"]; got != tt.expectDiagramType {
				t.Errorf("expected metrics.diagram-type=%q, got %v", tt.expectDiagramType, got)
			}

			issueCounts, ok := metrics["issue-counts"].(map[string]any)
			if !ok {
				t.Fatalf("expected issue-counts object, got %T", metrics["issue-counts"])
			}

			bySeverity, ok := issueCounts["by-severity"].(map[string]any)
			if !ok {
				t.Fatalf("expected by-severity object, got %T", issueCounts["by-severity"])
			}
			if !reflect.DeepEqual(bySeverity, anyFromFloat64Map(tt.expectBySeverity)) {
				t.Errorf("expected by-severity=%v, got %v", tt.expectBySeverity, bySeverity)
			}

			byRule, ok := issueCounts["by-rule"].(map[string]any)
			if !ok {
				t.Fatalf("expected by-rule object, got %T", issueCounts["by-rule"])
			}
			if !reflect.DeepEqual(byRule, anyFromFloat64Map(tt.expectByRule)) {
				t.Errorf("expected by-rule=%v, got %v", tt.expectByRule, byRule)
			}

			if tt.expectErrorCode != "" {
				errPayload, ok := resp["error"].(map[string]any)
				if !ok {
					t.Fatalf("expected error object, got %T", resp["error"])
				}
				if got := errPayload["code"]; got != tt.expectErrorCode {
					t.Errorf("expected error.code=%q, got %v", tt.expectErrorCode, got)
				}
			}
		})
	}
}

func anyFromFloat64Map(in map[string]float64) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// TestAnalyze_MaxDepthViolation tests that nodes violating the max-depth limit are detected.
func TestAnalyze_MaxDepthViolation(t *testing.T) {
	tests := []struct {
		name            string
		chainLength     int
		configLimit     *int
		shouldHaveIssue bool
		description     string
	}{
		{
			name:            "default limit (8) with chain of 10",
			chainLength:     10,
			configLimit:     nil,
			shouldHaveIssue: true,
			description:     "Chain exceeds default max depth of 8 (10 nodes = 9 edges = depth 9)",
		},
		{
			name:            "default limit (8) with chain of 9",
			chainLength:     9,
			configLimit:     nil,
			shouldHaveIssue: false,
			description:     "Chain at exactly the default limit (9 nodes = 8 edges = depth 8)",
		},
		{
			name:            "default limit (8) with chain of 8",
			chainLength:     8,
			configLimit:     nil,
			shouldHaveIssue: false,
			description:     "Chain within default limit (8 nodes = 7 edges = depth 7)",
		},
		{
			name:            "custom limit (20) with chain of 10",
			chainLength:     10,
			configLimit:     intPtr(20),
			shouldHaveIssue: false,
			description:     "Chain within custom higher limit",
		},
		{
			name:            "custom limit (5) with chain of 10",
			chainLength:     10,
			configLimit:     intPtr(5),
			shouldHaveIssue: true,
			description:     "Chain exceeds custom lower limit (10 nodes = 9 edges = depth 9 > limit 5)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a chain of nodes: N0 -> N1 -> N2 -> ... -> N(chainLength-1)
			nodes := make([]model.Node, tt.chainLength)
			edges := make([]model.Edge, tt.chainLength-1)
			for i := 0; i < tt.chainLength; i++ {
				nodeID := fmt.Sprintf("N%d", i)
				nodes[i] = model.Node{ID: nodeID, Label: nodeID}
				if i > 0 {
					edges[i-1] = model.Edge{From: fmt.Sprintf("N%d", i-1), To: nodeID, Type: "arrow"}
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

			// Build request config
			reqBody := map[string]interface{}{
				"code": "graph TD\n  N0-->N1",
			}
			if tt.configLimit != nil {
				reqBody["config"] = map[string]interface{}{
					"schema-version": "v1",
					"rules": map[string]interface{}{
						"max-depth": map[string]interface{}{
							"limit": *tt.configLimit,
						},
					},
				}
			}

			body, _ := json.Marshal(reqBody)
			req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatal("expected issues array in response")
			}

			// Find max-depth issues
			var depthIssues []map[string]interface{}
			for _, issue := range issues {
				if issueMap, ok := issue.(map[string]interface{}); ok {
					if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "max-depth" {
						depthIssues = append(depthIssues, issueMap)
					}
				}
			}

			if tt.shouldHaveIssue {
				if len(depthIssues) == 0 {
					t.Errorf("expected max-depth issue with config limit %v and chain length %d", tt.configLimit, tt.chainLength)
				} else {
					// Verify severity is "warning"
					if severity, ok := depthIssues[0]["severity"].(string); !ok || severity != "warning" {
						t.Errorf("expected severity=warning for max-depth, got %v", depthIssues[0]["severity"])
					}
					// Verify message contains depth info
					if msg, ok := depthIssues[0]["message"].(string); !ok || !strings.Contains(msg, "path depth") {
						t.Errorf("expected message to contain 'path depth', got %v", depthIssues[0]["message"])
					}
				}
			} else {
				if len(depthIssues) > 0 {
					t.Errorf("expected no max-depth issue with config limit %v and chain length %d, but got: %v", tt.configLimit, tt.chainLength, depthIssues)
				}
			}
		})
	}
}
