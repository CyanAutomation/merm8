// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// Engine holds a set of rules and runs them in order.
type Engine struct {
	rules []rules.Rule
}

// New returns an Engine pre-loaded with the default rule set.
func New() *Engine {
	return &Engine{
		rules: []rules.Rule{
			rules.NoDuplicateNodeIDs{},
			rules.NoDisconnectedNodes{},
			rules.MaxFanout{},
		},
	}
}

// Run executes every rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	var issues []model.Issue
	for _, r := range e.rules {
		issues = append(issues, r.Run(d, cfg)...)
	}
	if issues == nil {
		issues = []model.Issue{}
	}
	return issues
}
