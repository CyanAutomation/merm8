package telemetry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCanonicalOutcome(t *testing.T) {
	if got := CanonicalOutcome(OutcomeInternalError); got != OutcomeInternalError {
		t.Fatalf("CanonicalOutcome(valid) = %q, want %q", got, OutcomeInternalError)
	}
	if got := CanonicalOutcome("invalid_json"); got != OutcomeOther {
		t.Fatalf("CanonicalOutcome(invalid) = %q, want %q", got, OutcomeOther)
	}
}

func TestObserveOutcomeUnknownLabelsUseOther(t *testing.T) {
	m := NewMetrics()
	m.ObserveAnalyzeOutcome("invalid_json")
	m.ObserveParserDuration("missing_code", 5*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	m.Handler().ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `analyze_requests_total{outcome="other"} 1`) {
		t.Fatalf("expected analyze_requests_total fallback label, got %q", body)
	}
	if !strings.Contains(body, `parser_duration_seconds_count{outcome="other"} 1`) {
		t.Fatalf("expected parser_duration_seconds fallback label, got %q", body)
	}
}
