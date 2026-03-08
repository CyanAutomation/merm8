package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestObserveOutcomeUnknownLabelsUseOther(t *testing.T) {
	m := NewMetrics()
	m.ObserveAnalyzeOutcome(OutcomeInternalError)
	m.ObserveAnalyzeOutcome("invalid_json")
	m.ObserveParserDuration(OutcomeParserTimeout, 5*time.Millisecond)
	m.ObserveParserDuration("missing_code", 5*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `analyze_requests_total{outcome="internal_error"} 1`) {
		t.Fatalf("expected analyze_requests_total known label, got %q", body)
	}
	if !strings.Contains(body, `analyze_requests_total{outcome="other"} 1`) {
		t.Fatalf("expected analyze_requests_total fallback label, got %q", body)
	}
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="parser_timeout"} 1`) {
		t.Fatalf("expected parser_duration_seconds known label, got %q", body)
	}
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="other"} 1`) {
		t.Fatalf("expected parser_duration_seconds fallback label, got %q", body)
	}
}

func TestObserveParserCacheEvent_NormalizesLabels(t *testing.T) {
	m := NewMetrics()
	m.ObserveParserCacheEvent("hit", "success")
	m.ObserveParserCacheEvent("invalid", "invalid")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
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
