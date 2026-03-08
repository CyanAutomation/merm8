package engine_test

import (
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

type capturingSink struct {
	metrics []engine.RuleMetrics
}

func (s *capturingSink) RecordRuleMetrics(metrics engine.RuleMetrics) {
	s.metrics = append(s.metrics, metrics)
}

type countingSink struct {
	count atomic.Int64
}

func (s *countingSink) RecordRuleMetrics(_ engine.RuleMetrics) {
	s.count.Add(1)
}

type supportedRule struct{}

func (supportedRule) ID() string { return "supported-rule" }

func (supportedRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (supportedRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	return []model.Issue{{RuleID: "supported-rule", Severity: "warning", Message: "supported issue"}}
}

type namespacedCoreRule struct{}

func (namespacedCoreRule) ID() string { return "core/not-a-built-in" }

func (namespacedCoreRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue { return nil }

type namespacedCustomRule struct{}

func (namespacedCustomRule) ID() string { return "custom/acme/max-fanout" }

func (namespacedCustomRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue { return nil }

type legacyCustomRule struct{}

type collidingLegacyRule struct{}

func (collidingLegacyRule) ID() string { return "max-fanout" }

func (collidingLegacyRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue { return nil }

func (legacyCustomRule) ID() string { return "acme-custom" }

func (legacyCustomRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue { return nil }

type unsupportedRule struct{}

func (unsupportedRule) ID() string { return "unsupported-rule" }

func (unsupportedRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilySequence}
}

func (unsupportedRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	return []model.Issue{{RuleID: "unsupported-rule", Severity: "warning", Message: "unsupported issue"}}
}

func TestEngine_NewWithRules_RejectsReservedCoreNamespaceMisuse(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic for invalid core namespace registration")
		}
		if !strings.Contains(recovered.(string), "core/ namespace is reserved") {
			t.Fatalf("expected reserved core namespace panic message, got %v", recovered)
		}
	}()

	engine.NewWithRules(namespacedCoreRule{})
}

func TestEngine_NewWithRules_RejectsCanonicalCollisions(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic for duplicate canonical rule id")
		}
		if !strings.Contains(recovered.(string), "duplicate rule registration") {
			t.Fatalf("expected duplicate registration panic message, got %v", recovered)
		}
	}()

	engine.NewWithRules(rules.MaxFanout{}, collidingLegacyRule{})
}

func TestEngine_NewWithRules_AllowsLegacyCustomRuleDuringTransition(t *testing.T) {
	e := engine.NewWithRules(legacyCustomRule{})
	if len(e.KnownRuleIDs()) != 1 {
		t.Fatalf("expected one registered rule, got %d", len(e.KnownRuleIDs()))
	}
}

func TestEngine_DiagramFamilies_FromRegisteredRules(t *testing.T) {
	e := engine.New()
	got := e.DiagramFamilies()
	want := []model.DiagramFamily{
		model.DiagramFamilyFlowchart,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected diagram families %v, got %v", want, got)
	}
}

func TestEngine_CleanDiagram(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	e := engine.New()
	issues := e.Run(d, rules.Config{})
	if len(issues) != 0 {
		t.Fatalf("expected no issues for clean diagram, got %v", issues)
	}
	if issues == nil {
		t.Fatal("Run should never return a nil slice")
	}
}

func TestEngine_DuplicateAndDisconnected(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	e := engine.New()
	issues := e.Run(d, rules.Config{})
	if len(issues) < 2 {
		t.Fatalf("expected at least 2 issues, got %d: %v", len(issues), issues)
	}
}

func TestEngine_StableOrderingAcrossRuns(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
	}
	cfg := rules.Config{"max-fanout": map[string]any{"limit": 1}}
	e := engine.New()

	first := e.Run(d, cfg)
	for i := 0; i < 30; i++ {
		next := e.Run(d, cfg)
		if !reflect.DeepEqual(first, next) {
			t.Fatalf("expected stable issue ordering across runs, run 0=%v run %d=%v", first, i+1, next)
		}
	}
}

func TestEngine_StableOrderingAcrossRuleRegistrationOrder(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
	}
	cfg := rules.Config{"max-fanout": map[string]any{"limit": 1}}

	defaultOrder := engine.NewWithRules(
		rules.NoDuplicateNodeIDs{},
		rules.NoDisconnectedNodes{},
		rules.MaxFanout{},
	)
	reversedOrder := engine.NewWithRules(
		rules.MaxFanout{},
		rules.NoDisconnectedNodes{},
		rules.NoDuplicateNodeIDs{},
	)

	defaultIssues := defaultOrder.Run(d, cfg)
	reversedIssues := reversedOrder.Run(d, cfg)
	if !reflect.DeepEqual(defaultIssues, reversedIssues) {
		t.Fatalf("expected identical sorted issues despite rule registration changes: default=%v reversed=%v", defaultIssues, reversedIssues)
	}
}

type duplicateIssueRule struct{}

func intPtr(v int) *int { return &v }

func (duplicateIssueRule) ID() string { return "duplicate-issue-rule" }

func (duplicateIssueRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	issue := model.Issue{
		RuleID:   "duplicate-issue-rule",
		Severity: "warning",
		Message:  "duplicate issue",
		Line:     intPtr(2),
		Column:   intPtr(4),
	}
	return []model.Issue{issue, issue}
}

func TestEngine_DeduplicatesEquivalentIssues(t *testing.T) {
	e := engine.NewWithRules(duplicateIssueRule{})

	issues := e.Run(&model.Diagram{Type: model.DiagramTypeFlowchart}, rules.Config{})
	if len(issues) != 1 {
		t.Fatalf("expected duplicate issues to be deduplicated; got %d issues: %v", len(issues), issues)
	}
}

func TestEngine_DisabledRuleIsSkipped(t *testing.T) {
	d := &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}, {ID: "A"}}}
	e := engine.NewWithRules(rules.NoDuplicateNodeIDs{})

	issues := e.Run(d, rules.Config{"no-duplicate-node-ids": {"enabled": false}})
	if len(issues) != 0 {
		t.Fatalf("expected no issues when rule is disabled, got %v", issues)
	}
}

// The engine still emits a fallback issue for unsupported diagram families when used directly.
func TestEngine_UnsupportedDiagramTypeReturnsFallbackIssueForDirectEngineUse(t *testing.T) {
	d := &model.Diagram{Type: model.DiagramTypeUnknown}
	e := engine.New()

	issues := e.Run(d, rules.Config{})
	if len(issues) != 1 {
		t.Fatalf("expected exactly one fallback issue, got %d", len(issues))
	}
	if issues[0].RuleID != "unsupported-diagram-type" {
		t.Fatalf("expected fallback rule id, got %q", issues[0].RuleID)
	}
}

type nestedContextIssueRule struct{}

func (nestedContextIssueRule) ID() string { return "nested-context-issue-rule" }

func (nestedContextIssueRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line2 := 2
	line4 := 4
	line5 := 5
	line6 := 6
	line7 := 7
	line8 := 8
	return []model.Issue{
		{RuleID: "nested-context-issue-rule", Severity: "warning", Message: "outer node issue", Line: &line2},
		{RuleID: "nested-context-issue-rule", Severity: "warning", Message: "nested target issue", Line: &line4, Context: &model.IssueContext{SubgraphID: "inner"}},
		{RuleID: "nested-context-issue-rule", Severity: "warning", Message: "nested sibling issue", Line: &line5, Context: &model.IssueContext{SubgraphID: "inner"}},
		{RuleID: "nested-context-issue-rule", Severity: "warning", Message: "outer sibling issue", Line: &line8, Context: &model.IssueContext{SubgraphID: "outer"}},
		{RuleID: "other-rule", Severity: "warning", Message: "other nested target issue", Line: &line4, Context: &model.IssueContext{SubgraphID: "inner"}},
		{RuleID: "other-rule", Severity: "warning", Message: "other nested sibling issue", Line: &line5, Context: &model.IssueContext{SubgraphID: "inner"}},
		{RuleID: "other-rule", Severity: "warning", Message: "outside nested issue", Line: &line7},
		{RuleID: "other-rule", Severity: "warning", Message: "inside outer not inner", Line: &line6, Context: &model.IssueContext{SubgraphID: "outer"}},
	}
}

type fixedLineIssueRule struct{}

func (fixedLineIssueRule) ID() string { return "fixed-line-issue-rule" }

func (fixedLineIssueRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	lineOne := 1
	lineTwo := 2
	return []model.Issue{
		{RuleID: "fixed-line-issue-rule", Severity: "warning", Message: "line one issue", Line: &lineOne},
		{RuleID: "fixed-line-issue-rule", Severity: "warning", Message: "line two issue", Line: &lineTwo},
	}
}

func TestEngine_NextLineSuppressionSuppressesOnlyTargetedRule(t *testing.T) {
	e := engine.NewWithRules(fixedLineIssueRule{})
	d := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "fixed-line-issue-rule",
			Scope:      "next-line",
			Line:       1,
			TargetLine: 2,
		}},
	}

	issues := e.Run(d, rules.Config{})
	if len(issues) != 1 {
		t.Fatalf("expected one remaining issue after next-line suppression, got %#v", issues)
	}
	if issues[0].RuleID != "fixed-line-issue-rule" || issues[0].Line == nil || *issues[0].Line != 1 {
		t.Fatalf("expected line-1 issue to remain, got %#v", issues)
	}
}

func TestEngine_NextLineSuppressionWithRulePopulatedLocation(t *testing.T) {
	nodeLine := 2
	nodeColumn := 3
	e := engine.NewWithRules(rules.NoDisconnectedNodes{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A", Line: &nodeLine, Column: &nodeColumn}, {ID: "B"}},
		Edges: []model.Edge{{From: "B", To: "B"}},
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "no-disconnected-nodes",
			Scope:      "next-line",
			Line:       1,
			TargetLine: 2,
		}},
	}

	issues := e.Run(d, rules.Config{})
	if len(issues) != 0 {
		t.Fatalf("expected next-line suppression to match populated issue line, got %#v", issues)
	}
}

func TestEngine_NextLineSuppressionNonMatchingRuleDoesNotHideIssue(t *testing.T) {
	e := engine.NewWithRules(fixedLineIssueRule{})
	d := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "some-other-rule",
			Scope:      "next-line",
			Line:       1,
			TargetLine: 2,
		}},
	}

	issues := e.Run(d, rules.Config{})
	if len(issues) != 2 {
		t.Fatalf("expected non-matching suppression to keep all issues, got %#v", issues)
	}
}

func TestEngine_NextLineSuppressionNestedRegionSuppressesOnlyImmediateTargetLine(t *testing.T) {
	e := engine.NewWithRules(nestedContextIssueRule{})
	d := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "nested-context-issue-rule",
			Scope:      "next-line",
			Line:       3,
			TargetLine: 4,
			SubgraphID: "inner",
		}},
	}

	issues := e.Run(d, rules.Config{})
	if len(issues) != 7 {
		t.Fatalf("expected only one nested target issue to be suppressed, got %#v", issues)
	}
	for _, issue := range issues {
		if issue.RuleID == "nested-context-issue-rule" && issue.Line != nil && *issue.Line == 4 {
			t.Fatalf("expected nested target issue at line 4 to be suppressed, got %#v", issues)
		}
	}
	foundSibling := false
	for _, issue := range issues {
		if issue.RuleID == "nested-context-issue-rule" && issue.Line != nil && *issue.Line == 5 {
			foundSibling = true
			break
		}
	}
	if !foundSibling {
		t.Fatalf("expected sibling issue in nested region to remain visible, got %#v", issues)
	}
}

func TestEngine_NextLineSuppressionNestedRegionKeepsSiblingAndOutsideIssuesVisible(t *testing.T) {
	e := engine.NewWithRules(nestedContextIssueRule{})
	d := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "nested-context-issue-rule",
			Scope:      "next-line",
			Line:       3,
			TargetLine: 4,
			SubgraphID: "inner",
		}},
	}

	issues := e.Run(d, rules.Config{})
	for _, issue := range issues {
		if issue.RuleID == "nested-context-issue-rule" && issue.Line != nil && *issue.Line == 8 {
			return
		}
	}
	t.Fatalf("expected outside-region issue to remain visible, got %#v", issues)
}

func TestEngine_NestedRuleSpecificSuppressionDoesNotAffectOtherRules(t *testing.T) {
	e := engine.NewWithRules(nestedContextIssueRule{})
	d := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Suppressions: []model.SuppressionDirective{{
			RuleID:     "nested-context-issue-rule",
			Scope:      "next-line",
			Line:       3,
			TargetLine: 4,
			SubgraphID: "inner",
		}},
	}

	issues := e.Run(d, rules.Config{})
	for _, issue := range issues {
		if issue.RuleID == "other-rule" && issue.Line != nil && *issue.Line == 4 {
			return
		}
	}
	t.Fatalf("expected rule-specific suppression to keep other-rule issue visible at nested target line, got %#v", issues)
}

func TestEngine_FingerprintStableAcrossRuns(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "A"}},
	}
	e := engine.NewWithRules(rules.NoDisconnectedNodes{})
	first := e.Run(d, rules.Config{})
	if len(first) != 1 || first[0].Fingerprint == "" {
		t.Fatalf("expected issue with fingerprint, got %#v", first)
	}

	for i := 0; i < 10; i++ {
		next := e.Run(d, rules.Config{})
		if len(next) != 1 {
			t.Fatalf("expected one issue, got %#v", next)
		}
		if first[0].Fingerprint != next[0].Fingerprint {
			t.Fatalf("expected stable fingerprint, run0=%s run%d=%s", first[0].Fingerprint, i+1, next[0].Fingerprint)
		}
	}
}

func TestEngine_FingerprintDiffersForMateriallyDifferentIssues(t *testing.T) {
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "A"}},
	}
	e := engine.NewWithRules(rules.NoDisconnectedNodes{})
	issuesA := e.Run(d, rules.Config{})
	issuesB := e.Run(d, rules.Config{"no-disconnected-nodes": {"severity": "info"}})
	if len(issuesA) != 1 || len(issuesB) != 1 {
		t.Fatalf("expected one issue in each run, got A=%#v B=%#v", issuesA, issuesB)
	}
	if issuesA[0].Fingerprint == issuesB[0].Fingerprint {
		t.Fatalf("expected fingerprints to differ when severity changes, both=%s", issuesA[0].Fingerprint)
	}
}

func TestEngine_RunWithInstrumentation_RecordsEnabledAndSupportedRulesOnly(t *testing.T) {
	e := engine.NewWithRules(supportedRule{}, unsupportedRule{})
	d := &model.Diagram{Type: model.DiagramTypeFlowchart}
	sink := &capturingSink{}

	issues := e.RunWithInstrumentation(d, rules.Config{"supported-rule": {"enabled": true}, "unsupported-rule": {"enabled": true}}, sink)
	if len(issues) != 1 || issues[0].RuleID != "supported-rule" {
		t.Fatalf("expected only supported rule issue, got %#v", issues)
	}

	if len(sink.metrics) != 1 {
		t.Fatalf("expected one recorded metric, got %#v", sink.metrics)
	}
	if sink.metrics[0].RuleID != "supported-rule" {
		t.Fatalf("expected supported-rule metric, got %#v", sink.metrics[0])
	}
	if sink.metrics[0].Executions != 1 || sink.metrics[0].IssuesEmitted != 1 {
		t.Fatalf("expected execution/issue counts of 1, got %#v", sink.metrics[0])
	}
	if sink.metrics[0].TotalDurationNS < 0 {
		t.Fatalf("expected non-negative duration, got %#v", sink.metrics[0])
	}
}

func TestEngine_RunWithInstrumentation_SkipsDisabledRules(t *testing.T) {
	e := engine.NewWithRules(supportedRule{})
	d := &model.Diagram{Type: model.DiagramTypeFlowchart}
	sink := &capturingSink{}

	issues := e.RunWithInstrumentation(d, rules.Config{"supported-rule": {"enabled": false}}, sink)
	if len(issues) != 0 {
		t.Fatalf("expected no issues from disabled rule, got %#v", issues)
	}
	if len(sink.metrics) != 0 {
		t.Fatalf("expected no recorded metrics for disabled rule, got %#v", sink.metrics)
	}
}

func TestEngine_Run_ConcurrentSinkUpdates(t *testing.T) {
	e := engine.NewWithRules(supportedRule{})
	d := &model.Diagram{Type: model.DiagramTypeFlowchart}

	const workers = 6
	const iterations = 200

	errCh := make(chan []model.Issue, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				e.SetInstrumentationSink(&countingSink{})
				issues := e.Run(d, rules.Config{})
				if len(issues) != 1 || issues[0].RuleID != "supported-rule" {
					errCh <- issues
					return
				}
				e.SetInstrumentationSink(nil)
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for issues := range errCh {
		t.Fatalf("expected supported-rule issue, got %#v", issues)
	}
}

type contextIssueRule struct{}

func (contextIssueRule) ID() string { return "context-issue-rule" }

func (contextIssueRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line := 3
	return []model.Issue{{
		RuleID:   "context-issue-rule",
		Severity: "warning",
		Message:  "context issue",
		Line:     &line,
		Context:  &model.IssueContext{SubgraphID: "cluster-1", SubgraphLabel: "Core"},
	}}
}

func TestEngine_ConfigSuppressionSelectors_Node(t *testing.T) {
	line := 2
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A", Line: &line}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"node:A"}}})
	if len(issues) != 0 {
		t.Fatalf("expected node selector to suppress max-fanout issue, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_RuleAndSubgraph(t *testing.T) {
	eRule := engine.NewWithRules(rules.MaxFanout{})
	dRule := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}
	issues := eRule.Run(dRule, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"rule:max-fanout"}}})
	if len(issues) != 0 {
		t.Fatalf("expected rule selector to suppress issue, got %#v", issues)
	}

	eSubgraph := engine.NewWithRules(contextIssueRule{})
	dSubgraph := &model.Diagram{Type: model.DiagramTypeFlowchart}
	issues = eSubgraph.Run(dSubgraph, rules.Config{"context-issue-rule": {"suppression-selectors": []string{"subgraph:cluster-1"}}})
	if len(issues) != 0 {
		t.Fatalf("expected subgraph selector to suppress issue, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_MalformedRejectsConfig(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"", "node", "node:", "unknown:A"}}})
	if len(issues) != 0 {
		t.Fatalf("expected malformed selectors to fail config validation and produce no issues, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_NegationRequiresInclude(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"!rule:max-fanout"}}})
	if len(issues) != 1 {
		t.Fatalf("expected negation-only selectors to keep issue, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_NegationOverridesInclude(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}, {ID: "E"}, {ID: "F"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "D", To: "E"}, {From: "D", To: "F"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"node:A", "!node:D"}}})
	if len(issues) != 1 {
		t.Fatalf("expected mixed selectors to suppress node A issue and keep node D issue, got %#v", issues)
	}
	if !strings.Contains(issues[0].Message, `node "D" has fanout 2`) {
		t.Fatalf("expected unsuppressed issue for node D, got %#v", issues[0])
	}
}

func TestEngine_ConfigSuppressionSelectors_NegatedRuleSelectorBehavior(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"rule:max-fanout", "!rule:max-fanout"}}})
	if len(issues) != 1 {
		t.Fatalf("expected negated rule selector to override matching include rule selector, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_MalformedNegationRejectsConfig(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"rule:max-fanout", "! node:A", "!", "!\tnode:A"}}})
	if len(issues) != 0 {
		t.Fatalf("expected malformed negated selectors to fail config validation and produce no issues, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_WhitespaceInSelectorRejectsConfig(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"node: A", "subgraph:payments team"}}})
	if len(issues) != 0 {
		t.Fatalf("expected whitespace-containing selectors to fail config validation and produce no issues, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_UnknownRuleSelectorDoesNotMatch(t *testing.T) {
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{"rule:unknown-rule"}}})
	if len(issues) != 1 {
		t.Fatalf("expected unknown rule selector value to not suppress issue, got %#v", issues)
	}
}

func TestEngine_ConfigSuppressionSelectors_NodeIDContainingColon(t *testing.T) {
	line := 2
	e := engine.NewWithRules(rules.MaxFanout{})
	d := &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "team:alpha", Line: &line}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{{From: "team:alpha", To: "B"}, {From: "team:alpha", To: "C"}, {From: "team:alpha", To: "D"}},
	}

	issues := e.Run(d, rules.Config{"max-fanout": {"limit": 1, "suppression-selectors": []string{`node:team:alpha`}}})
	if len(issues) != 0 {
		t.Fatalf("expected selector containing colon in value to suppress node issue, got %#v", issues)
	}
}
