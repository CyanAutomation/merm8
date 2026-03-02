// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"sort"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// Engine holds a set of rules and runs them in order.
type Engine struct {
	rules []rules.Rule
}

// New returns an Engine pre-loaded with the default rule set.
func New() *Engine {
	return NewWithRules(
		rules.NoDuplicateNodeIDs{},
		rules.NoDisconnectedNodes{},
		rules.MaxFanout{},
	)
}

// NewWithRules returns an Engine configured with the provided rule set.
func NewWithRules(registeredRules ...rules.Rule) *Engine {
	return &Engine{rules: registeredRules}
}

// Run executes every rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	var issues []model.Issue
	for _, r := range e.rules {
		issues = append(issues, r.Run(d, cfg)...)
	}

	if len(issues) == 0 {
		return []model.Issue{}
	}

	sortIssues(issues)
	issues = dedupeIssues(issues)
	return issues
}

func sortIssues(issues []model.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := issues[i]
		right := issues[j]
		if severityPriority(left.Severity) != severityPriority(right.Severity) {
			return severityPriority(left.Severity) < severityPriority(right.Severity)
		}
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		if left.Column != right.Column {
			return left.Column < right.Column
		}
		return left.Message < right.Message
	})
}

func dedupeIssues(issues []model.Issue) []model.Issue {
	result := issues[:0]
	for _, issue := range issues {
		if len(result) > 0 && sameIssue(result[len(result)-1], issue) {
			continue
		}
		result = append(result, issue)
	}
	return result
}

func sameIssue(left, right model.Issue) bool {
	return left.RuleID == right.RuleID &&
		left.Severity == right.Severity &&
		left.Message == right.Message &&
		left.Line == right.Line &&
		left.Column == right.Column
}

func severityPriority(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warn":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}
