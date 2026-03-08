package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

type nilEngineTestParser struct {
	diagram *model.Diagram
}

func (p *nilEngineTestParser) Parse(_ string) (*model.Diagram, *parser.SyntaxError, error) {
	if p.diagram != nil {
		return p.diagram, nil, nil
	}
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func TestNewHandler_DefaultsNilEngineDependency(t *testing.T) {
	h := NewHandler(&nilEngineTestParser{}, nil)
	if h.engine == nil {
		t.Fatal("expected NewHandler to initialize default engine when nil dependency provided")
	}
}

func TestAnalyze_EngineUnavailableReturnsStructuredInternalError(t *testing.T) {
	h := NewHandler(&nilEngineTestParser{}, engine.New())
	h.engine = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Analyze(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != "internal_error" {
		t.Fatalf("expected code internal_error, got %q", resp.Error.Code)
	}
	if resp.Error.Message != "analysis engine unavailable" {
		t.Fatalf("expected stable message, got %q", resp.Error.Message)
	}
	if got := resp.Error.Details["dependency"]; got != "engine" {
		t.Fatalf("expected details.dependency=engine, got %#v", got)
	}
}

func TestAnalyzeRaw_EngineUnavailableReturnsStructuredInternalError(t *testing.T) {
	h := NewHandler(&nilEngineTestParser{}, engine.New())
	h.engine = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/raw", strings.NewReader("graph TD;A-->B"))
	w := httptest.NewRecorder()

	h.AnalyzeRaw(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != "internal_error" || resp.Error.Message != "analysis engine unavailable" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if got := resp.Error.Details["dependency"]; got != "engine" {
		t.Fatalf("expected details.dependency=engine, got %#v", got)
	}
}

func TestAnalyzeSARIF_EngineUnavailableReturnsInternalErrorReport(t *testing.T) {
	h := NewHandler(&nilEngineTestParser{}, engine.New())
	h.engine = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze/sarif", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.AnalyzeSARIF(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/sarif+json" {
		t.Fatalf("expected SARIF content-type, got %q", got)
	}
	if !strings.Contains(w.Body.String(), `"internal_error"`) {
		t.Fatalf("expected internal_error in SARIF body, got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"analysis engine unavailable"`) {
		t.Fatalf("expected stable engine unavailable message in SARIF body, got %s", w.Body.String())
	}
}

func TestRuleConfigSchema_EngineUnavailableReturnsStructuredInternalError(t *testing.T) {
	h := NewHandler(&nilEngineTestParser{}, engine.New())
	h.engine = nil

	req := httptest.NewRequest(http.MethodGet, "/v1/rules/schema", nil)
	w := httptest.NewRecorder()

	h.RuleConfigSchema(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != "internal_error" || resp.Error.Message != "analysis engine unavailable" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if got := resp.Error.Details["dependency"]; got != "engine" {
		t.Fatalf("expected details.dependency=engine, got %#v", got)
	}
}
