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

	issues = applySuppressions(issues, d.Suppressions)
	if len(issues) == 0 {
		return []model.Issue{}
	}

	sortIssues(issues)
	issues = dedupeIssues(issues)
	return issues
}

func applySuppressions(issues []model.Issue, suppressions []model.SuppressionDirective) []model.Issue {
	if len(suppressions) == 0 {
		return issues
	}

	filtered := make([]model.Issue, 0, len(issues))
	for _, issue := range issues {
		if isSuppressed(issue, suppressions) {
			continue
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

func isSuppressed(issue model.Issue, suppressions []model.SuppressionDirective) bool {
	for _, suppression := range suppressions {
		if !suppressionMatchesRule(issue.RuleID, suppression.RuleID) {
			continue
		}

		switch suppression.Scope {
		case "file":
			return true
		case "next-line":
			if issue.Line != nil && *issue.Line == suppression.TargetLine {
				return true
			}
		}
	}
	return false
}

func suppressionMatchesRule(issueRuleID, suppressedRuleID string) bool {
	return suppressedRuleID == "all" || suppressedRuleID == issueRuleID
}

func sortIssues(issues []model.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := issues[i]
		right := issues[j]
		if severityPriority(left.Severity) != severityPriority(right.Severity) {
			return severityPriority(left.Severity) < severityPriority(right.Severity)
		}
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if compareIntPtr(left.Line, right.Line) != 0 {
			return compareIntPtr(left.Line, right.Line) < 0
		}
		if compareIntPtr(left.Column, right.Column) != 0 {
			return compareIntPtr(left.Column, right.Column) < 0
		}
		return left.Message < right.Message
	})
}

func compareIntPtr(left, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
func intPtrValue(v *int) int {
	if v == nil {
		return -1
	}
	return *v
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
		compareIntPtr(left.Line, right.Line) == 0 &&
		compareIntPtr(left.Column, right.Column) == 0
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
