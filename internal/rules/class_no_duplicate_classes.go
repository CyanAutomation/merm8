package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// ClassNoDuplicateClasses flags duplicate class definitions in class diagrams.
type ClassNoDuplicateClasses struct{}

func (r ClassNoDuplicateClasses) ID() string { return "no-duplicate-classes" }

func (r ClassNoDuplicateClasses) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyClass}
}

func (r ClassNoDuplicateClasses) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Count class definitions
	classCounts := make(map[string]int)
	classLocations := make(map[string]model.Node)

	for _, node := range d.Nodes {
		classCounts[node.ID]++
		if _, exists := classLocations[node.ID]; !exists {
			classLocations[node.ID] = node
		}
	}

	var issues []model.Issue
	for classID, count := range classCounts {
		if count > 1 {
			node := classLocations[classID]
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "duplicate class definition: " + classID,
				Line:     node.Line,
				Column:   node.Column,
			})
		}
	}

	return issues
}
