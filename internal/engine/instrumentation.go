package engine

// RuleMetrics captures per-rule execution counters for a single engine run.
type RuleMetrics struct {
	RuleID          string
	Executions      int
	IssuesEmitted   int
	TotalDurationNS int64
}

// InstrumentationSink receives per-rule execution metrics.
type InstrumentationSink interface {
	RecordRuleMetrics(metrics RuleMetrics)
}

// NoopInstrumentationSink ignores all instrumentation.
type NoopInstrumentationSink struct{}

// RecordRuleMetrics implements InstrumentationSink.
func (NoopInstrumentationSink) RecordRuleMetrics(RuleMetrics) {}
