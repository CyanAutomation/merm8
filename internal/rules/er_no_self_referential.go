package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// ERNoSelfReferential flags self-referential entity relationships in ER diagrams.
type ERNoSelfReferential struct{}

func (r ERNoSelfReferential) ID() string { return "no-self-referential" }

func (r ERNoSelfReferential) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyER}
}

func (r ERNoSelfReferential) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")

	var issues []model.Issue
	for _, edge := range d.Edges {
		if edge.From == edge.To {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "self-referential entity relationship: " + edge.From,
				Line:     edge.Line,
				Column:   edge.Column,
			})
		}
	}

	return issues
}
