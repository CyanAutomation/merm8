package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
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

	assertDownstreamParity := func(t *testing.T, next http.Handler, path string) {
		t.Helper()

		downstreamReq := httptest.NewRequest(http.MethodGet, path, nil)
		downstreamResp := httptest.NewRecorder()
		next.ServeHTTP(downstreamResp, downstreamReq)

		mw := MetricsMiddleware(next, routes, nil)
		mwReq := httptest.NewRequest(http.MethodGet, path, nil)
		mwResp := httptest.NewRecorder()

		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("expected middleware not to panic with nil metrics, got %v", recovered)
			}
		}()

		mw.ServeHTTP(mwResp, mwReq)

		if mwResp.Code != downstreamResp.Code {
			t.Fatalf("expected middleware status %d to match downstream status %d", mwResp.Code, downstreamResp.Code)
		}

		if mwResp.Body.String() != downstreamResp.Body.String() {
			t.Fatalf("expected middleware body %q to match downstream body %q", mwResp.Body.String(), downstreamResp.Body.String())
		}

		if !reflect.DeepEqual(mwResp.Header(), downstreamResp.Header()) {
			t.Fatalf("expected middleware headers %v to match downstream headers %v", mwResp.Header(), downstreamResp.Header())
		}
	}

	matchedHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Route", "matched")
		w.WriteHeader(http.StatusNoContent)
	})
	assertDownstreamParity(t, matchedHandler, "/health")

	unmatchedHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Route", "unmatched")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("unmatched"))
	})
	assertDownstreamParity(t, unmatchedHandler, "/missing")
}

func TestMetricsMiddleware_WithMetrics_PreservesHTTPBehaviorAndAddsMetrics(t *testing.T) {
	routes := map[string]string{"GET /health": "/health"}
	tm := telemetry.NewMetrics()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Trace", "downstream")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	downstreamReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	downstreamResp := httptest.NewRecorder()
	next.ServeHTTP(downstreamResp, downstreamReq)

	mw := MetricsMiddleware(next, routes, tm)
	mwReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	mwResp := httptest.NewRecorder()
	mw.ServeHTTP(mwResp, mwReq)

	if mwResp.Code != downstreamResp.Code {
		t.Fatalf("expected middleware status %d to match downstream status %d", mwResp.Code, downstreamResp.Code)
	}

	if mwResp.Body.String() != downstreamResp.Body.String() {
		t.Fatalf("expected middleware body %q to match downstream body %q", mwResp.Body.String(), downstreamResp.Body.String())
	}

	if !reflect.DeepEqual(mwResp.Header(), downstreamResp.Header()) {
		t.Fatalf("expected middleware headers %v to match downstream headers %v", mwResp.Header(), downstreamResp.Header())
	}

	metricsResp := httptest.NewRecorder()
	tm.Handler().ServeHTTP(metricsResp, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	payload := metricsResp.Body.String()
	if !strings.Contains(payload, `request_total{route="/health",method="GET",status="201"} 1`) {
		t.Fatalf("expected request_total metric for /health with status 201, got %q", payload)
	}
}
