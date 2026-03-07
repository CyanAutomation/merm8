package rules

import (
	"sort"

	"github.com/CyanAutomation/merm8/internal/model"
)

// NoDuplicateNodeIDs flags diagrams that contain more than one node with the
// same ID, which would silently produce wrong graph layouts.
type NoDuplicateNodeIDs struct{}

func (r NoDuplicateNodeIDs) ID() string { return "no-duplicate-node-ids" }

func (r NoDuplicateNodeIDs) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r NoDuplicateNodeIDs) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Detect duplicate node IDs from AST node counting.
	// Also include IDs provided by parser source analysis, which captures
	// duplicates that may have been collapsed by parser normalization.
	idCount := make(map[string]int)
	lastNodeByID := make(map[string]*model.Node)

	for i := range d.Nodes {
		idCount[d.Nodes[i].ID]++
		lastNodeByID[d.Nodes[i].ID] = &d.Nodes[i]
	}

	duplicateIDs := make(map[string]struct{})
	for nodeID, count := range idCount {
		if count > 1 {
			duplicateIDs[nodeID] = struct{}{}
		}
	}
	for _, nodeID := range d.DuplicateNodeIDs {
		duplicateIDs[nodeID] = struct{}{}
	}

	orderedDuplicateIDs := make([]string, 0, len(duplicateIDs))
	for nodeID := range duplicateIDs {
		orderedDuplicateIDs = append(orderedDuplicateIDs, nodeID)
	}
	sort.Strings(orderedDuplicateIDs)

	var issues []model.Issue
	for _, nodeID := range orderedDuplicateIDs {
		issue := model.Issue{
			RuleID:   r.ID(),
			Severity: severity,
			Message:  "duplicate node ID: " + nodeID,
		}
		if lastNode := lastNodeByID[nodeID]; lastNode != nil {
			issue.Line = lastNode.Line
			issue.Column = lastNode.Column
		}
		issues = append(issues, issue)
	}

	return issues
}
