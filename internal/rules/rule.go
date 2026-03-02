// Package rules defines the Rule interface and all built-in lint rules.
package rules

import (
	"encoding/json"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

// Config holds per-rule configuration supplied by the caller.
type Config map[string]RuleConfig

// RuleConfig stores generic rule controls and rule-specific options.
type RuleConfig struct {
	Enabled              *bool
	Severity             string
	SuppressionSelectors []string
	Options              map[string]interface{}
}

// UnmarshalJSON supports permissive decoding while preserving unknown
// rule-specific options in Options.
func (rc *RuleConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	rc.Options = make(map[string]interface{})
	for key, value := range raw {
		switch key {
		case "enabled":
			if enabled, ok := value.(bool); ok {
				rc.Enabled = &enabled
			}
		case "severity":
			if severity, ok := value.(string); ok {
				rc.Severity = severity
			}
		case "suppress", "suppression_selectors":
			if selectors, ok := toStringSlice(value); ok {
				rc.SuppressionSelectors = selectors
			}
		default:
			rc.Options[key] = value
		}
	}

	return nil
}

func toStringSlice(value interface{}) ([]string, bool) {
	items, ok := value.([]interface{})
	if !ok {
		return nil, false
	}
	selectors := make([]string, 0, len(items))
	for _, item := range items {
		selector, ok := item.(string)
		if !ok {
			return nil, false
		}
		selectors = append(selectors, selector)
	}
	return selectors, true
}

// IsEnabled returns whether a rule should run. The default is enabled.
func (rc RuleConfig) IsEnabled() bool {
	if rc.Enabled == nil {
		return true
	}
	return *rc.Enabled
}

// Option returns a rule-specific option by key.
func (rc RuleConfig) Option(key string) (interface{}, bool) {
	v, ok := rc.Options[key]
	return v, ok
}

// SeverityOrDefault resolves severity from config with normalization.
func SeverityOrDefault(cfg Config, ruleID, fallback string) string {
	rc, ok := cfg[ruleID]
	if !ok {
		return fallback
	}
	if normalized, ok := normalizeSeverity(rc.Severity); ok {
		return normalized
	}
	return fallback
}

func normalizeSeverity(severity string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "error":
		return "error", true
	case "warn", "warning":
		return "warn", true
	case "info":
		return "info", true
	default:
		return "", false
	}
}

// Rule is the interface every lint rule must implement.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "no-duplicate-node-ids").
	ID() string
	// Run evaluates the diagram and returns any issues found.
	Run(d *model.Diagram, cfg Config) []model.Issue
}
