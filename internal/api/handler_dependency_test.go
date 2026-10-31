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

type dependencyTestParser struct{}

func (dependencyTestParser) Parse(string) (*model.Diagram, *parser.SyntaxError, error) {
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

// @spec: API-009: A nil engine dependency falls back to the default analysis engine.
func TestNewHandler_DefaultsNilEngineDependency(t *testing.T) {
	h := NewHandler(dependencyTestParser{}, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(`{"code":"graph TD;A-->B"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var response struct {
		Valid bool `json:"valid"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode analysis response: %v", err)
	}
	if !response.Valid {
		t.Fatalf("analysis valid = false, body=%s", w.Body.String())
	}
}

type dependencyHandler func(*Handler, http.ResponseWriter, *http.Request)

type unavailableDependencyCase struct {
	name            string
	dependency      string
	method          string
	path            string
	body            string
	contentType     string
	wantMessage     string
	wantContentType string
	call            dependencyHandler
}

// @spec: API-013: Handler endpoints return dependency-specific internal errors when a required dependency is unavailable.
func TestHandlerReturnsDependencyUnavailableError(t *testing.T) {
	cases := []unavailableDependencyCase{
		{
			name: "analyze parser", dependency: "parser", method: http.MethodPost,
			path: "/v1/analyze", body: `{"code":"graph TD;A-->B"}`,
			contentType: "application/json", wantMessage: "parser dependency unavailable",
			call: (*Handler).Analyze,
		},
		{
			name: "raw analyze parser", dependency: "parser", method: http.MethodPost,
			path: "/v1/analyze/raw", body: "graph TD;A-->B",
			wantMessage: "parser dependency unavailable", call: (*Handler).AnalyzeRaw,
		},
		{
			name: "SARIF parser", dependency: "parser", method: http.MethodPost,
			path: "/v1/analyze/sarif", body: `{"code":"graph TD;A-->B"}`,
			contentType: "application/json", wantMessage: "parser dependency unavailable",
			wantContentType: "application/sarif+json", call: (*Handler).AnalyzeSARIF,
		},
		{
			name: "analyze engine", dependency: "engine", method: http.MethodPost,
			path: "/v1/analyze", body: `{"code":"graph TD;A-->B"}`,
			contentType: "application/json", wantMessage: "analysis engine unavailable",
			call: (*Handler).Analyze,
		},
		{
			name: "raw analyze engine", dependency: "engine", method: http.MethodPost,
			path: "/v1/analyze/raw", body: "graph TD;A-->B",
			wantMessage: "analysis engine unavailable", call: (*Handler).AnalyzeRaw,
		},
		{
			name: "SARIF engine", dependency: "engine", method: http.MethodPost,
			path: "/v1/analyze/sarif", body: `{"code":"graph TD;A-->B"}`,
			contentType: "application/json", wantMessage: "analysis engine unavailable",
			wantContentType: "application/sarif+json", call: (*Handler).AnalyzeSARIF,
		},
		{
			name: "rule config schema engine", dependency: "engine", method: http.MethodGet,
			path: "/v1/rules/schema", wantMessage: "analysis engine unavailable",
			call: (*Handler).RuleConfigSchema,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parserDependency ParserInterface = dependencyTestParser{}
			if tc.dependency == "parser" {
				parserDependency = nil
			}
			h := NewHandler(parserDependency, engine.New())
			if tc.dependency == "engine" {
				h.engine = nil
			}

			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			w := httptest.NewRecorder()
			tc.call(h, w, req)

			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusInternalServerError, w.Body.String())
			}
			if tc.wantContentType != "" && w.Header().Get("Content-Type") != tc.wantContentType {
				t.Errorf("Content-Type = %q, want %q", w.Header().Get("Content-Type"), tc.wantContentType)
			}

			if tc.wantContentType == "application/sarif+json" {
				if !strings.Contains(w.Body.String(), `"internal_error"`) || !strings.Contains(w.Body.String(), tc.wantMessage) {
					t.Fatalf("SARIF body should include internal error and %q, got %s", tc.wantMessage, w.Body.String())
				}
				return
			}

			var response struct {
				Error struct {
					Code    string         `json:"code"`
					Message string         `json:"message"`
					Details map[string]any `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode error response: %v; body=%s", err, w.Body.String())
			}
			if response.Error.Code != "internal_error" || response.Error.Message != tc.wantMessage {
				t.Errorf("error = (%q, %q), want (internal_error, %q)", response.Error.Code, response.Error.Message, tc.wantMessage)
			}
			if got := response.Error.Details["dependency"]; got != tc.dependency {
				t.Errorf("details.dependency = %#v, want %q", got, tc.dependency)
			}
		})
	}
}
