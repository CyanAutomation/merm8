package rules

import "github.com/CyanAutomation/merm8/internal/model"

// NoDisconnectedNodes flags nodes that have no incoming or outgoing edges.
// A single-node diagram (no edges at all) is exempt to avoid false positives.
type NoDisconnectedNodes struct{}

func (r NoDisconnectedNodes) ID() string { return "no-disconnected-nodes" }

func (r NoDisconnectedNodes) Families() []model.DiagramFamily {
	return []model.DiagramFamily{
		model.DiagramFamilyFlowchart,
	}
}

func (r NoDisconnectedNodes) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Single-node diagram with no edges is exempt to avoid false positives
	if len(d.Nodes) == 1 && len(d.Edges) == 0 {
		return nil
	}

	// Also exempt if we have no nodes
	if len(d.Nodes) == 0 {
		return nil
	}

	// If no edges at all but multiple nodes, all are disconnected
	if len(d.Edges) == 0 {
		var issues []model.Issue
		for i := range d.Nodes {
			issue := model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "node is disconnected: " + d.Nodes[i].ID,
				Line:     d.Nodes[i].Line,
				Column:   d.Nodes[i].Column,
			}
			issues = append(issues, issue)
		}
		return issues
	}

	// Build set of nodes referenced in edges
	nodeInEdges := make(map[string]bool)
	for _, edge := range d.Edges {
		nodeInEdges[edge.From] = true
		nodeInEdges[edge.To] = true
	}

	// Find nodes not referenced in any edge
	var issues []model.Issue
	for i := range d.Nodes {
		if !nodeInEdges[d.Nodes[i].ID] {
			issue := model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "node is disconnected: " + d.Nodes[i].ID,
				Line:     d.Nodes[i].Line,
				Column:   d.Nodes[i].Column,
			}
			issues = append(issues, issue)
		}
	}

	return issues
}
