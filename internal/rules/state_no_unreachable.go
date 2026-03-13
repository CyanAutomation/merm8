package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// StateNoUnreachable flags states that cannot be reached from the initial state.
type StateNoUnreachable struct{}

func (r StateNoUnreachable) ID() string { return "no-unreachable-state" }

func (r StateNoUnreachable) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyState}
}

func (r StateNoUnreachable) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Build adjacency list
	adj := make(map[string][]string)
	for _, edge := range d.Edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
	}

	// Find initial states
	reachable := make(map[string]bool)
	queue := make([]string, 0)

	// Use explicit start states if available (from [*] transitions)
	if len(d.StartStates) > 0 {
		for _, startState := range d.StartStates {
			if !reachable[startState] {
				queue = append(queue, startState)
				reachable[startState] = true
			}
		}
	} else {
		// Fallback: find nodes with no incoming edges
		indegree := make(map[string]int)
		for _, node := range d.Nodes {
			indegree[node.ID] = 0
		}
		for _, edge := range d.Edges {
			indegree[edge.To]++
		}

		for nodeID, degree := range indegree {
			if degree == 0 {
				queue = append(queue, nodeID)
				reachable[nodeID] = true
			}
		}
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, neighbor := range adj[current] {
			if !reachable[neighbor] {
				reachable[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	// Find unreachable states
	nodeByID := make(map[string]model.Node)
	for _, node := range d.Nodes {
		nodeByID[node.ID] = node
	}

	var issues []model.Issue
	for _, node := range d.Nodes {
		if !reachable[node.ID] {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  "unreachable state: " + node.ID,
				Line:     node.Line,
				Column:   node.Column,
			})
		}
	}

	return issues
}
