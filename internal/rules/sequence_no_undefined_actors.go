package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// SequenceNoUndefinedActors flags messages referencing actors that are not
// defined as participants in the sequence diagram.
type SequenceNoUndefinedActors struct{}

func (r SequenceNoUndefinedActors) ID() string { return "no-undefined-actors" }

func (r SequenceNoUndefinedActors) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilySequence}
}

func (r SequenceNoUndefinedActors) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Build set of defined actors from nodes (participants are nodes in sequence diagrams)
	definedActors := make(map[string]bool)
	for _, node := range d.Nodes {
		definedActors[node.ID] = true
	}

	// Check all edge endpoints for undefined actors
	undefinedRefs := make(map[string]model.Edge)
	for _, edge := range d.Edges {
		if edge.From != "" && !definedActors[edge.From] {
			undefinedRefs[edge.From] = edge
		}
		if edge.To != "" && !definedActors[edge.To] {
			undefinedRefs[edge.To] = edge
		}
	}

	if len(undefinedRefs) == 0 {
		return nil
	}

	issues := make([]model.Issue, 0, len(undefinedRefs))
	for actorID, edge := range undefinedRefs {
		issues = append(issues, model.Issue{
			RuleID:   r.ID(),
			Severity: severity,
			Message:  "undefined actor reference: " + actorID,
			Line:     edge.Line,
			Column:   edge.Column,
		})
	}

	return issues
}
