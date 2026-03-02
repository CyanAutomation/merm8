// Package rules defines the Rule interface and all built-in lint rules.
package rules

import (
	"fmt"

	"github.com/CyanAutomation/merm8/internal/model"
)

// Config holds per-rule configuration supplied by the caller.
type Config map[string]map[string]interface{}

var allowedSeverities = map[string]struct{}{
	"error": {},
	"warn":  {},
	"info":  {},
}

// resolveSeverity returns the configured severity for the given rule ID, or
// defaultSeverity when no override is present.
//
// Allowed severity values are: error, warn, info.
func resolveSeverity(ruleID string, cfg Config, defaultSeverity string) (string, error) {
	ruleConfig, ok := cfg[ruleID]
	if !ok {
		return defaultSeverity, nil
	}

	rawSeverity, ok := ruleConfig["severity"]
	if !ok {
		return defaultSeverity, nil
	}

	severity, ok := rawSeverity.(string)
	if !ok {
		return "", fmt.Errorf("invalid severity for rule %q: must be one of error, warn, info", ruleID)
	}

	if _, ok := allowedSeverities[severity]; !ok {
		return "", fmt.Errorf("invalid severity for rule %q: %q (allowed: error, warn, info)", ruleID, severity)
	}

	return severity, nil
}

// ValidateConfig validates lint configuration values shared across rules.
func ValidateConfig(cfg Config) error {
	for ruleID := range cfg {
		if _, err := resolveSeverity(ruleID, cfg, "warn"); err != nil {
			return err
		}
	}
	return nil
}

// Rule is the interface every lint rule must implement.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "no-duplicate-node-ids").
	ID() string
	// Run evaluates the diagram and returns any issues found.
	Run(d *model.Diagram, cfg Config) []model.Issue
}
