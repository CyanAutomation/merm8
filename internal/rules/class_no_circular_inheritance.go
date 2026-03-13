package rules

import (
	"github.com/CyanAutomation/merm8/internal/model"
)

// ClassNoCircularInheritance flags circular inheritance chains in class diagrams.
type ClassNoCircularInheritance struct{}

func (r ClassNoCircularInheritance) ID() string { return "no-circular-inheritance" }

func (r ClassNoCircularInheritance) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyClass}
}

func (r ClassNoCircularInheritance) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")

	// Build adjacency list for inheritance relationships (extension type)
	adj := make(map[string][]string)
	for _, edge := range d.Edges {
		if edge.Type == "extension" {
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
			Message:  "circular inheritance detected: " + cycle,
		})
	}

	return issues
}

func joinPath(path []string) string {
	if len(path) == 0 {
		return ""
	}
	result := path[0]
	for i := 1; i < len(path); i++ {
		result += " -> " + path[i]
	}
	return result
}
