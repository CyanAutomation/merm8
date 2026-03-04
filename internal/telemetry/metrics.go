package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
)

type Metrics struct {
	registry *prometheus.Registry

	requestTotal       *prometheus.CounterVec
	requestDuration    *prometheus.HistogramVec
	analyzeRequests    *prometheus.CounterVec
	parserDuration     *prometheus.HistogramVec
	ruleExecutionTime  *prometheus.HistogramVec
	ruleIssuesEmitted  *prometheus.CounterVec
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
		ruleExecutionTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "rule_execution_duration_seconds",
			Help:    "Duration of individual rule executions in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"rule_id"}),
		ruleIssuesEmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rule_issues_emitted_total",
			Help: "Total number of issues emitted by each linting rule.",
		}, []string{"rule_id"}),
	}

	registry.MustRegister(m.requestTotal, m.requestDuration, m.analyzeRequests, m.parserDuration, m.ruleExecutionTime, m.ruleIssuesEmitted)
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
	m.analyzeRequests.WithLabelValues(outcome).Inc()
}

func (m *Metrics) ObserveParserDuration(outcome string, duration time.Duration) {
	if m == nil {
		return
	}
	m.parserDuration.WithLabelValues(outcome).Observe(duration.Seconds())
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
