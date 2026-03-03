// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"log"
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
	registeredRules := make([]rules.Rule, 0)
	registeredRules = append(registeredRules, rules.FlowchartRules()...)
	registeredRules = append(registeredRules, rules.SequenceRules()...)
	registeredRules = append(registeredRules, rules.ClassRules()...)
	registeredRules = append(registeredRules, rules.ERRules()...)
	registeredRules = append(registeredRules, rules.StateRules()...)
	return NewWithRules(registeredRules...)
}

// NewWithRules returns an Engine configured with the provided rule set.
func NewWithRules(registeredRules ...rules.Rule) *Engine {
	return &Engine{rules: registeredRules}
}

// Run executes every rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	normalizedCfg, err := e.NormalizeConfig(cfg)
	if err != nil {
		log.Printf("warn: invalid lint config ignored: %v", err)
		return []model.Issue{}
	}

	family := d.Type.Family()
	if family != model.DiagramFamilyFlowchart {
		return []model.Issue{unsupportedDiagramTypeIssue(d)}
	}

	var issues []model.Issue
	for _, r := range e.rules {
		if !rules.SupportsDiagramFamily(r, family) {
			continue
		}
		if !rules.RuleEnabled(r.ID(), normalizedCfg) {
			continue
		}
		issues = append(issues, r.Run(d, normalizedCfg)...)
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

// NormalizeConfig normalizes config key aliases and validates rule IDs against
// this engine's registered rules.
func (e *Engine) NormalizeConfig(cfg rules.Config) (rules.Config, error) {
	return rules.NormalizeConfig(cfg, e.KnownRuleIDs())
}

// KnownRuleIDs returns the set of rule IDs registered on this engine.
func (e *Engine) KnownRuleIDs() map[string]struct{} {
	knownRuleIDs := make(map[string]struct{}, len(e.rules))
	for _, rule := range e.rules {
		knownRuleIDs[rule.ID()] = struct{}{}
	}
	return knownRuleIDs
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

func unsupportedDiagramTypeIssue(d *model.Diagram) model.Issue {
	return model.Issue{
		RuleID:   "unsupported-diagram-type",
		Severity: "info",
		Message:  "diagram type \"" + string(d.Type) + "\" is parsed but lint rules are not available yet",
	}
}
