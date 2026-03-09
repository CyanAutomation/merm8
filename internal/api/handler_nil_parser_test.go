package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
)

func TestAnalyze_NilParserReturnsStructuredInternalError(t *testing.T) {
	h := NewHandler(nil, engine.New())

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
	if resp.Error.Code != "internal_error" || resp.Error.Message != "parser dependency unavailable" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if got := resp.Error.Details["dependency"]; got != "parser" {
		t.Fatalf("expected details.dependency=parser, got %#v", got)
	}
}

func TestAnalyzeRaw_NilParserReturnsStructuredInternalError(t *testing.T) {
	h := NewHandler(nil, engine.New())

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
	if resp.Error.Code != "internal_error" || resp.Error.Message != "parser dependency unavailable" {
		t.Fatalf("unexpected error payload: %+v", resp.Error)
	}
	if got := resp.Error.Details["dependency"]; got != "parser" {
		t.Fatalf("expected details.dependency=parser, got %#v", got)
	}
}

func TestAnalyzeSARIF_NilParserReturnsInternalErrorReport(t *testing.T) {
	h := NewHandler(nil, engine.New())

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
	if !strings.Contains(w.Body.String(), `"parser dependency unavailable"`) {
		t.Fatalf("expected parser dependency unavailable in SARIF body, got %s", w.Body.String())
	}
}
