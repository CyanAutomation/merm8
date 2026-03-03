package rules

import "github.com/CyanAutomation/merm8/internal/model"

// NoDuplicateNodeIDs flags diagrams that contain more than one node with the
// same ID, which would silently produce wrong graph layouts.
type NoDuplicateNodeIDs struct{}

func (r NoDuplicateNodeIDs) ID() string { return "no-duplicate-node-ids" }

func (r NoDuplicateNodeIDs) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity, err := resolveSeverity(r.ID(), cfg, "error")
	if err != nil {
		severity = "error"
	}
	seen := make(map[string]bool, len(d.Nodes))
	reported := make(map[string]bool)
	var issues []model.Issue
	for _, n := range d.Nodes {
		if seen[n.ID] && !reported[n.ID] {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "duplicate node ID: " + n.ID,
			})
			reported[n.ID] = true
		}
		seen[n.ID] = true
	}
	return issues
}
