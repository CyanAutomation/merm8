package rules

import "github.com/CyanAutomation/merm8/internal/model"

// NoDisconnectedNodes flags nodes that have no incoming or outgoing edges.
// A single-node diagram (no edges at all) is exempt to avoid false positives.
type NoDisconnectedNodes struct{}

func (r NoDisconnectedNodes) ID() string { return "no-disconnected-nodes" }

func (r NoDisconnectedNodes) Run(d *model.Diagram, _ Config) []model.Issue {
	if len(d.Edges) == 0 {
		return nil
	}
	connected := make(map[string]bool, len(d.Nodes))
	for _, e := range d.Edges {
		connected[e.From] = true
		connected[e.To] = true
	}
	var issues []model.Issue
	for _, n := range d.Nodes {
		if !connected[n.ID] {
			issues = append(issues, model.Issue{
				RuleID:   r.ID(),
				Severity: "error",
				Message:  "node is disconnected: " + n.ID,
			})
		}
	}
	return issues
}
