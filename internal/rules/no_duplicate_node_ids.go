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

	// Detect duplicate node IDs - report each unique ID that appears multiple times
	IDCount := make(map[string]int)
	lastNodeByID := make(map[string]*model.Node)

	for i := range d.Nodes {
		IDCount[d.Nodes[i].ID]++
		lastNodeByID[d.Nodes[i].ID] = &d.Nodes[i]
	}

	var issues []model.Issue
	seen := make(map[string]bool)

	for nodeID, count := range IDCount {
		if count > 1 && !seen[nodeID] {
			seen[nodeID] = true
			lastNode := lastNodeByID[nodeID]
			issue := model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "duplicate node ID: " + nodeID,
				Line:     lastNode.Line,
				Column:   lastNode.Column,
			}
			issues = append(issues, issue)
		}
	}

	return issues
}
