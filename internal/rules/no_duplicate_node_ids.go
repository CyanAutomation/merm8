package rules

import "github.com/CyanAutomation/merm8/internal/model"

// NoDuplicateNodeIDs flags diagrams that contain more than one node with the
// same ID, which would silently produce wrong graph layouts.
type NoDuplicateNodeIDs struct{}

func (r NoDuplicateNodeIDs) ID() string { return "no-duplicate-node-ids" }

func (r NoDuplicateNodeIDs) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r NoDuplicateNodeIDs) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")
	seen := make(map[string]int, len(d.Nodes))
	reported := make(map[string]bool)
	var issues []model.Issue
	for _, n := range d.Nodes {
		if seen[n.ID] > 0 && !reported[n.ID] {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "duplicate node ID: " + n.ID,
				Line:     n.Line,
				Column:   n.Column,
				Context:  NodeSubgraphContextForOccurrence(d, n.ID, seen[n.ID]),
			})
			reported[n.ID] = true
		}
		seen[n.ID]++
	}
	return issues
}
