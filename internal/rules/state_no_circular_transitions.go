package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// StateNoCircularTransitions flags self-loop state transitions in state diagrams.
// Note: Bidirectional transitions (A ↔ B) are common and valid in state diagrams,
// so this rule only flags self-loops (State → State).
type StateNoCircularTransitions struct{}

func (r StateNoCircularTransitions) ID() string { return "no-circular-transitions" }

func (r StateNoCircularTransitions) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyState}
}

func (r StateNoCircularTransitions) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	var issues []model.Issue
	for _, edge := range d.Edges {
		// Only flag self-loops (State → State)
		if edge.From == edge.To {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "self-loop state transition: " + edge.From,
				Line:     edge.Line,
				Column:   edge.Column,
			})
		}
	}

	return issues
}
