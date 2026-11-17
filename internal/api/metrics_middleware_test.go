package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

// TestMetricsMiddleware_RecordsMetricsAndPreservesHTTPBehavior verifies metric labels and
// confirms that instrumentation leaves the downstream HTTP response unchanged.
// @spec: OBSERVABILITY-001: Metrics middleware records HTTP request metadata.
// @spec: OBSERVABILITY-002: Metrics middleware preserves downstream HTTP behavior.
// @spec: OBSERVABILITY-003: Metrics recording does not affect downstream request handling.
func TestMetricsMiddleware_RecordsMetricsAndPreservesHTTPBehavior(t *testing.T) {
	routes := map[string]string{"GET /health": "/health"}
	tm := telemetry.NewMetrics()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Trace", "downstream")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	mw := MetricsMiddleware(next, routes, tm)
	mwReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	mwResp := httptest.NewRecorder()
	mw.ServeHTTP(mwResp, mwReq)

	// Verify status code passes through
	if mwResp.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, mwResp.Code)
	}

	// Verify response body passes through
	if mwResp.Body.String() != "created" {
		t.Fatalf("expected body %q, got %q", "created", mwResp.Body.String())
	}

	// Verify downstream headers pass through
	if got := mwResp.Header().Get("X-Trace"); got != "downstream" {
		t.Fatalf("expected X-Trace header %q, got %q", "downstream", got)
	}

	metricsResp := httptest.NewRecorder()
	tm.Handler().ServeHTTP(metricsResp, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	payload := metricsResp.Body.String()
	assertMetricLabelExists(t, payload, "request_total", map[string]string{
		"route":  "/health",
		"method": "GET",
		"status": "201",
	})
}

// TestMetricsMiddleware_UsesKnownVersionedRouteLabels validates that actively served /v1 routes
// are mapped to explicit metric route labels and do not fall back to "unknown".
// @spec: OBSERVABILITY-004: Versioned API route labels are registered in metrics middleware
func TestMetricsMiddleware_UsesKnownVersionedRouteLabels(t *testing.T) {
	tm := telemetry.NewMetrics()
	routes := map[string]string{
		"POST /v1/analyze":       "/v1/analyze",
		"POST /v1/analyze/raw":   "/v1/analyze/raw",
		"POST /v1/analyze/sarif": "/v1/analyze/sarif",
		"GET /v1/spec":           "/v1/spec",
		"GET /v1/docs":           "/v1/docs",
	}

	mw := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), routes, tm)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/analyze"},
		{method: http.MethodPost, path: "/v1/analyze/raw"},
		{method: http.MethodPost, path: "/v1/analyze/sarif"},
		{method: http.MethodGet, path: "/v1/spec"},
		{method: http.MethodGet, path: "/v1/docs"},
	} {
		request := httptest.NewRequest(tc.method, tc.path, nil)
		response := httptest.NewRecorder()
		mw.ServeHTTP(response, request)
	}

	metricsResp := httptest.NewRecorder()
	tm.Handler().ServeHTTP(metricsResp, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	payload := metricsResp.Body.String()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/v1/analyze"},
		{method: http.MethodPost, path: "/v1/analyze/raw"},
		{method: http.MethodPost, path: "/v1/analyze/sarif"},
		{method: http.MethodGet, path: "/v1/spec"},
		{method: http.MethodGet, path: "/v1/docs"},
	} {
		assertMetricLabelExists(t, payload, "request_total", map[string]string{
			"route":  tc.path,
			"method": tc.method,
			"status": "200",
		})
	}

	if strings.Contains(payload, `request_total{route="unknown"`) {
		t.Fatalf("expected no unknown route label for covered v1 routes, got payload:\n%s", payload)
	}
}
