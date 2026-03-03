package rules

import (
	"fmt"
	"math"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultMaxDepth = 8

// MaxDepth flags root-to-leaf traversals that exceed the configured depth.
type MaxDepth struct{}

func (r MaxDepth) ID() string { return "max-depth" }

func (r MaxDepth) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r MaxDepth) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")
	limit := defaultMaxDepth
	if rc, ok := cfg[r.ID()]; ok {
		if raw, ok := rc["limit"]; ok {
			switch n := raw.(type) {
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

	adj := make(map[string][]string)
	indegree := make(map[string]int)
	nodeByID := make(map[string]model.Node, len(d.Nodes))
	for _, node := range d.Nodes {
		nodeByID[node.ID] = node
		if _, ok := indegree[node.ID]; !ok {
			indegree[node.ID] = 0
		}
	}
	for _, edge := range d.Edges {
		adj[edge.From] = append(adj[edge.From], edge.To)
		indegree[edge.To]++
		if _, ok := indegree[edge.From]; !ok {
			indegree[edge.From] = 0
		}
	}

	starts := make([]string, 0)
	for nodeID, degree := range indegree {
		if degree == 0 {
			starts = append(starts, nodeID)
		}
	}
	if len(starts) == 0 {
		for nodeID := range indegree {
			starts = append(starts, nodeID)
		}
	}

	issues := make([]model.Issue, 0)
	for _, start := range starts {
		depth, path := longestPathFrom(start, adj, map[string]bool{})
		if depth <= limit {
			continue
		}

		issue := model.Issue{
			RuleID:   r.ID(),
			Severity: severity,
			Message:  fmt.Sprintf("path depth %d exceeds configured limit %d: %s", depth, limit, strings.Join(path, " -> ")),
			Context:  NodeSubgraphContext(d, start),
		}
		if node, ok := nodeByID[start]; ok {
			issue.Line = node.Line
			issue.Column = node.Column
		}
		issues = append(issues, issue)
	}

	return issues
}

func longestPathFrom(nodeID string, adj map[string][]string, visited map[string]bool) (int, []string) {
	if visited[nodeID] {
		return 0, []string{nodeID}
	}

	visited[nodeID] = true
	defer delete(visited, nodeID)

	nextNodes := adj[nodeID]
	if len(nextNodes) == 0 {
		return 0, []string{nodeID}
	}

	bestDepth := 0
	bestPath := []string{nodeID}
	for _, next := range nextNodes {
		depth, path := longestPathFrom(next, adj, visited)
		candidateDepth := depth + 1
		if candidateDepth > bestDepth {
			bestDepth = candidateDepth
			bestPath = append([]string{nodeID}, path...)
		}
	}

	return bestDepth, bestPath
}
