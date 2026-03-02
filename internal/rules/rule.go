// Package rules defines the Rule interface and all built-in lint rules.
package rules

import "github.com/CyanAutomation/merm8/internal/model"

// Config holds per-rule configuration supplied by the caller.
type Config map[string]map[string]interface{}

// Rule is the interface every lint rule must implement.
type Rule interface {
	// ID returns the unique rule identifier (e.g. "no-duplicate-node-ids").
	ID() string
	// Run evaluates the diagram and returns any issues found.
	Run(d *model.Diagram, cfg Config) []model.Issue
}
