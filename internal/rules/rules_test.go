package rules_test

import (
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestNoDuplicateNodeIDs_Duplicate_DefaultSeverity(t *testing.T) {
	d := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}}}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 1 || issues[0].Severity != rules.SeverityError {
		t.Fatalf("expected one %s issue, got %#v", rules.SeverityError, issues)
	}
}

func TestNoDuplicateNodeIDs_SeverityOverride(t *testing.T) {
	d := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "A"}}}
	cfg := rules.Config{"no-duplicate-node-ids": {Severity: "warn"}}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, cfg)
	if len(issues) != 1 || issues[0].Severity != rules.SeverityWarn {
		t.Fatalf("expected severity override to warn, got %#v", issues)
	}
}

func TestNoDisconnectedNodes_Disconnected(t *testing.T) {
	d := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}}, Edges: []model.Edge{{From: "A", To: "B"}}}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 1 || issues[0].RuleID != "no-disconnected-nodes" {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}

func TestMaxFanout_OverLimit_DefaultSeverity(t *testing.T) {
	edges := make([]model.Edge, 6)
	for i := range edges {
		edges[i] = model.Edge{From: "A", To: string(rune('B' + i))}
	}
	issues := rules.MaxFanout{}.Run(&model.Diagram{Edges: edges}, nil)
	if len(issues) != 1 || issues[0].Severity != rules.SeverityWarn {
		t.Fatalf("expected one %s issue, got %#v", rules.SeverityWarn, issues)
	}
}

func TestMaxFanout_SeverityOverrideAndCustomLimit(t *testing.T) {
	edges := []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}}
	cfg := rules.Config{"max-fanout": {Severity: "error", Options: map[string]interface{}{"limit": 2}}}
	issues := rules.MaxFanout{}.Run(&model.Diagram{Edges: edges}, cfg)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != rules.SeverityError {
		t.Fatalf("expected severity error, got %s", issues[0].Severity)
	}
}

func TestMaxFanout_InvalidLimitsFallbackToDefault(t *testing.T) {
	edges := make([]model.Edge, 6)
	for i := range edges {
		edges[i] = model.Edge{From: "A", To: string(rune('B' + i))}
	}
	d := &model.Diagram{Edges: edges}

	tests := []struct {
		name  string
		limit interface{}
	}{
		{name: "zero", limit: 0},
		{name: "negative", limit: -1},
		{name: "fractional float", limit: 2.5},
		{name: "string", limit: "3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := rules.Config{"max-fanout": {Options: map[string]interface{}{"limit": tt.limit}}}
			issues := rules.MaxFanout{}.Run(d, cfg)
			if len(issues) != 1 || !strings.Contains(issues[0].Message, "exceeding limit of 5") {
				t.Fatalf("expected default limit fallback issue, got %#v", issues)
			}
		})
	}
}

func TestRuleConfig_EnabledOrDefault(t *testing.T) {
	if !((rules.RuleConfig{}).EnabledOrDefault()) {
		t.Fatal("expected nil enabled to default to true")
	}
	disabled := false
	if (rules.RuleConfig{Enabled: &disabled}).EnabledOrDefault() {
		t.Fatal("expected explicit enabled=false to disable rule")
	}
}

func TestRuleConfig_SeverityOrDefault(t *testing.T) {
	rc := rules.RuleConfig{Severity: " WARN "}
	if got := rc.SeverityOrDefault(rules.SeverityError); got != rules.SeverityWarn {
		t.Fatalf("expected warn severity, got %s", got)
	}
	invalid := rules.RuleConfig{Severity: "urgent"}
	if got := invalid.SeverityOrDefault(rules.SeverityError); got != rules.SeverityError {
		t.Fatalf("expected fallback error severity, got %s", got)
	}
}
