package rules

import (
	"fmt"
	"math"
	"sort"
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
		starts = sccRepresentatives(adj, indegree)
	}

	issues := make([]model.Issue, 0)
	seenPaths := make(map[string]struct{})
	memo := make(map[string]pathResult)
	sort.Strings(starts)
	for _, start := range starts {
		depth, path := longestPathFrom(start, adj, map[string]bool{}, memo)
		if depth <= limit {
			continue
		}

		pathKey := strings.Join(path, "->")
		if _, exists := seenPaths[pathKey]; exists {
			continue
		}
		seenPaths[pathKey] = struct{}{}

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

type pathResult struct {
	depth int
	path  []string
}

func longestPathFrom(nodeID string, adj map[string][]string, visited map[string]bool, memo map[string]pathResult) (int, []string) {
	depth, path, _ := longestPathFromWithCycleInfo(nodeID, adj, visited, memo)
	return depth, path
}

func longestPathFromWithCycleInfo(nodeID string, adj map[string][]string, visited map[string]bool, memo map[string]pathResult) (int, []string, bool) {
	if visited[nodeID] {
		return 0, []string{nodeID}, true
	}
	if cached, ok := memo[nodeID]; ok {
		return cached.depth, append([]string(nil), cached.path...), false
	}

	visited[nodeID] = true
	defer delete(visited, nodeID)

	nextNodes := adj[nodeID]
	if len(nextNodes) == 0 {
		return 0, []string{nodeID}, false
	}

	bestDepth := 0
	bestPath := []string{nodeID}
	cacheable := true
	hasCycle := false
	for _, next := range nextNodes {
		if visited[next] {
			cacheable = false
			hasCycle = true
		}

		depth, path, childHasCycle := longestPathFromWithCycleInfo(next, adj, visited, memo)
		if childHasCycle {
			cacheable = false
			hasCycle = true
		}
		candidateDepth := depth + 1
		if candidateDepth > bestDepth {
			bestDepth = candidateDepth
			bestPath = append([]string{nodeID}, path...)
		}
	}
	if cacheable {
		memo[nodeID] = pathResult{depth: bestDepth, path: append([]string(nil), bestPath...)}
	}

	return bestDepth, bestPath, hasCycle
}

func sccRepresentatives(adj map[string][]string, indegree map[string]int) []string {
	index := 0
	indices := map[string]int{}
	lowlink := map[string]int{}
	onStack := map[string]bool{}
	stack := make([]string, 0)
	representatives := make([]string, 0)

	var strongConnect func(string)
	strongConnect = func(v string) {
		indices[v] = index
		lowlink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range adj[v] {
			if _, seen := indices[w]; !seen {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] && indices[w] < lowlink[v] {
				lowlink[v] = indices[w]
			}
		}

		if lowlink[v] == indices[v] {
			component := make([]string, 0)
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				component = append(component, w)
				if w == v {
					break
				}
			}
			sort.Strings(component)
			representatives = append(representatives, component[0])
		}
	}

	nodes := make([]string, 0, len(indegree))
	for nodeID := range indegree {
		nodes = append(nodes, nodeID)
	}
	sort.Strings(nodes)
	for _, nodeID := range nodes {
		if _, seen := indices[nodeID]; !seen {
			strongConnect(nodeID)
		}
	}

	sort.Strings(representatives)
	return representatives
}
