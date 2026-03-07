package rules

import (
	"sort"

	"github.com/CyanAutomation/merm8/internal/model"
)

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

	// Track the first occurrence of each node for optional source locations.
	nodeByID := make(map[string]model.Node)
	for i := range d.Nodes {
		if _, ok := nodeByID[d.Nodes[i].ID]; !ok {
			nodeByID[d.Nodes[i].ID] = d.Nodes[i]
		}
	}

	disconnectedIDs := make(map[string]struct{})

	// Single-node diagram with no edges is fully exempt (both graph and source analysis).
	singleNodeNoEdgesExempt := len(d.Nodes) == 1 && len(d.Edges) == 0

	// Keep current graph-based logic as one source of disconnected nodes.
	if !singleNodeNoEdgesExempt {
		switch {
		// If we have no nodes there is nothing graph-derived to report.
		case len(d.Nodes) == 0:
			// No graph-derived disconnected node IDs.

		// If no edges at all but multiple nodes, all are disconnected.
		case len(d.Edges) == 0:
			for i := range d.Nodes {
				disconnectedIDs[d.Nodes[i].ID] = struct{}{}
			}

		default:
			// Build set of nodes referenced in edges.
			nodeInEdges := make(map[string]bool)
			for _, edge := range d.Edges {
				nodeInEdges[edge.From] = true
				nodeInEdges[edge.To] = true
			}

			// Find nodes not referenced in any edge.
			for i := range d.Nodes {
				if !nodeInEdges[d.Nodes[i].ID] {
					disconnectedIDs[d.Nodes[i].ID] = struct{}{}
				}
			}
		}

		// Union graph-derived IDs with parser/source-derived disconnected IDs.
		for _, nodeID := range d.DisconnectedNodeIDs {
			disconnectedIDs[nodeID] = struct{}{}
		}
	}

	if len(disconnectedIDs) == 0 {
		return nil
	}

	// Keep deterministic ordering for predictable tests/output.
	sortedIDs := make([]string, 0, len(disconnectedIDs))
	for nodeID := range disconnectedIDs {
		sortedIDs = append(sortedIDs, nodeID)
	}
	sort.Strings(sortedIDs)

	issues := make([]model.Issue, 0, len(sortedIDs))
	for _, nodeID := range sortedIDs {
		issue := model.Issue{
			RuleID:   r.ID(),
			Severity: severity,
			Message:  "node is disconnected: " + nodeID,
		}

		if node, ok := nodeByID[nodeID]; ok {
			issue.Line = node.Line
			issue.Column = node.Column
		}

		issues = append(issues, issue)
	}

	return issues
}
