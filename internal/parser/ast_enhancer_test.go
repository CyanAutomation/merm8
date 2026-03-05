package parser

import (
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
)

func TestEnhanceASTWithSourceAnalysis_DisconnectedNodes(t *testing.T) {
	source := `graph TD
    A --> B
    C[Orphan]`

	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
		},
	}

	EnhanceASTWithSourceAnalysis(diagram, source)

	if len(diagram.DisconnectedNodeIDs) != 1 {
		t.Fatalf("expected 1 disconnected node, got %d: %v", len(diagram.DisconnectedNodeIDs), diagram.DisconnectedNodeIDs)
	}
	if diagram.DisconnectedNodeIDs[0] != "C" {
		t.Fatalf("expected C to be disconnected, got %v", diagram.DisconnectedNodeIDs)
	}
}

func TestEnhanceASTWithSourceAnalysis_DuplicateNodeIDs(t *testing.T) {
	source := `graph TD
    A[First]
    A[Second]
    B[Single]`

	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
		},
	}

	EnhanceASTWithSourceAnalysis(diagram, source)

	if len(diagram.DuplicateNodeIDs) != 1 {
		t.Fatalf("expected 1 duplicate node ID, got %d: %v", len(diagram.DuplicateNodeIDs), diagram.DuplicateNodeIDs)
	}
	if diagram.DuplicateNodeIDs[0] != "A" {
		t.Fatalf("expected A to be duplicate, got %v", diagram.DuplicateNodeIDs)
	}
}

func TestEnhanceASTWithSourceAnalysis_MultipleDisconnected(t *testing.T) {
	source := `A --> B
C[Orphan]
D[Another]`

	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
		},
	}

	EnhanceASTWithSourceAnalysis(diagram, source)

	if len(diagram.DisconnectedNodeIDs) != 2 {
		t.Fatalf("expected 2 disconnected nodes, got %d: %v", len(diagram.DisconnectedNodeIDs), diagram.DisconnectedNodeIDs)
	}
}

func TestEnhanceASTWithSourceAnalysis_AllNodesConnected(t *testing.T) {
	source := `graph TD
    A --> B
    B --> C`

	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "A"},
			{ID: "B"},
			{ID: "C"},
		},
		Edges: []model.Edge{
			{From: "A", To: "B"},
			{From: "B", To: "C"},
		},
	}

	EnhanceASTWithSourceAnalysis(diagram, source)

	if len(diagram.DisconnectedNodeIDs) != 0 {
		t.Fatalf("expected no disconnected nodes, got %v", diagram.DisconnectedNodeIDs)
	}
	if len(diagram.DuplicateNodeIDs) != 0 {
		t.Fatalf("expected no duplicate node IDs, got %v", diagram.DuplicateNodeIDs)
	}
}

func TestExtractAllNodeIDsFromSource_BasicPatterns(t *testing.T) {
	tests := []struct {
		name         string
		source       string
		wantContains []string // Just check that these node IDs are present
	}{
		{
			name:         "square brackets",
			source:       "A[Box]\nB[Another]",
			wantContains: []string{"A", "B"},
		},
		{
			name:         "parentheses",
			source:       "A(Round)\nB(Circle)",
			wantContains: []string{"A", "B"},
		},
		{
			name:         "curly braces",
			source:       "A{Diamond}\nB{Rhombus}",
			wantContains: []string{"A", "B"},
		},
		{
			name:         "no duplicate A in output",
			source:       "A[First]\nA[Second]",
			wantContains: []string{"A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAllNodeIDsFromSource(tt.source)
			gotSet := make(map[string]bool)
			for _, id := range got {
				gotSet[id] = true
			}
			for _, want := range tt.wantContains {
				if !gotSet[want] {
					t.Fatalf("expected node ID %q to be in result, got %v", want, got)
				}
			}
		})
	}
}

func TestFindDuplicateNodeIDs_VariousCounts(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string // Must be sorted
	}{
		{
			name:   "single duplicate",
			source: "A[First]\nA[Second]",
			want:   []string{"A"},
		},
		{
			name:   "multiple duplicates",
			source: "A[1]\nA[2]\nB(1)\nB(2)\nC{1}",
			want:   []string{"A", "B"},
		},
		{
			name:   "triple duplicate",
			source: "A[1]\nA[2]\nA[3]",
			want:   []string{"A"},
		},
		{
			name:   "no duplicates",
			source: "A[1]\nB[2]\nC[3]",
			want:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDuplicateNodeIDs(tt.source)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d duplicates, got %d: %v", len(tt.want), len(got), got)
			}
			for i, nodeID := range got {
				if nodeID != tt.want[i] {
					t.Fatalf("expected %q at position %d, got %q", tt.want[i], i, nodeID)
				}
			}
		})
	}
}

func TestEnhanceASTWithSourceAnalysis_NilDiagram(t *testing.T) {
	// Should not panic
	EnhanceASTWithSourceAnalysis(nil, "A --> B")
}

func TestEnhanceASTWithSourceAnalysis_EmptySource(t *testing.T) {
	diagram := &model.Diagram{Type: model.DiagramTypeFlowchart}
	// Should not panic
	EnhanceASTWithSourceAnalysis(diagram, "")

	if len(diagram.SourceNodeIDs) != 0 {
		t.Fatalf("expected empty SourceNodeIDs for empty source")
	}
}
