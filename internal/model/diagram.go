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

// RecognizedDiagramTypes returns Mermaid diagram types currently identified by
// the parser.
func RecognizedDiagramTypes() []DiagramType {
	return []DiagramType{
		DiagramTypeFlowchart,
		DiagramTypeSequence,
		DiagramTypeClass,
		DiagramTypeER,
		DiagramTypeState,
	}
}

// DiagramTypesForFamily returns the parser diagram types belonging to a family.
func DiagramTypesForFamily(family DiagramFamily) []DiagramType {
	switch family {
	case DiagramFamilyFlowchart:
		return []DiagramType{DiagramTypeFlowchart}
	case DiagramFamilySequence:
		return []DiagramType{DiagramTypeSequence}
	case DiagramFamilyClass:
		return []DiagramType{DiagramTypeClass}
	case DiagramFamilyER:
		return []DiagramType{DiagramTypeER}
	case DiagramFamilyState:
		return []DiagramType{DiagramTypeState}
	default:
		return nil
	}
}

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
		return DiagramFamilyUnknown
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
	ID     string
	Label  string
	Line   *int
	Column *int
}

// Edge represents a directed connection between two nodes.
type Edge struct {
	From   string
	To     string
	Type   string
	Line   *int
	Column *int
}

// Subgraph represents a named cluster of nodes.
type Subgraph struct {
	ID    string
	Label string
	Nodes []string
}

// Issue is a single lint finding produced by a rule.
type Issue struct {
	RuleID      string        `json:"rule-id"`
	Severity    string        `json:"severity"`
	Message     string        `json:"message"`
	Line        *int          `json:"line,omitempty"`
	Column      *int          `json:"column,omitempty"`
	Fingerprint string        `json:"fingerprint"`
	Context     *IssueContext `json:"context,omitempty"`
}

// IssueContext captures optional grouping information for an issue.
type IssueContext struct {
	SubgraphID    string `json:"subgraph-id,omitempty"`
	SubgraphLabel string `json:"subgraph-label,omitempty"`
}
