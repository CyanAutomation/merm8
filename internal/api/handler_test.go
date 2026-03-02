package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
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
func newTestMuxWithRealParser(scriptPath string) *http.ServeMux {
	mux := http.NewServeMux()
	h := api.NewHandler(parser.New(scriptPath), engine.New())
	h.RegisterRoutes(mux)
	return mux
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
	if len(errObj) != 2 {
		t.Fatalf("expected exactly code/message in error object, got %#v", errObj)
	}
	if code, ok := errObj["code"].(string); !ok || code != wantCode {
		t.Fatalf("expected error.code=%q, got %#v", wantCode, errObj["code"])
	}
	if msg, ok := errObj["message"].(string); !ok || msg != wantMessage {
		t.Fatalf("expected error.message=%q, got %#v", wantMessage, errObj["message"])
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
		return nil, nil, context.DeadlineExceeded
	})
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser times out, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_timeout", "parser timed out")
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
	assertExactErrorResponse(t, w.Body.Bytes(), "internal_error", "parser returned nil diagram")
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
	if syntaxErrResp, ok := resp["syntax_error"].(map[string]interface{}); !ok {
		t.Error("expected syntax_error object")
	} else {
		if msg, ok := syntaxErrResp["message"].(string); !ok || msg != "No diagram type detected" {
			t.Errorf("expected error message, got %v", syntaxErrResp["message"])
		}
	}
}

// TestAnalyze_ConfigApplied_MaxFanout tests that custom rule config is applied.
func TestAnalyze_ConfigApplied_MaxFanout(t *testing.T) {
	// Diagram with node A having 3 outgoing edges (violates limit of 2)
	diagram := &model.Diagram{
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

	// Log timing for performance tracking (SLA: should complete in <1 second)
	t.Logf("Large diagram analysis completed in %v (nodes: 500, edges: 499)", elapsed)
	if elapsed > 1*time.Second {
		t.Fatalf("PERFORMANCE SLA EXCEEDED: Large diagram analysis took %v (max 1s)", elapsed)
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
				"severity": "warning",
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

	assertExactErrorResponse(t, w.Body.Bytes(), "invalid_config", `invalid severity for rule "max-fanout": "warning" (allowed: error, warn, info)`)
}
