package model

// Diagram is the internal representation of a parsed Mermaid flowchart.
type Diagram struct {
	Direction    string
	Nodes        []Node
	Edges        []Edge
	Subgraphs    []Subgraph
	Suppressions []SuppressionDirective
}

// SuppressionDirective represents a source-level lint suppression comment.
type SuppressionDirective struct {
	RuleID     string
	Scope      string
	Line       int
	TargetLine int
}

// Node represents a single node in the diagram.
type Node struct {
	ID    string
	Label string
}

// Edge represents a directed connection between two nodes.
type Edge struct {
	From string
	To   string
	Type string
}

// Subgraph represents a named cluster of nodes.
type Subgraph struct {
	ID    string
	Label string
	Nodes []string
}

// Issue is a single lint finding produced by a rule.
type Issue struct {
	RuleID   string `json:"rule_id"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     *int   `json:"line,omitempty"`
	Column   *int   `json:"column,omitempty"`
}
