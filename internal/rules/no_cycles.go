package rules

import (
	"fmt"
	"strings"

	"github.com/CyanAutomation/merm8/internal/model"
)

// NoCycles flags directed cycles in flowcharts.
type NoCycles struct{}

func (r NoCycles) ID() string { return "no-cycles" }

func (r NoCycles) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r NoCycles) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "error")
	allowSelfLoops := false
	if rc, ok := cfg[r.ID()]; ok {
		if raw, ok := rc["allow-self-loop"]; ok {
			if value, ok := raw.(bool); ok {
				allowSelfLoops = value
			}
		}
	}

	nodeByID := make(map[string]model.Node, len(d.Nodes))
	for _, node := range d.Nodes {
		nodeByID[node.ID] = node
	}

	adj := make(map[string][]model.Edge)
	for _, edge := range d.Edges {
		adj[edge.From] = append(adj[edge.From], edge)
	}

	visited := map[string]bool{}
	onStack := map[string]bool{}
	stack := make([]string, 0)
	seenCycles := map[string]struct{}{}
	issues := make([]model.Issue, 0)

	var dfs func(string)
	dfs = func(nodeID string) {
		visited[nodeID] = true
		onStack[nodeID] = true
		stack = append(stack, nodeID)

		for _, edge := range adj[nodeID] {
			if allowSelfLoops && edge.From == edge.To {
				continue
			}

			next := edge.To
			if !visited[next] {
				dfs(next)
				continue
			}

			if onStack[next] {
				cycleStart := -1
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == next {
						cycleStart = i
						break
					}
				}
				if cycleStart == -1 {
					continue
				}

				cycleNodes := append([]string{}, stack[cycleStart:]...)
				cycleNodes = append(cycleNodes, next)
				cycleKey := normalizedCycleKey(cycleNodes)
				if _, exists := seenCycles[cycleKey]; exists {
					continue
				}
				seenCycles[cycleKey] = struct{}{}

				issue := model.Issue{
					RuleID:   r.ID(),
					Severity: severity,
					Message:  fmt.Sprintf("directed cycle detected: %s", strings.Join(cycleNodes, " -> ")),
					Context:  NodeSubgraphContext(d, next),
				}
				if node, ok := nodeByID[next]; ok {
					issue.Line = node.Line
					issue.Column = node.Column
				} else {
					issue.Line = edge.Line
					issue.Column = edge.Column
				}
				issues = append(issues, issue)
			}
		}

		stack = stack[:len(stack)-1]
		onStack[nodeID] = false
	}

	for _, node := range d.Nodes {
		if !visited[node.ID] {
			dfs(node.ID)
		}
	}
	for from := range adj {
		if !visited[from] {
			dfs(from)
		}
	}

	return issues
}

func normalizedCycleKey(cycleNodes []string) string {
	if len(cycleNodes) == 0 {
		return ""
	}

	ring := append([]string{}, cycleNodes...)
	if len(ring) > 1 && ring[0] == ring[len(ring)-1] {
		ring = ring[:len(ring)-1]
	}

	minIdx := 0
	for i := 1; i < len(ring); i++ {
		if ring[i] < ring[minIdx] {
			minIdx = i
		}
	}

	normalized := append(append([]string{}, ring[minIdx:]...), ring[:minIdx]...)
	return strings.Join(normalized, "->")
}
