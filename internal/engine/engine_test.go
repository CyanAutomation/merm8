package engine_test

import (
	"reflect"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestEngine_CleanDiagram(t *testing.T) {
	d := &model.Diagram{
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

	issues := e.Run(&model.Diagram{}, rules.Config{})
	if len(issues) != 1 {
		t.Fatalf("expected duplicate issues to be deduplicated; got %d issues: %v", len(issues), issues)
	}
}

func TestEngine_DisabledRuleIsSkipped(t *testing.T) {
	d := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "A"}}}
	e := engine.NewWithRules(rules.NoDuplicateNodeIDs{})

	issues := e.Run(d, rules.Config{"no-duplicate-node-ids": {"enabled": false}})
	if len(issues) != 0 {
		t.Fatalf("expected no issues when rule is disabled, got %v", issues)
	}
}

func TestEngine_UnsupportedDiagramTypeReturnsFallbackIssue(t *testing.T) {
	d := &model.Diagram{Type: model.DiagramTypeSequence}
	e := engine.New()

	issues := e.Run(d, rules.Config{})
	if len(issues) != 1 {
		t.Fatalf("expected exactly one fallback issue, got %d", len(issues))
	}
	if issues[0].RuleID != "unsupported-diagram-type" {
		t.Fatalf("expected fallback rule id, got %q", issues[0].RuleID)
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
