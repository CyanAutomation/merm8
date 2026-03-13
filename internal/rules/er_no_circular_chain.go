package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// ERNoCircularChain flags circular chains of entity relationships in ER diagrams.
type ERNoCircularChain struct{}

func (r ERNoCircularChain) ID() string { return "no-circular-chain" }

func (r ERNoCircularChain) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyER}
}

func (r ERNoCircularChain) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Build adjacency list (exclude self-referential edges)
	adj := make(map[string][]string)
	for _, edge := range d.Edges {
		if edge.From != edge.To {
			adj[edge.From] = append(adj[edge.From], edge.To)
		}
	}

	// Detect cycles using DFS
	visited := make(map[string]bool)
	recStack := make(map[string]bool)
	var cycles []string

	var dfs func(node string, path []string) bool
	dfs = func(node string, path []string) bool {
		visited[node] = true
		recStack[node] = true
		path = append(path, node)

		for _, neighbor := range adj[node] {
			if !visited[neighbor] {
				if dfs(neighbor, path) {
					return true
				}
			} else if recStack[neighbor] {
				// Found cycle
				cycleStart := -1
				for i, n := range path {
					if n == neighbor {
						cycleStart = i
						break
					}
				}
				if cycleStart >= 0 {
					cyclePath := append(path[cycleStart:], neighbor)
					cycles = append(cycles, joinPath(cyclePath))
				}
				return true
			}
		}

		recStack[node] = false
		return false
	}

	for node := range adj {
		if !visited[node] {
			dfs(node, nil)
		}
	}

	if len(cycles) == 0 {
		return nil
	}

	issues := make([]model.Issue, 0, len(cycles))
	for _, cycle := range cycles {
		issues = append(issues, model.Issue{
			RuleID:   r.ID(),
			Severity: severity,
			Message:  "circular entity relationship chain detected: " + cycle,
		})
	}

	return issues
}
