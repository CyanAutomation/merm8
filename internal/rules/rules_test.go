package rules_test

import (
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestNoDuplicateNodeIDs_Clean(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
	}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
}

func TestNoDuplicateNodeIDs_Duplicate(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "B"}},
	}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != "error" {
		t.Errorf("expected severity error, got %s", issues[0].Severity)
	}
}

func TestNoDuplicateNodeIDs_MultiDuplicate(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}, {ID: "A"}},
	}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	// Should report only once per duplicate ID
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue for triplicate, got %d", len(issues))
	}
}

func TestNoDisconnectedNodes_AllConnected(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
}

func TestNoDisconnectedNodes_Disconnected(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].RuleID != "no-disconnected-nodes" {
		t.Errorf("wrong rule ID: %s", issues[0].RuleID)
	}
}

func TestNoDisconnectedNodes_NoEdgesExempt(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 0 {
		t.Fatalf("single-node diagram with no edges should be exempt")
	}
}

func TestMaxFanout_UnderLimit(t *testing.T) {
	d := &model.Diagram{
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "A", To: "C"},
		},
	}
	issues := rules.MaxFanout{}.Run(d, nil)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %d", len(issues))
	}
}

func TestMaxFanout_OverLimit(t *testing.T) {
	edges := make([]model.Edge, 6)
	for i := range edges {
		edges[i] = model.Edge{From: "A", To: string(rune('B' + i))}
	}
	d := &model.Diagram{Edges: edges}
	issues := rules.MaxFanout{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != "warn" {
		t.Errorf("expected warn severity, got %s", issues[0].Severity)
	}
}

func TestMaxFanout_CustomLimit(t *testing.T) {
	edges := []model.Edge{
		{From: "A", To: "B"},
		{From: "A", To: "C"},
		{From: "A", To: "D"},
	}
	d := &model.Diagram{Edges: edges}
	cfg := rules.Config{"max-fanout": {"limit": 2}}
	issues := rules.MaxFanout{}.Run(d, cfg)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue with custom limit 2, got %d", len(issues))
	}
}
