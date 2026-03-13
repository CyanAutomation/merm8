package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestMetrics_UnknownAnalyzeOutcomeDefaultsToOther validates that telemetry library
// coerces unknown analyze outcome labels to 'other' for metric safety.
// @spec: TELEMETRY-001: Unknown outcome labels fallback to 'other' for metric safety
func TestMetrics_UnknownAnalyzeOutcomeDefaultsToOther(t *testing.T) {
	m := NewMetrics()
	m.ObserveAnalyzeOutcome(OutcomeInternalError) // Known outcome
	m.ObserveAnalyzeOutcome("invalid_json")       // Unknown outcome

	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	// Verify known outcome is recorded correctly
	if !strings.Contains(body, `analyze_requests_total{outcome="internal_error"} 1`) {
		t.Fatalf("expected known analyze outcome recorded, got %q", body)
	}
	// Verify unknown outcome falls back to 'other'
	if !strings.Contains(body, `analyze_requests_total{outcome="other"} 1`) {
		t.Fatalf("expected unknown analyze outcome to fallback to 'other', got %q", body)
	}
}

// TestMetrics_UnknownParserDurationOutcomeDefaultsToOther validates that telemetry library
// coerces unknown parser duration outcome labels to 'other' for metric safety.
// @spec: TELEMETRY-001: Unknown outcome labels fallback to 'other' for metric safety
func TestMetrics_UnknownParserDurationOutcomeDefaultsToOther(t *testing.T) {
	m := NewMetrics()
	m.ObserveParserDuration(OutcomeParserTimeout, 5*time.Millisecond) // Known outcome
	m.ObserveParserDuration("missing_code", 5*time.Millisecond)       // Unknown outcome

	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	// Verify known outcome is recorded correctly
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="parser_timeout"} 1`) {
		t.Fatalf("expected known parser duration outcome recorded, got %q", body)
	}
	// Verify unknown outcome falls back to 'other'
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="other"} 1`) {
		t.Fatalf("expected unknown parser duration outcome to fallback to 'other', got %q", body)
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
