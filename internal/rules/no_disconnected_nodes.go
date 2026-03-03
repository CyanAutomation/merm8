package rules

import "github.com/CyanAutomation/merm8/internal/model"

// NoDisconnectedNodes flags nodes that have no incoming or outgoing edges.
// A single-node diagram (no edges at all) is exempt to avoid false positives.
type NoDisconnectedNodes struct{}

func (r NoDisconnectedNodes) ID() string { return "no-disconnected-nodes" }

func (r NoDisconnectedNodes) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r NoDisconnectedNodes) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")
	if len(d.Edges) == 0 && len(d.Nodes) <= 1 {
		return nil
	}
	connected := make(map[string]bool, len(d.Nodes))
	for _, e := range d.Edges {
		connected[e.From] = true
		connected[e.To] = true
	}
	var issues []model.Issue
	for _, n := range d.Nodes {
		if !connected[n.ID] {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "node is disconnected: " + n.ID,
				Line:     n.Line,
				Column:   n.Column,
				Context:  NodeSubgraphContext(d, n.ID),
			})
		}
	}
	return issues
}
