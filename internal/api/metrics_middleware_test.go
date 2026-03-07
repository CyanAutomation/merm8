package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

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
	if !strings.Contains(payload, `request_total{route="/health",method="GET",status="204"} 1`) {
		t.Fatalf("expected request_total metric for /health with status 204, got %q", payload)
	}
}

func TestMetricsMiddleware_AllowsNilMetrics(t *testing.T) {
	routes := map[string]string{"GET /health": "/health"}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	mw := MetricsMiddleware(next, routes, nil)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	mw.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected response status %d, got %d", http.StatusNoContent, response.Code)
	}
}
