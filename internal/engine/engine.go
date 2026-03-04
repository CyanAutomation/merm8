// Package engine runs all registered rules against a Diagram and collects
// the resulting Issues.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// Engine holds a set of rules and runs them in order.
type Engine struct {
	rules []rules.Rule
	sink  InstrumentationSink
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
	return &Engine{rules: registeredRules, sink: NoopInstrumentationSink{}}
}

// SetInstrumentationSink configures an optional metrics sink used for rule
// execution instrumentation.
func (e *Engine) SetInstrumentationSink(sink InstrumentationSink) {
	if sink == nil {
		e.sink = NoopInstrumentationSink{}
		return
	}
	e.sink = sink
}

// Run executes every rule against d and returns all issues found.
func (e *Engine) Run(d *model.Diagram, cfg rules.Config) []model.Issue {
	return e.RunWithInstrumentation(d, cfg, e.sink)
}

// RunWithInstrumentation executes rules and emits per-rule metrics into sink.
// A nil sink uses a no-op implementation.
func (e *Engine) RunWithInstrumentation(d *model.Diagram, cfg rules.Config, sink InstrumentationSink) []model.Issue {
	if sink == nil {
		sink = NoopInstrumentationSink{}
	}

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

		ruleStart := time.Now()
		ruleIssues := r.Run(d, normalizedCfg)
		ruleDuration := time.Since(ruleStart)
		sink.RecordRuleMetrics(RuleMetrics{
			RuleID:          r.ID(),
			Executions:      1,
			IssuesEmitted:   len(ruleIssues),
			TotalDurationNS: ruleDuration.Nanoseconds(),
		})

		issues = append(issues, ruleIssues...)
	}

	if len(issues) == 0 {
		return []model.Issue{}
	}

	issues = applySuppressions(issues, d.Suppressions)
	if len(issues) == 0 {
		return []model.Issue{}
	}

	issues = applyConfigSelectors(issues, d, normalizedCfg)
	if len(issues) == 0 {
		return []model.Issue{}
	}

	sortIssues(issues)
	issues = dedupeIssues(issues)
	populateFingerprints(issues)
	return issues
}

// NormalizeConfig normalizes config key aliases and validates rule IDs against
// this engine's registered rules.
func (e *Engine) NormalizeConfig(cfg rules.Config) (rules.Config, error) {
	return rules.NormalizeConfig(cfg, e.KnownRuleIDs())
}

// DiagramFamilies returns sorted unique diagram families supported by the
// currently registered rule set.
func (e *Engine) DiagramFamilies() []model.DiagramFamily {
	set := map[model.DiagramFamily]struct{}{}
	for _, rule := range e.rules {
		familyRule, ok := rule.(rules.DiagramFamilyRule)
		if !ok {
			continue
		}
		for _, family := range familyRule.Families() {
			if family == model.DiagramFamilyUnknown {
				continue
			}
			set[family] = struct{}{}
		}
	}

	families := make([]model.DiagramFamily, 0, len(set))
	for _, candidate := range []model.DiagramFamily{
		model.DiagramFamilyFlowchart,
		model.DiagramFamilySequence,
		model.DiagramFamilyClass,
		model.DiagramFamilyER,
		model.DiagramFamilyState,
	} {
		if _, ok := set[candidate]; ok {
			families = append(families, candidate)
		}
	}
	return families
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
			if suppressionAppliesToContext(issue, suppression) {
				return true
			}
		case "next-line":
			if issue.Line != nil && *issue.Line == suppression.TargetLine && suppressionAppliesToContext(issue, suppression) {
				return true
			}
		}
	}
	return false
}

func suppressionMatchesRule(issueRuleID, suppressedRuleID string) bool {
	return suppressedRuleID == "all" || suppressedRuleID == issueRuleID
}

func suppressionAppliesToContext(issue model.Issue, suppression model.SuppressionDirective) bool {
	if suppression.SubgraphID == "" {
		return true
	}
	return issue.Context != nil && issue.Context.SubgraphID == suppression.SubgraphID
}

var (
	maxFanoutNodePattern        = regexp.MustCompile(`^node "([^"]+)" has fanout`)
	disconnectedNodePattern     = regexp.MustCompile(`^node is disconnected: (.+)$`)
	duplicateNodeIDPattern      = regexp.MustCompile(`^duplicate node ID: (.+)$`)
	suppressionSelectorPrefixes = map[string]struct{}{"node": {}, "subgraph": {}, "rule": {}}
)

func applyConfigSelectors(issues []model.Issue, d *model.Diagram, cfg rules.Config) []model.Issue {
	filtered := make([]model.Issue, 0, len(issues))
	nodeLineIndex := buildNodeLineIndex(d)
	for _, issue := range issues {
		selectors := suppressionSelectorsForRule(cfg, issue.RuleID)
		if len(selectors) == 0 {
			filtered = append(filtered, issue)
			continue
		}

		if issueMatchesAnySelector(issue, selectors, nodeLineIndex) {
			continue
		}
		filtered = append(filtered, issue)
	}
	return filtered
}

func suppressionSelectorsForRule(cfg rules.Config, ruleID string) []string {
	ruleCfg, ok := cfg[ruleID]
	if !ok {
		return nil
	}
	raw, ok := ruleCfg["suppression-selectors"]
	if !ok {
		return nil
	}

	switch selectors := raw.(type) {
	case []string:
		return selectors
	case []interface{}:
		out := make([]string, 0, len(selectors))
		for _, selector := range selectors {
			value, ok := selector.(string)
			if !ok {
				continue
			}
			out = append(out, value)
		}
		return out
	default:
		return nil
	}
}

func issueMatchesAnySelector(issue model.Issue, selectors []string, nodeLineIndex map[int][]string) bool {
	matchedInclude := false
	matchedExclude := false

	for _, raw := range selectors {
		parsed, ok := parseSelector(raw)
		if !ok {
			continue
		}

		matched := false
		switch parsed.Prefix {
		case "rule":
			matched = issue.RuleID == parsed.Value
		case "subgraph":
			matched = issue.Context != nil && issue.Context.SubgraphID == parsed.Value
		case "node":
			for _, nodeID := range linkedNodeIDs(issue, nodeLineIndex) {
				if nodeID == parsed.Value {
					matched = true
					break
				}
			}
		}

		if !matched {
			continue
		}

		if parsed.Negated {
			matchedExclude = true
			continue
		}

		matchedInclude = true
	}

	return matchedInclude && !matchedExclude
}

type suppressionSelector struct {
	Negated bool
	Prefix  string
	Value   string
}

func parseSelector(raw string) (suppressionSelector, bool) {
	selector := strings.TrimSpace(raw)
	if selector == "" {
		return suppressionSelector{}, false
	}

	negated := false
	if strings.HasPrefix(selector, "!") {
		negated = true
		selector = strings.TrimSpace(strings.TrimPrefix(selector, "!"))
	}

	prefixRaw, valueRaw, ok := splitSelector(selector)
	if !ok {
		return suppressionSelector{}, false
	}

	prefix := strings.ToLower(strings.TrimSpace(prefixRaw))
	if _, valid := suppressionSelectorPrefixes[prefix]; !valid {
		return suppressionSelector{}, false
	}

	value := strings.TrimSpace(unescapeSelectorValue(valueRaw))
	if value == "" {
		return suppressionSelector{}, false
	}

	return suppressionSelector{Negated: negated, Prefix: prefix, Value: value}, true
}

func splitSelector(selector string) (prefix string, value string, ok bool) {
	var prev rune
	for i, r := range selector {
		if r == ':' && prev != '\\' {
			return selector[:i], selector[i+1:], true
		}
		prev = r
	}

	return "", "", false
}

func unescapeSelectorValue(value string) string {
	var b strings.Builder
	b.Grow(len(value))

	escaped := false
	for _, r := range value {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}

		if r == '\\' {
			escaped = true
			continue
		}

		b.WriteRune(r)
	}

	if escaped {
		b.WriteRune('\\')
	}

	return b.String()
}

func buildNodeLineIndex(d *model.Diagram) map[int][]string {
	index := map[int][]string{}
	for _, node := range d.Nodes {
		if node.Line == nil {
			continue
		}
		index[*node.Line] = append(index[*node.Line], node.ID)
	}
	return index
}

func linkedNodeIDs(issue model.Issue, nodeLineIndex map[int][]string) []string {
	if issue.Line != nil {
		if ids, ok := nodeLineIndex[*issue.Line]; ok {
			return ids
		}
	}

	if matches := maxFanoutNodePattern.FindStringSubmatch(issue.Message); len(matches) == 2 {
		return []string{matches[1]}
	}
	if matches := disconnectedNodePattern.FindStringSubmatch(issue.Message); len(matches) == 2 {
		return []string{matches[1]}
	}
	if matches := duplicateNodeIDPattern.FindStringSubmatch(issue.Message); len(matches) == 2 {
		return []string{matches[1]}
	}

	return nil
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
		if compareIssueContext(left.Context, right.Context) != 0 {
			return compareIssueContext(left.Context, right.Context) < 0
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
		compareIntPtr(left.Column, right.Column) == 0 &&
		compareIssueContext(left.Context, right.Context) == 0
}

func compareIssueContext(left, right *model.IssueContext) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return 1
	}
	if right == nil {
		return -1
	}
	if left.SubgraphID < right.SubgraphID {
		return -1
	}
	if left.SubgraphID > right.SubgraphID {
		return 1
	}
	if left.SubgraphLabel < right.SubgraphLabel {
		return -1
	}
	if left.SubgraphLabel > right.SubgraphLabel {
		return 1
	}
	return 0
}

func populateFingerprints(issues []model.Issue) {
	for i := range issues {
		issues[i].Fingerprint = issueFingerprint(issues[i])
	}
}

func issueFingerprint(issue model.Issue) string {
	contextID := ""
	contextLabel := ""
	if issue.Context != nil {
		contextID = issue.Context.SubgraphID
		contextLabel = issue.Context.SubgraphLabel
	}
	signature := fmt.Sprintf("%s|%s|%s|%d|%d|%s|%s", issue.RuleID, issue.Severity, issue.Message, intValue(issue.Line), intValue(issue.Column), contextID, contextLabel)
	hash := sha256.Sum256([]byte(signature))
	return hex.EncodeToString(hash[:])
}

func intValue(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func severityPriority(severity string) int {
	switch severity {
	case "error":
		return 0
	case "warning", "warn":
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
