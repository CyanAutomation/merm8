package rules_test

import (
	"strings"
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
	if issues[0].Line != nil || issues[0].Column != nil {
		t.Errorf("expected location to be unset when unknown, got line=%v column=%v", issues[0].Line, issues[0].Column)
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

func TestNoDisconnectedNodes_NoEdgesMultipleNodes(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 3 {
		t.Fatalf("expected 3 issues for three disconnected nodes, got %d", len(issues))
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
	if issues[0].Severity != "warning" {
		t.Errorf("expected warning severity, got %s", issues[0].Severity)
	}
	if issues[0].Line != nil || issues[0].Column != nil {
		t.Errorf("expected location to be unset when unknown, got line=%v column=%v", issues[0].Line, issues[0].Column)
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
			cfg := rules.Config{"max-fanout": {"limit": tt.limit}}
			issues := rules.MaxFanout{}.Run(d, cfg)
			if len(issues) != 1 {
				t.Fatalf("expected 1 issue using default limit fallback, got %d", len(issues))
			}
			want := "exceeding limit of 5"
			if !strings.Contains(issues[0].Message, want) {
				t.Fatalf("expected message to contain %q, got %q", want, issues[0].Message)
			}
		})
	}
}

func TestMaxFanout_SeverityOverride(t *testing.T) {
	edges := make([]model.Edge, 6)
	for i := range edges {
		edges[i] = model.Edge{From: "A", To: string(rune('B' + i))}
	}
	d := &model.Diagram{Edges: edges}
	cfg := rules.Config{"max-fanout": {"severity": "error"}}

	issues := rules.MaxFanout{}.Run(d, cfg)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != "error" {
		t.Fatalf("expected severity override to error, got %q", issues[0].Severity)
	}
}

func TestNoDuplicateNodeIDs_SeverityOverride(t *testing.T) {
	d := &model.Diagram{Nodes: []model.Node{{ID: "A"}, {ID: "A"}}}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, rules.Config{"no-duplicate-node-ids": {"severity": "info"}})
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != "info" {
		t.Fatalf("expected overridden severity info, got %q", issues[0].Severity)
	}
}

func TestRuleEnabled_DefaultAndConfigured(t *testing.T) {
	if !rules.RuleEnabled("max-fanout", rules.Config{}) {
		t.Fatal("expected rule to be enabled by default")
	}
	if rules.RuleEnabled("max-fanout", rules.Config{"max-fanout": {"enabled": false}}) {
		t.Fatal("expected rule to be disabled when enabled=false")
	}
}

func TestNormalizeConfig_KeyAliases(t *testing.T) {
	cfg := rules.Config{
		"max-fanout": {
			"Severity":              "error",
			"suppression-selectors": []interface{}{"node:A"},
		},
	}

	normalized, err := rules.NormalizeConfig(cfg, map[string]struct{}{"max-fanout": {}})
	if err != nil {
		t.Fatalf("expected normalization to succeed, got %v", err)
	}
	if _, ok := normalized["max-fanout"]["severity"]; !ok {
		t.Fatal("expected severity key to be normalized to lowercase")
	}
	if _, ok := normalized["max-fanout"]["suppression_selectors"]; !ok {
		t.Fatal("expected suppression selector alias to normalize to suppression_selectors")
	}
}

func TestNormalizeConfig_UnknownRuleID(t *testing.T) {
	_, err := rules.NormalizeConfig(rules.Config{"unknown": {}}, map[string]struct{}{"max-fanout": {}})
	if err == nil {
		t.Fatal("expected error for unknown rule id")
	}
}

func TestConfigRegistry_ContainsBuiltins(t *testing.T) {
	registry := rules.ConfigRegistry()
	if _, ok := registry["max-fanout"]; !ok {
		t.Fatal("expected max-fanout in config registry")
	}
	if _, ok := registry["no-duplicate-node-ids"]; !ok {
		t.Fatal("expected no-duplicate-node-ids in config registry")
	}
	if _, ok := registry["no-disconnected-nodes"]; !ok {
		t.Fatal("expected no-disconnected-nodes in config registry")
	}
}

func TestValidateOption_MaxFanoutLimit(t *testing.T) {
	if err := rules.ValidateOption("max-fanout", "limit", 2); err != nil {
		t.Fatalf("expected valid limit, got %v", err)
	}
	if err := rules.ValidateOption("max-fanout", "limit", 0); err == nil {
		t.Fatal("expected invalid limit to fail validation")
	}
}
