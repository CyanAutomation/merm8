package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/api"
)

// newTestServer creates a test HTTP server backed by a handler that uses the
// given parser script path (may not exist; tests that hit the parser will fail
// gracefully).
func newTestMux(scriptPath string) *http.ServeMux {
	mux := http.NewServeMux()
	h := api.NewHandler(scriptPath)
	h.RegisterRoutes(mux)
	return mux
}

func TestAnalyze_MissingCode(t *testing.T) {
	mux := newTestMux("/nonexistent/parse.mjs")
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAnalyze_InvalidJSON(t *testing.T) {
	mux := newTestMux("/nonexistent/parse.mjs")
	req := httptest.NewRequest(http.MethodPost, "/analyze", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAnalyze_ParserFails_Returns500(t *testing.T) {
	mux := newTestMux("/nonexistent/parse.mjs") // script doesn't exist
	body, _ := json.Marshal(map[string]string{"code": "graph TD; A-->B"})
	req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 when parser script missing, got %d", w.Code)
	}
}
