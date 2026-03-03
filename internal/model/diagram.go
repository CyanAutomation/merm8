package model

// DiagramType identifies a Mermaid diagram type returned by the parser.
type DiagramType string

const (
	DiagramTypeUnknown   DiagramType = "unknown"
	DiagramTypeFlowchart DiagramType = "flowchart"
	DiagramTypeSequence  DiagramType = "sequence"
	DiagramTypeClass     DiagramType = "class"
	DiagramTypeER        DiagramType = "er"
	DiagramTypeState     DiagramType = "state"
)

// DiagramFamily groups related Mermaid diagram types.
type DiagramFamily string

const (
	DiagramFamilyUnknown   DiagramFamily = "unknown"
	DiagramFamilyFlowchart DiagramFamily = "flowchart"
	DiagramFamilySequence  DiagramFamily = "sequence"
	DiagramFamilyClass     DiagramFamily = "class"
	DiagramFamilyER        DiagramFamily = "er"
	DiagramFamilyState     DiagramFamily = "state"
)

// Family returns the normalized family for the diagram type.
func (t DiagramType) Family() DiagramFamily {
	switch t {
	case DiagramTypeFlowchart:
		return DiagramFamilyFlowchart
	case DiagramTypeSequence:
		return DiagramFamilySequence
	case DiagramTypeClass:
		return DiagramFamilyClass
	case DiagramTypeER:
		return DiagramFamilyER
	case DiagramTypeState:
		return DiagramFamilyState
	case "":
		return DiagramFamilyFlowchart
	default:
		return DiagramFamilyUnknown
	}
}

// Diagram is the internal representation of a parsed Mermaid diagram.
type Diagram struct {
	Type         DiagramType
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
