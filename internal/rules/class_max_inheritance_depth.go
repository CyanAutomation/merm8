package rules

import (
	"fmt"
	"math"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultClassMaxInheritanceDepth = 5

// ClassMaxInheritanceDepth flags inheritance hierarchies that exceed the
// configured depth limit.
type ClassMaxInheritanceDepth struct{}

func (r ClassMaxInheritanceDepth) ID() string { return "max-inheritance-depth" }

func (r ClassMaxInheritanceDepth) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyClass}
}

func (r ClassMaxInheritanceDepth) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")

	limit := defaultClassMaxInheritanceDepth
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

	// Build adjacency list for inheritance relationships (extension type)
	// Edge direction: child → parent (e.g., Derived → Base means Derived extends Base)
	// We need to traverse from leaves (most derived) to root (base class)
	// So we build reverse adjacency: parent → children
	reverseAdj := make(map[string][]string)
	indegree := make(map[string]int)
	nodeByID := make(map[string]model.Node, len(d.Nodes))

	for _, node := range d.Nodes {
		nodeByID[node.ID] = node
		if _, ok := indegree[node.ID]; !ok {
			indegree[node.ID] = 0
		}
	}

	for _, edge := range d.Edges {
		if edge.Type == "extension" {
			// edge.From extends edge.To, so edge.To is parent of edge.From
			reverseAdj[edge.To] = append(reverseAdj[edge.To], edge.From)
			indegree[edge.From]++
			if _, ok := indegree[edge.To]; !ok {
				indegree[edge.To] = 0
			}
		}
	}

	// Find root classes (no incoming inheritance - base classes)
	roots := make([]string, 0)
	for nodeID, degree := range indegree {
		if degree == 0 {
			roots = append(roots, nodeID)
		}
	}

	if len(roots) == 0 {
		return nil
	}

	// Calculate max depth from each root (traverse down to leaves)
	var issues []model.Issue
	for _, root := range roots {
		depth := calculateInheritanceDepth(root, reverseAdj, make(map[string]bool))
		if depth > limit {
			node := nodeByID[root]
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  fmt.Sprintf("inheritance depth %d from %q exceeds limit %d", depth, root, limit),
				Line:     node.Line,
				Column:   node.Column,
			})
		}
	}

	return issues
}

func calculateInheritanceDepth(node string, adj map[string][]string, visited map[string]bool) int {
	if visited[node] {
		return 0 // Avoid cycles
	}
	visited[node] = true
	defer func() { visited[node] = false }()

	maxChildDepth := 0
	for _, child := range adj[node] {
		childDepth := calculateInheritanceDepth(child, adj, visited)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}

	return maxChildDepth + 1
}
