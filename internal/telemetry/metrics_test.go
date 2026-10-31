package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// @spec: TELEMETRY-001: Unknown outcome labels fallback to 'other' for metric safety.
func TestMetrics_UnknownOutcomeLabelsDefaultToOther(t *testing.T) {
	tests := []struct {
		name       string
		observe    func(*Metrics)
		knownLine  string
		fallback   string
	}{
		{
			name: "analysis outcome",
			observe: func(m *Metrics) {
				m.ObserveAnalyzeOutcome(OutcomeInternalError)
				m.ObserveAnalyzeOutcome("invalid_json")
			},
			knownLine: `analyze_requests_total{outcome="internal_error"} 1`,
			fallback:  `analyze_requests_total{outcome="other"} 1`,
		},
		{
			name: "parser duration outcome",
			observe: func(m *Metrics) {
				m.ObserveParserDuration(OutcomeParserTimeout, 5*time.Millisecond)
				m.ObserveParserDuration("missing_code", 5*time.Millisecond)
			},
			knownLine: `parser_duration_seconds_count{outcome="parser_timeout"} 1`,
			fallback:  `parser_duration_seconds_count{outcome="other"} 1`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMetrics()
			tc.observe(m)
			w := httptest.NewRecorder()
			m.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/metrics", nil))

			body := w.Body.String()
			for _, want := range []string{tc.knownLine, tc.fallback} {
				if !strings.Contains(body, want) {
					t.Errorf("metrics output missing %q; body=%q", want, body)
				}
			}
		})
	}
}

// TestObserveParserCacheEvent_NormalizesLabels validates that cache event labels are normalized
// (hit/miss -> result; success/any -> entry_type) for consistent metric cardinality.
// @spec: TELEMETRY-002: Parser cache event metric label normalization
func TestObserveParserCacheEvent_NormalizesLabels(t *testing.T) {
	m := NewMetrics()
	m.ObserveParserCacheEvent("hit", "success")
	m.ObserveParserCacheEvent("invalid", "invalid")

	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `parser_cache_events_total{result="hit",entry_type="success"} 1`) {
		t.Fatalf("expected parser cache hit metric, got %q", body)
	}
	if !strings.Contains(body, `parser_cache_events_total{result="miss",entry_type="any"} 1`) {
		t.Fatalf("expected parser cache fallback metric, got %q", body)
	}
}

// @spec: TELEMETRY-003: Parser cache occupancy metrics report estimated bytes by cache entry type.
func TestObserveParserCacheBytesExportsEstimatedOccupancy(t *testing.T) {
	m := NewMetrics()
	m.ObserveParserCacheBytes("success", 1024)
	m.ObserveParserCacheBytes("syntax", 17)
	response := httptest.NewRecorder()
	m.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(response.Body.String(), `parser_cache_estimated_bytes{entry_type="success"} 1024`) || !strings.Contains(response.Body.String(), `parser_cache_estimated_bytes{entry_type="syntax"} 17`) {
		t.Fatalf("parser cache bytes absent from metrics: %q", response.Body.String())
	}
}

func TestMetricsHandlerEscapesNewlinesInLabelValues(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveRuleExecutionDuration("rule\nline", 500*time.Millisecond)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	want := "rule_execution_duration_seconds_bucket{rule_id=\"rule\\nline\",le=\"0.5\"} 1\n"
	if !strings.Contains(response.Body.String(), want) {
		t.Fatalf("expected escaped histogram label line %q, got body:\n%s", want, response.Body.String())
	}
}

func TestMetricsHandlerExportsCountersAndHistogramSummaries(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTPRequest("/v1/analyze", "POST", 200, 1500*time.Millisecond)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if got := response.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("unexpected metrics content type %q", got)
	}

	body := response.Body.String()
	for _, want := range []string{
		"# HELP request_total Total number of HTTP requests by route, method, and status.\n",
		"# TYPE request_total counter\n",
		"request_total{route=\"/v1/analyze\",method=\"POST\",status=\"200\"} 1\n",
		"# TYPE request_duration_seconds histogram\n",
		"request_duration_seconds_bucket{route=\"/v1/analyze\",method=\"POST\",le=\"+Inf\"} 1\n",
		"request_duration_seconds_sum{route=\"/v1/analyze\",method=\"POST\"} 1.5\n",
		"request_duration_seconds_count{route=\"/v1/analyze\",method=\"POST\"} 1\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected metrics output to contain %q, got body:\n%s", want, body)
		}
	}
}
