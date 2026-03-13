package rules

import (
	"fmt"
	"math"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultStateMaxTransitions = 10

// StateMaxTransitions flags states with too many outgoing transitions.
type StateMaxTransitions struct{}

func (r StateMaxTransitions) ID() string { return "max-transitions" }

func (r StateMaxTransitions) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyState}
}

func (r StateMaxTransitions) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")

	limit := defaultStateMaxTransitions
	if rc, ok := cfg[r.ID()]; ok {
		if v, ok := rc["limit"]; ok {
			switch n := v.(type) {
			case int:
				if n >= 1 {
					limit = n
				}
			case float64:
				if n >= 1 && n == math.Trunc(n) {
					limit = int(n)
				}
			}
		}
	}

	// Count outgoing transitions per state
	outgoing := make(map[string]int)
	nodeByID := make(map[string]model.Node)
	for _, node := range d.Nodes {
		nodeByID[node.ID] = node
	}

	for _, edge := range d.Edges {
		outgoing[edge.From]++
	}

	var issues []model.Issue
	for stateID, count := range outgoing {
		if count > limit {
			node := nodeByID[stateID]
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  fmt.Sprintf("state %q has %d outgoing transitions, exceeding limit of %d", stateID, count, limit),
				Line:     node.Line,
				Column:   node.Column,
			})
		}
	}

	return issues
}
