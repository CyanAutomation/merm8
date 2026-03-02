package rules

import (
	"fmt"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultMaxFanout = 5

// MaxFanout warns when any node has more outgoing edges than the configured
// limit (default 5).
type MaxFanout struct{}

func (r MaxFanout) ID() string { return "max-fanout" }

func (r MaxFanout) Run(d *model.Diagram, cfg Config) []model.Issue {
	limit := defaultMaxFanout
	if rc, ok := cfg[r.ID()]; ok {
		if v, ok := rc["limit"]; ok {
			switch n := v.(type) {
			case int:
				limit = n
			case float64:
				limit = int(n)
			}
		}
	}

	fanout := make(map[string]int, len(d.Nodes))
	for _, e := range d.Edges {
		fanout[e.From]++
	}

	var issues []model.Issue
	for nodeID, count := range fanout {
		if count > limit {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: "warn",
				Message:  fmt.Sprintf("node %q has fanout %d, exceeding limit of %d", nodeID, count, limit),
			})
		}
	}
	return issues
}
