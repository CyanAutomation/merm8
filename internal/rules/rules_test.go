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

func TestSeverityOverride_NormalizesCaseAndWhitespace(t *testing.T) {
	edges := make([]model.Edge, 6)
	for i := range edges {
		edges[i] = model.Edge{From: "A", To: string(rune('B' + i))}
	}
	d := &model.Diagram{Edges: edges}

	tests := []struct {
		name     string
		severity string
		want     string
	}{
		{name: "upper error", severity: "ERROR", want: "error"},
		{name: "title warning", severity: "Warning", want: "warning"},
		{name: "title info with spaces", severity: "  Info  ", want: "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := rules.Config{"max-fanout": {"severity": tt.severity}}
			issues := rules.MaxFanout{}.Run(d, cfg)
			if len(issues) != 1 {
				t.Fatalf("expected 1 issue, got %d", len(issues))
			}
			if issues[0].Severity != tt.want {
				t.Fatalf("expected severity %q, got %q", tt.want, issues[0].Severity)
			}
		})
	}
}

func TestValidateConfig_RejectsWarnAlias(t *testing.T) {
	err := rules.ValidateConfig(rules.Config{"max-fanout": {"severity": "warn"}})
	if err == nil {
		t.Fatal("expected warn severity alias to be rejected")
	}
	if !strings.Contains(err.Error(), "allowed: error, warning, info") {
		t.Fatalf("expected allowed-values guidance in error, got %v", err)
	}
}

func TestValidateConfig_RejectsMalformedSuppressionSelector(t *testing.T) {
	err := rules.ValidateConfig(rules.Config{"max-fanout": {"suppression-selectors": []string{"node", "! node:A"}}})
	if err == nil {
		t.Fatal("expected malformed suppression selectors to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid suppression selector") {
		t.Fatalf("expected invalid suppression selector error, got %v", err)
	}
}

func TestValidateConfig_AcceptsCanonicalSuppressionSelectors(t *testing.T) {
	err := rules.ValidateConfig(rules.Config{"max-fanout": {"suppression-selectors": []string{"node:A", "!rule:max-fanout", "subgraph:cluster-1"}}})
	if err != nil {
		t.Fatalf("expected canonical suppression selectors to be accepted, got %v", err)
	}
}

func TestNoDuplicateNodeIDs_UsesDuplicateNodeLocation(t *testing.T) {
	line := 4
	col := 7
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A", Line: &line, Column: &col}},
	}
	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Line == nil || *issues[0].Line != line {
		t.Fatalf("expected duplicate issue line=%d, got %v", line, issues[0].Line)
	}
	if issues[0].Column == nil || *issues[0].Column != col {
		t.Fatalf("expected duplicate issue column=%d, got %v", col, issues[0].Column)
	}
}

func TestNoDisconnectedNodes_UsesNodeLocation(t *testing.T) {
	line := 5
	col := 2
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B", Line: &line, Column: &col}},
		Edges: []model.Edge{{From: "A", To: "A"}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Line == nil || *issues[0].Line != line {
		t.Fatalf("expected disconnected issue line=%d, got %v", line, issues[0].Line)
	}
	if issues[0].Column == nil || *issues[0].Column != col {
		t.Fatalf("expected disconnected issue column=%d, got %v", col, issues[0].Column)
	}
}

func TestMaxFanout_UsesNodeLocation(t *testing.T) {
	line := 6
	col := 4
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A", Line: &line, Column: &col}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}, {From: "A", To: "E"}, {From: "A", To: "F"}, {From: "A", To: "G"}},
	}
	issues := rules.MaxFanout{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Line == nil || *issues[0].Line != line {
		t.Fatalf("expected fanout issue line=%d, got %v", line, issues[0].Line)
	}
	if issues[0].Column == nil || *issues[0].Column != col {
		t.Fatalf("expected fanout issue column=%d, got %v", col, issues[0].Column)
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
	if _, ok := normalized["max-fanout"]["suppression-selectors"]; !ok {
		t.Fatal("expected suppression selector alias to normalize to suppression-selectors")
	}
}

func TestNormalizeConfig_LegacySnakeCaseAlias(t *testing.T) {
	cfg := rules.Config{
		"max-fanout": {
			"suppression_selectors": []interface{}{"node:A"},
		},
	}

	normalized, err := rules.NormalizeConfig(cfg, map[string]struct{}{"max-fanout": {}})
	if err != nil {
		t.Fatalf("expected normalization to succeed, got %v", err)
	}
	if _, ok := normalized["max-fanout"]["suppression-selectors"]; !ok {
		t.Fatal("expected suppression_selectors legacy key to normalize to suppression-selectors")
	}
}

func TestNormalizeConfig_NamespacedBuiltInRuleID(t *testing.T) {
	cfg := rules.Config{
		"core/max-fanout": {
			"limit": 2,
		},
	}

	normalized, err := rules.NormalizeConfig(cfg, map[string]struct{}{"max-fanout": {}})
	if err != nil {
		t.Fatalf("expected normalization to succeed, got %v", err)
	}
	if _, ok := normalized["max-fanout"]; !ok {
		t.Fatal("expected core/max-fanout to normalize to max-fanout")
	}
	if normalized["max-fanout"]["limit"] != 2 {
		t.Fatalf("expected merged limit value to be preserved, got %#v", normalized["max-fanout"]["limit"])
	}
}

func TestNormalizeConfig_MergesLegacyAndNamespacedBuiltInRuleID(t *testing.T) {
	cfg := rules.Config{
		"max-fanout": {
			"enabled": false,
		},
		"core/max-fanout": {
			"limit": 3,
		},
	}

	normalized, err := rules.NormalizeConfig(cfg, map[string]struct{}{"max-fanout": {}})
	if err != nil {
		t.Fatalf("expected normalization to succeed, got %v", err)
	}
	ruleCfg, ok := normalized["max-fanout"]
	if !ok {
		t.Fatal("expected merged max-fanout config entry")
	}
	if got, ok := ruleCfg["enabled"].(bool); !ok || got {
		t.Fatalf("expected enabled=false to be preserved, got %#v", ruleCfg["enabled"])
	}
	if got, ok := ruleCfg["limit"].(int); !ok || got != 3 {
		t.Fatalf("expected limit=3 from namespaced entry, got %#v", ruleCfg["limit"])
	}
}

func TestNormalizeConfig_UnknownNamespacedRuleID(t *testing.T) {
	_, err := rules.NormalizeConfig(rules.Config{"core/not-a-built-in": {}}, map[string]struct{}{"max-fanout": {}})
	if err == nil {
		t.Fatal("expected error for unknown namespaced rule id")
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
	if _, ok := registry["no-cycles"]; !ok {
		t.Fatal("expected no-cycles in config registry")
	}
	if _, ok := registry["max-depth"]; !ok {
		t.Fatal("expected max-depth in config registry")
	}
}

func TestValidateRegisteredRuleID(t *testing.T) {
	tests := []struct {
		name        string
		ruleID      string
		wantWarning bool
		wantErr     bool
	}{
		{name: "built-in legacy", ruleID: "max-fanout"},
		{name: "built-in namespaced", ruleID: "core/max-fanout"},
		{name: "custom namespaced", ruleID: "custom/acme/max-fanout-guard"},
		{name: "legacy custom", ruleID: "third-party-rule", wantWarning: true},
		{name: "invalid core id", ruleID: "core/unknown", wantErr: true},
		{name: "invalid custom shape", ruleID: "custom/acme", wantErr: true},
		{name: "unsupported namespace", ruleID: "vendor/rule", wantErr: true},
		{name: "empty", ruleID: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warning, err := rules.ValidateRegisteredRuleID(tt.ruleID)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.ruleID)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error for %q, got %v", tt.ruleID, err)
			}
			if (warning != "") != tt.wantWarning {
				t.Fatalf("unexpected warning state for %q: warning=%q", tt.ruleID, warning)
			}
		})
	}
}

func TestCanonicalRuleRegistrationID(t *testing.T) {
	tests := []struct {
		ruleID string
		want   string
	}{
		{ruleID: "max-fanout", want: "core/max-fanout"},
		{ruleID: "core/max-fanout", want: "core/max-fanout"},
		{ruleID: "custom/acme/graph-depth", want: "custom/acme/graph-depth"},
		{ruleID: "legacy-custom", want: "custom/legacy/legacy-custom"},
	}

	for _, tt := range tests {
		if got := rules.CanonicalRuleRegistrationID(tt.ruleID); got != tt.want {
			t.Fatalf("canonical id mismatch for %q: got %q want %q", tt.ruleID, got, tt.want)
		}
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

func TestNoDuplicateNodeIDs_SubgraphContextUsesDuplicateOccurrence(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}},
		Subgraphs: []model.Subgraph{
			{ID: "cluster-1", Label: "First", Nodes: []string{"A"}},
			{ID: "cluster-2", Label: "Second", Nodes: []string{"A"}},
		},
	}

	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Context == nil {
		t.Fatal("expected context for duplicate occurrence")
	}
	if issues[0].Context.SubgraphID != "cluster-2" || issues[0].Context.SubgraphLabel != "Second" {
		t.Fatalf("expected second occurrence context, got %#v", issues[0].Context)
	}
}

func TestNoDuplicateNodeIDs_SubgraphContextAbsentWhenDuplicateOutsideSubgraph(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "A"}},
		Subgraphs: []model.Subgraph{
			{ID: "cluster-1", Label: "First", Nodes: []string{"A"}},
		},
	}

	issues := rules.NoDuplicateNodeIDs{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Context != nil {
		t.Fatalf("expected no context for second occurrence outside subgraph, got %#v", issues[0].Context)
	}
}

func TestNoDisconnectedNodes_SubgraphContextPresentForMemberNode(t *testing.T) {
	d := &model.Diagram{
		Nodes:     []model.Node{{ID: "A"}, {ID: "B"}},
		Edges:     []model.Edge{{From: "A", To: "A"}},
		Subgraphs: []model.Subgraph{{ID: "cluster-1", Label: "Core", Nodes: []string{"B"}}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Context == nil || issues[0].Context.SubgraphID != "cluster-1" || issues[0].Context.SubgraphLabel != "Core" {
		t.Fatalf("expected issue context with subgraph details, got %#v", issues[0].Context)
	}
}

func TestNoDisconnectedNodes_SubgraphContextAbsentForNonMemberNode(t *testing.T) {
	d := &model.Diagram{
		Nodes:     []model.Node{{ID: "A"}, {ID: "B"}},
		Edges:     []model.Edge{{From: "A", To: "A"}},
		Subgraphs: []model.Subgraph{{ID: "cluster-1", Label: "Core", Nodes: []string{"A"}}},
	}
	issues := rules.NoDisconnectedNodes{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Context != nil {
		t.Fatalf("expected no context for node outside subgraph, got %#v", issues[0].Context)
	}
}

func TestNoCycles_DetectsDirectedCycle(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "B", To: "C"}, {From: "C", To: "A"}},
	}
	issues := rules.NoCycles{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 cycle issue, got %d", len(issues))
	}
	if issues[0].RuleID != "no-cycles" {
		t.Fatalf("expected rule no-cycles, got %q", issues[0].RuleID)
	}
}

func TestNoCycles_DeduplicatesRotations(t *testing.T) {
	d := &model.Diagram{
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
			{From: "C", To: "A"},
			{From: "D", To: "B"},
		},
	}

	issues := rules.NoCycles{}.Run(d, nil)
	if len(issues) != 1 {
		t.Fatalf("expected 1 deduplicated cycle issue, got %d", len(issues))
	}
}

func TestNoCycles_AllowSelfLoopOption(t *testing.T) {
	d := &model.Diagram{Edges: []model.Edge{{From: "A", To: "A"}}}
	issues := rules.NoCycles{}.Run(d, rules.Config{"no-cycles": {"allow-self-loop": true}})
	if len(issues) != 0 {
		t.Fatalf("expected self-loop to be ignored, got %d issues", len(issues))
	}
}

func TestMaxDepth_OverLimit(t *testing.T) {
	d := &model.Diagram{Edges: []model.Edge{{From: "A", To: "B"}, {From: "B", To: "C"}, {From: "C", To: "D"}}}
	issues := rules.MaxDepth{}.Run(d, rules.Config{"max-depth": {"limit": 2}})
	if len(issues) != 1 {
		t.Fatalf("expected 1 max-depth issue, got %d", len(issues))
	}
	if issues[0].RuleID != "max-depth" {
		t.Fatalf("expected rule max-depth, got %q", issues[0].RuleID)
	}
}

func TestMaxDepth_NoRootsDoesNotDuplicateIssues(t *testing.T) {
	d := &model.Diagram{
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
			{From: "C", To: "A"},
			{From: "C", To: "D"},
			{From: "D", To: "E"},
		},
	}

	issues := rules.MaxDepth{}.Run(d, rules.Config{"max-depth": {"limit": 2}})
	if len(issues) != 1 {
		t.Fatalf("expected 1 deduplicated max-depth issue, got %d", len(issues))
	}
}

func TestMaxDepth_UnderDefaultLimit(t *testing.T) {
	d := &model.Diagram{Edges: []model.Edge{{From: "A", To: "B"}, {From: "B", To: "C"}}}
	issues := rules.MaxDepth{}.Run(d, nil)
	if len(issues) != 0 {
		t.Fatalf("expected no max-depth issue, got %d", len(issues))
	}
}

func TestValidateOption_NewRules(t *testing.T) {
	if err := rules.ValidateOption("max-depth", "limit", 3); err != nil {
		t.Fatalf("expected max-depth limit to be valid, got %v", err)
	}
	if err := rules.ValidateOption("max-depth", "limit", 0); err == nil {
		t.Fatal("expected max-depth limit=0 to be invalid")
	}
	if err := rules.ValidateOption("no-cycles", "allow-self-loop", true); err != nil {
		t.Fatalf("expected allow-self-loop to be valid, got %v", err)
	}
	if err := rules.ValidateOption("no-cycles", "allow-self-loop", "true"); err == nil {
		t.Fatal("expected non-boolean allow-self-loop to be invalid")
	}
}
