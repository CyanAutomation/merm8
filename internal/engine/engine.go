// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"fmt"
	"sort"
	"strings"

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

// NormalizeConfig validates and normalizes config keys for known rules.
// Unknown rule IDs are rejected.
func (e *Engine) NormalizeConfig(cfg rules.Config) (rules.Config, error) {
	if cfg == nil {
		return rules.Config{}, nil
	}

	known := make(map[string]struct{}, len(e.rules))
	for _, r := range e.rules {
		known[r.ID()] = struct{}{}
	}

	normalized := make(rules.Config, len(cfg))
	var unknown []string
	for ruleID, ruleCfg := range cfg {
		if _, ok := known[ruleID]; !ok {
			unknown = append(unknown, ruleID)
			continue
		}
		normalized[ruleID] = ruleCfg
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown rule ids in config: %s", strings.Join(unknown, ", "))
	}

	return normalized, nil
}

// Run executes every enabled rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	var issues []model.Issue
	for _, r := range e.rules {
		ruleCfg, hasCfg := cfg[r.ID()]
		if hasCfg && !ruleCfg.IsEnabled() {
			continue
		}
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
		if left.RuleID != right.RuleID {
			return left.RuleID < right.RuleID
		}
		if compareOptionalInt(left.Line, right.Line) != 0 {
			return compareOptionalInt(left.Line, right.Line) < 0
		}
		if compareOptionalInt(left.Column, right.Column) != 0 {
			return compareOptionalInt(left.Column, right.Column) < 0
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

func compareOptionalInt(left, right *int) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	if *left < *right {
		return -1
	}
	if *left > *right {
		return 1
	}
	return 0
}
