package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

// TestMetricsMiddleware_RecordsRequestWhenMetricsConfigured validates that metrics middleware
// records HTTP requests with correct route, method, and status labels.
// @spec: OBSERVABILITY-001: Metrics middleware records HTTP request metadata
func TestMetricsMiddleware_RecordsRequestWhenMetricsConfigured(t *testing.T) {
	tm := telemetry.NewMetrics()
	routes := map[string]string{"GET /health": "/health"}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mw := MetricsMiddleware(next, routes, tm)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	mw.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected response status %d, got %d", http.StatusNoContent, response.Code)
	}

	metricsResp := httptest.NewRecorder()
	tm.Handler().ServeHTTP(metricsResp, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	payload := metricsResp.Body.String()
	// Use structured assertion for Prometheus metrics parsing
	assertMetricLabelExists(t, payload, "request_total", map[string]string{
		"route":  "/health",
		"method": "GET",
		"status": "204",
	})
}

// TestMetricsMiddleware_PreservesHTTPBehavior validates that metrics middleware
// does NOT modify HTTP response status, headers, or body.
// @spec: OBSERVABILITY-002: Metrics middleware preserves downstream HTTP behavior
func TestMetricsMiddleware_PreservesHTTPBehavior(t *testing.T) {
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
}

// TestMetricsMiddleware_RecordsMetricsWithoutSideEffects validates that metrics recording
// does NOT affect downstream request/response handling.
// @spec: OBSERVABILITY-003: Metrics middleware records without side effects
func TestMetricsMiddleware_RecordsMetricsWithoutSideEffects(t *testing.T) {
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

	// Verify metrics were recorded with correct labels
	metricsResp := httptest.NewRecorder()
	tm.Handler().ServeHTTP(metricsResp, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	payload := metricsResp.Body.String()
	// Use structured assertion for Prometheus metrics parsing
	assertMetricLabelExists(t, payload, "request_total", map[string]string{
		"route":  "/health",
		"method": "GET",
		"status": "201",
	})
}
