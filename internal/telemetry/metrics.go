package telemetry

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
)

const (
	OutcomeSyntaxError         = "syntax_error"
	OutcomeLintSuccess         = "lint_success"
	OutcomeParserTimeout       = "parser_timeout"
	OutcomeParserSubprocessErr = "parser_subprocess_error"
	OutcomeParserDecodeErr     = "parser_decode_error"
	OutcomeParserContractErr   = "parser_contract_violation"
	OutcomeInternalError       = "internal_error"
	OutcomeOther               = "other"
)

type Metrics struct {
	collectors []prometheusMetric

	requestTotal          *counterVec
	requestDuration       *histogramVec
	analyzeRequests       *counterVec
	parserDuration        *histogramVec
	parserLatency         *histogramVec // request latency histogram (alias for clearer metric naming)
	ruleExecutionTime     *histogramVec
	ruleIssuesEmitted     *counterVec
	ruleViolationsBySev   *counterVec   // per-rule violations by severity
	ruleSuppressions      *counterVec   // per-rule suppression counts
	analysisLatency       *histogramVec // analysis end-to-end latency
	diagramTypeAnalyzed   *counterVec   // analyses by diagram type
	lintSupportCheckCount *counterVec   // count of lint-support checks by result
	corsRejectedTotal     *counterVec   // total rejected CORS origins
	parserCacheEvents     *counterVec   // parser cache events by result and entry type
}

func NewMetrics() *Metrics {
	m := &Metrics{
		requestTotal:          newCounterVec("request_total", "Total number of HTTP requests by route, method, and status.", "route", "method", "status"),
		requestDuration:       newHistogramVec("request_duration_seconds", "Duration of HTTP requests in seconds by route and method.", "route", "method"),
		analyzeRequests:       newCounterVec("analyze_requests_total", "Total analyze requests by outcome.", "outcome"),
		parserDuration:        newHistogramVec("parser_duration_seconds", "Duration of parser invocations in seconds by outcome.", "outcome"),
		parserLatency:         newHistogramVec("parser_latency_seconds", "Parser request latency in seconds by outcome.", "outcome"),
		ruleExecutionTime:     newHistogramVec("rule_execution_duration_seconds", "Duration of individual rule executions in seconds.", "rule_id"),
		ruleIssuesEmitted:     newCounterVec("rule_issues_emitted_total", "Total number of issues emitted by each linting rule.", "rule_id"),
		ruleViolationsBySev:   newCounterVec("rule_violations_by_severity_total", "Total violations by rule ID and severity (error, warning, info).", "rule_id", "severity"),
		ruleSuppressions:      newCounterVec("rule_suppressions_total", "Total suppressions applied by rule ID.", "rule_id"),
		analysisLatency:       newHistogramVec("analysis_latency_seconds", "End-to-end analysis latency in seconds (from parse to linting complete).", "diagram_type"),
		diagramTypeAnalyzed:   newCounterVec("diagram_type_analyzed_total", "Total diagrams analyzed by type.", "diagram_type"),
		lintSupportCheckCount: newCounterVec("lint_support_check_total", "Total lint-support checks by result (supported or unsupported).", "diagram_type", "result"),
		corsRejectedTotal:     newCounterVec("cors_rejected_total", "Total CORS requests rejected because origin is not in the allowlist."),
		parserCacheEvents:     newCounterVec("parser_cache_events_total", "Parser cache events grouped by result (hit/miss/eviction) and entry type.", "result", "entry_type"),
	}

	m.collectors = []prometheusMetric{
		m.requestTotal,
		m.requestDuration,
		m.analyzeRequests,
		m.parserDuration,
		m.parserLatency,
		m.ruleExecutionTime,
		m.ruleIssuesEmitted,
		m.ruleViolationsBySev,
		m.ruleSuppressions,
		m.analysisLatency,
		m.diagramTypeAnalyzed,
		m.lintSupportCheckCount,
		m.corsRejectedTotal,
		m.parserCacheEvents,
	}
	return m
}

func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var payload strings.Builder
		for _, collector := range m.collectors {
			collector.appendPrometheus(&payload)
		}
		_, _ = w.Write([]byte(payload.String()))
	})
}

func (m *Metrics) ObserveHTTPRequest(route, method string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	statusLabel := strconv.Itoa(status)
	m.requestTotal.inc(route, method, statusLabel)
	m.requestDuration.observe(duration.Seconds(), route, method)
}

func (m *Metrics) ObserveAnalyzeOutcome(outcome string) {
	if m == nil {
		return
	}
	m.analyzeRequests.inc(CanonicalOutcome(outcome))
}

func (m *Metrics) ObserveParserDuration(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.parserDuration.observe(duration.Seconds(), CanonicalOutcome(outcome))
}

func (m *Metrics) ObserveParserLatency(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.parserLatency.observe(duration.Seconds(), CanonicalOutcome(outcome))
}

func (m *Metrics) ObserveRuleExecutionDuration(ruleID string, duration time.Duration) {
	if m == nil {
		return
	}
	m.ruleExecutionTime.observe(duration.Seconds(), ruleID)
}

func (m *Metrics) ObserveRuleIssuesEmitted(ruleID string, count int) {
	if m == nil {
		return
	}
	for i := 0; i < count; i++ {
		m.ruleIssuesEmitted.inc(ruleID)
	}
}

// RecordRuleMetrics implements engine.InstrumentationSink.
func (m *Metrics) RecordRuleMetrics(metrics engine.RuleMetrics) {
	if m == nil {
		return
	}
	ruleID := metrics.RuleID
	duration := time.Duration(metrics.TotalDurationNS)
	m.ObserveRuleExecutionDuration(ruleID, duration)
	m.ObserveRuleIssuesEmitted(ruleID, metrics.IssuesEmitted)
}

// ObserveRuleViolationBySeverity records a violation for a rule with given severity.
func (m *Metrics) ObserveRuleViolationBySeverity(ruleID, severity string) {
	if m == nil {
		return
	}
	// Validate severity to prevent label cardinality explosion
	switch severity {
	case "error", "warning", "info":
		m.ruleViolationsBySev.inc(ruleID, severity)
	}
}

// ObserveRuleSuppression records that a rule violation was suppressed.
func (m *Metrics) ObserveRuleSuppression(ruleID string) {
	if m == nil {
		return
	}
	m.ruleSuppressions.inc(ruleID)
}

// ObserveAnalysisLatency records the end-to-end analysis latency by diagram type.
func (m *Metrics) ObserveAnalysisLatency(diagramType string, duration time.Duration) {
	if m == nil {
		return
	}
	m.analysisLatency.observe(duration.Seconds(), diagramType)
}

// ObserveDiagramTypeAnalyzed records that a diagram of the given type was analyzed.
func (m *Metrics) ObserveDiagramTypeAnalyzed(diagramType string) {
	if m == nil {
		return
	}
	m.diagramTypeAnalyzed.inc(diagramType)
}

// ObserveLintSupportCheck records the result of a lint-support check.
func (m *Metrics) ObserveLintSupportCheck(diagramType string, supported bool) {
	if m == nil {
		return
	}
	result := "unsupported"
	if supported {
		result = "supported"
	}
	m.lintSupportCheckCount.inc(diagramType, result)
}

// ObserveCORSRejectedOrigin records a rejected CORS request due to disallowed origin.
func (m *Metrics) ObserveCORSRejectedOrigin() {
	if m == nil {
		return
	}
	m.corsRejectedTotal.inc()
}

// ObserveParserCacheEvent records parser cache hit/miss/eviction events.
func (m *Metrics) ObserveParserCacheEvent(result, entryType string) {
	if m == nil {
		return
	}
	switch result {
	case "hit", "miss", "eviction":
	default:
		result = "miss"
	}
	switch entryType {
	case "success", "syntax", "any":
	default:
		entryType = "any"
	}
	m.parserCacheEvents.inc(result, entryType)
}
