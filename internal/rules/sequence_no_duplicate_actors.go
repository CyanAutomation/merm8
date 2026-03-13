package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// SequenceNoDuplicateActors flags duplicate participant definitions in
// sequence diagrams.
type SequenceNoDuplicateActors struct{}

func (r SequenceNoDuplicateActors) ID() string { return "no-duplicate-actors" }

func (r SequenceNoDuplicateActors) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilySequence}
}

func (r SequenceNoDuplicateActors) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Track actor definitions and detect duplicates
	actorCounts := make(map[string]int)
	actorLocations := make(map[string]model.Node)

	for _, node := range d.Nodes {
		actorCounts[node.ID]++
		if _, exists := actorLocations[node.ID]; !exists {
			actorLocations[node.ID] = node
		}
	}

	var issues []model.Issue
	for actorID, count := range actorCounts {
		if count > 1 {
			node := actorLocations[actorID]
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "duplicate actor definition: " + actorID,
				Line:     node.Line,
				Column:   node.Column,
			})
		}
	}

	return issues
}
