// Package rules defines the Rule interface and all built-in lint rules.
package rules

import (
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

const (
	SeverityError = "error"
	SeverityWarn  = "warn"
	SeverityInfo  = "info"
)

// RuleConfig holds per-rule config including shared metadata and rule options.
type RuleConfig struct {
	Enabled  *bool
	Severity string
	Suppress []string
	Options  map[string]interface{}
}

// Config holds per-rule configuration supplied by the caller.
type Config map[string]RuleConfig

// EnabledOrDefault reports whether a rule should run (default true).
func (rc RuleConfig) EnabledOrDefault() bool {
	return rc.Enabled == nil || *rc.Enabled
}

// SeverityOrDefault returns a validated severity, falling back to defaultSeverity.
func (rc RuleConfig) SeverityOrDefault(defaultSeverity string) string {
	s := strings.ToLower(strings.TrimSpace(rc.Severity))
	switch s {
	case SeverityError, SeverityWarn, SeverityInfo:
		return s
	default:
		return defaultSeverity
	}
}

// Option returns a typed option value when present.
func (rc RuleConfig) Option(key string) (interface{}, bool) {
	if rc.Options == nil {
		return nil, false
	}
	v, ok := rc.Options[key]
	return v, ok
}

// Rule is the interface every lint rule must implement.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "no-duplicate-node-ids").
	ID() string
	// Run evaluates the diagram and returns any issues found.
	Run(d *model.Diagram, cfg Config) []model.Issue
}
