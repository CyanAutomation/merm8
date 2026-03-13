package api_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/telemetry"
)

type metricsParser struct {
	diagram   *model.Diagram
	syntaxErr *parser.SyntaxError
	err       error
}

func (m *metricsParser) Parse(_ string) (*model.Diagram, *parser.SyntaxError, error) {
	return m.diagram, m.syntaxErr, m.err
}

func TestMetricsEndpoint_ExposesCoreMetricFamilies(t *testing.T) {
	mux := http.NewServeMux()
	tm := telemetry.NewMetrics()
	h := api.NewHandler(&metricsParser{diagram: &model.Diagram{Type: model.DiagramTypeFlowchart}}, engine.New())
	h.SetTelemetryMetrics(tm)
	h.SetMetricsHandler(tm.Handler())
	h.RegisterRoutes(mux)

	routes := map[string]string{
		"GET /v1/health":   "/v1/health",
		"GET /v1/healthz":  "/v1/healthz",
		"GET /v1/ready":    "/v1/ready",
		"GET /v1/metrics":  "/v1/metrics",
		"POST /v1/analyze": "/v1/analyze",
	}
	root := api.MetricsMiddleware(mux, routes, tm)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/v1/healthz"},
		{method: http.MethodGet, path: "/v1/ready"},
		{method: http.MethodPost, path: "/v1/analyze", body: `{"code":"graph TD\nA-->B"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		root.ServeHTTP(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for /metrics, got %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("expected text/plain content-type, got %q", got)
	}

	body := w.Body.String()
	for _, family := range []string{
		"# HELP request_total",
		"# HELP request_duration_seconds",
		"# HELP analyze_requests_total",
		"# HELP parser_duration_seconds",
	} {
		if !strings.Contains(body, family) {
			t.Fatalf("expected metrics payload to include %q, got %q", family, body)
		}
	}
}

func TestAnalyzeErrorMetrics_OutcomesRecorded(t *testing.T) {
	mux := http.NewServeMux()
	tm := telemetry.NewMetrics()
	h := api.NewHandler(&metricsParser{err: errors.New("boom")}, engine.New())
	h.SetTelemetryMetrics(tm)
	h.SetMetricsHandler(tm.Handler())
	h.RegisterRoutes(mux)

	routes := map[string]string{
		"GET /v1/metrics":  "/v1/metrics",
		"POST /v1/analyze": "/v1/analyze",
	}
	root := api.MetricsMiddleware(mux, routes, tm)

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewBufferString(`{"code":"graph TD\nA-->B"}`))
	w := httptest.NewRecorder()
	root.ServeHTTP(w, req)

	metricsReq := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	metricsW := httptest.NewRecorder()
	root.ServeHTTP(metricsW, metricsReq)

	body := metricsW.Body.String()
	if !strings.Contains(body, `analyze_requests_total{outcome="internal_error"} 1`) {
		t.Fatalf("expected analyze outcome counter for internal_error, got %q", body)
	}
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="internal_error"} 1`) {
		t.Fatalf("expected parser duration histogram count for internal_error, got %q", body)
	}
}

type blockingMetricsParser struct {
	started chan struct{}
	release chan struct{}
}

func (p *blockingMetricsParser) Parse(_ string) (*model.Diagram, *parser.SyntaxError, error) {
	close(p.started)
	<-p.release
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func TestAnalyzeErrorMetrics_NonCanonicalOutcomesCoerced(t *testing.T) {
	mux := http.NewServeMux()
	tm := telemetry.NewMetrics()
	bp := &blockingMetricsParser{started: make(chan struct{}), release: make(chan struct{})}
	h := api.NewHandler(bp, engine.New())
	h.SetParserConcurrencyLimit(1)
	h.SetTelemetryMetrics(tm)
	h.SetMetricsHandler(tm.Handler())
	h.RegisterRoutes(mux)

	routes := map[string]string{
		"GET /v1/metrics":  "/v1/metrics",
		"POST /v1/analyze": "/v1/analyze",
	}
	root := api.MetricsMiddleware(mux, routes, tm)

	go func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewBufferString(`{"code":"graph TD\nA-->B"}`))
		w := httptest.NewRecorder()
		root.ServeHTTP(w, req)
	}()
	<-bp.started

	busyReq := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewBufferString(`{"code":"graph TD\nA-->B"}`))
	busyW := httptest.NewRecorder()
	root.ServeHTTP(busyW, busyReq)
	if busyW.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for concurrent request, got %d", busyW.Code)
	}

	close(bp.release)

	for _, body := range []string{"{invalid}", `{"options":{}}`} {
		req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		root.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %q, got %d", body, w.Code)
		}
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	metricsW := httptest.NewRecorder()
	root.ServeHTTP(metricsW, metricsReq)

	body := metricsW.Body.String()
	if !strings.Contains(body, `analyze_requests_total{outcome="other"} 3`) {
		t.Fatalf("expected fallback analyze outcome count for non-canonical outcomes, got %q", body)
	}
}
