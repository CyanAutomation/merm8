package rules

import (
	"fmt"
	"math"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultMaxFanout = 5

// MaxFanout warns when any node has more outgoing edges than the configured
// limit (default 5).
type MaxFanout struct{}

func (r MaxFanout) ID() string { return "max-fanout" }

func (r MaxFanout) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (r MaxFanout) Run(d *model.Diagram, cfg Config) []model.Issue {
	severity := EffectiveSeverity(r.ID(), cfg, "warning")

	limit := defaultMaxFanout
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

	fanout := make(map[string]int, len(d.Nodes))
	nodeLocations := make(map[string]model.Node, len(d.Nodes))
	for _, node := range d.Nodes {
		nodeLocations[node.ID] = node
	}
	edgeLocations := make(map[string]model.Edge)
	for _, e := range d.Edges {
		fanout[e.From]++
		if _, exists := edgeLocations[e.From]; !exists {
			edgeLocations[e.From] = e
		}
	}

	var issues []model.Issue
	for nodeID, count := range fanout {
		if count > limit {
			issue := model.Issue{
				RuleID:   r.ID(),
				Severity: severity,
				Message:  fmt.Sprintf("node %q has fanout %d, exceeding limit of %d", nodeID, count, limit),
				Context:  NodeSubgraphContext(d, nodeID),
			}
			if node, ok := nodeLocations[nodeID]; ok {
				issue.Line = node.Line
				issue.Column = node.Column
			} else if edge, ok := edgeLocations[nodeID]; ok {
				issue.Line = edge.Line
				issue.Column = edge.Column
			}
			issues = append(issues, issue)
		}
	}
	return issues
}
