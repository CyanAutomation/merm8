package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	registry *prometheus.Registry

	requestTotal          *prometheus.CounterVec
	requestDuration       *prometheus.HistogramVec
	analyzeRequests       *prometheus.CounterVec
	parserDuration        *prometheus.HistogramVec
	parserLatency         *prometheus.HistogramVec // request latency histogram (alias for clearer metric naming)
	ruleExecutionTime     *prometheus.HistogramVec
	ruleIssuesEmitted     *prometheus.CounterVec
	ruleViolationsBySev   *prometheus.CounterVec   // per-rule violations by severity
	ruleSuppressions      *prometheus.CounterVec   // per-rule suppression counts
	analysisLatency       *prometheus.HistogramVec // analysis end-to-end latency
	diagramTypeAnalyzed   *prometheus.CounterVec   // analyses by diagram type
	lintSupportCheckCount *prometheus.CounterVec   // count of lint-support checks by result
	corsRejectedTotal     *prometheus.CounterVec   // total rejected CORS origins
	parserCacheEvents     *prometheus.CounterVec   // parser cache events by result and entry type
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,
		requestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "request_total",
			Help: "Total number of HTTP requests by route, method, and status.",
		}, []string{"route", "method", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "request_duration_seconds",
			Help:    "Duration of HTTP requests in seconds by route and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route", "method"}),
		analyzeRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "analyze_requests_total",
			Help: "Total analyze requests by outcome.",
		}, []string{"outcome"}),
		parserDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "parser_duration_seconds",
			Help:    "Duration of parser invocations in seconds by outcome.",
			Buckets: prometheus.DefBuckets,
		}, []string{"outcome"}),
		parserLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "parser_latency_seconds",
			Help:    "Parser request latency in seconds by outcome.",
			Buckets: prometheus.DefBuckets,
		}, []string{"outcome"}),
		ruleExecutionTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rule_execution_duration_seconds",
			Help:    "Duration of individual rule executions in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"rule_id"}),
		ruleIssuesEmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rule_issues_emitted_total",
			Help: "Total number of issues emitted by each linting rule.",
		}, []string{"rule_id"}),
		ruleViolationsBySev: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rule_violations_by_severity_total",
			Help: "Total violations by rule ID and severity (error, warning, info).",
		}, []string{"rule_id", "severity"}),
		ruleSuppressions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rule_suppressions_total",
			Help: "Total suppressions applied by rule ID.",
		}, []string{"rule_id"}),
		analysisLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "analysis_latency_seconds",
			Help:    "End-to-end analysis latency in seconds (from parse to linting complete).",
			Buckets: prometheus.DefBuckets,
		}, []string{"diagram_type"}),
		diagramTypeAnalyzed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "diagram_type_analyzed_total",
			Help: "Total diagrams analyzed by type.",
		}, []string{"diagram_type"}),
		lintSupportCheckCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lint_support_check_total",
			Help: "Total lint-support checks by result (supported or unsupported).",
		}, []string{"diagram_type", "result"}),
		corsRejectedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cors_rejected_total",
			Help: "Total CORS requests rejected because origin is not in the allowlist.",
		}, []string{}),
		parserCacheEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "parser_cache_events_total",
			Help: "Parser cache events grouped by result (hit/miss/eviction) and entry type.",
		}, []string{"result", "entry_type"}),
	}

	registry.MustRegister(
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
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) ObserveHTTPRequest(route, method string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	statusLabel := strconv.Itoa(status)
	m.requestTotal.WithLabelValues(route, method, statusLabel).Inc()
	m.requestDuration.WithLabelValues(route, method).Observe(duration.Seconds())
}

func (m *Metrics) ObserveAnalyzeOutcome(outcome string) {
	if m == nil {
		return
	}
	m.analyzeRequests.WithLabelValues(CanonicalOutcome(outcome)).Inc()
}

func (m *Metrics) ObserveParserDuration(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.parserDuration.WithLabelValues(CanonicalOutcome(outcome)).Observe(duration.Seconds())
}

func (m *Metrics) ObserveParserLatency(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.parserLatency.WithLabelValues(CanonicalOutcome(outcome)).Observe(duration.Seconds())
}

func (m *Metrics) ObserveRuleExecutionDuration(ruleID string, duration time.Duration) {
	if m == nil {
		return
	}
	m.ruleExecutionTime.WithLabelValues(ruleID).Observe(duration.Seconds())
}

func (m *Metrics) ObserveRuleIssuesEmitted(ruleID string, count int) {
	if m == nil {
		return
	}
	for i := 0; i < count; i++ {
		m.ruleIssuesEmitted.WithLabelValues(ruleID).Inc()
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
		m.ruleViolationsBySev.WithLabelValues(ruleID, severity).Inc()
	}
}

// ObserveRuleSuppression records that a rule violation was suppressed.
func (m *Metrics) ObserveRuleSuppression(ruleID string) {
	if m == nil {
		return
	}
	m.ruleSuppressions.WithLabelValues(ruleID).Inc()
}

// ObserveAnalysisLatency records the end-to-end analysis latency by diagram type.
func (m *Metrics) ObserveAnalysisLatency(diagramType string, duration time.Duration) {
	if m == nil {
		return
	}
	m.analysisLatency.WithLabelValues(diagramType).Observe(duration.Seconds())
}

// ObserveDiagramTypeAnalyzed records that a diagram of the given type was analyzed.
func (m *Metrics) ObserveDiagramTypeAnalyzed(diagramType string) {
	if m == nil {
		return
	}
	m.diagramTypeAnalyzed.WithLabelValues(diagramType).Inc()
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
	m.lintSupportCheckCount.WithLabelValues(diagramType, result).Inc()
}

// ObserveCORSRejectedOrigin records a rejected CORS request due to disallowed origin.
func (m *Metrics) ObserveCORSRejectedOrigin() {
	if m == nil {
		return
	}
	m.corsRejectedTotal.WithLabelValues().Inc()
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
	m.parserCacheEvents.WithLabelValues(result, entryType).Inc()
}
