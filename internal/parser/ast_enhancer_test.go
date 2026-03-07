package parser

import (
	"reflect"
	"sort"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
)

func assertNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

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
		name              string
		source            string
		wantIDsInOrder    []string
		forbiddenIDs      []string
		downstreamEdgeIDs map[string]bool
		wantDisconnected  []string
		wantDuplicates    []string
	}{
		{
			name:              "square brackets",
			source:            "A[Box]\nB[Another]",
			wantIDsInOrder:    []string{"A", "B"},
			forbiddenIDs:      []string{"graph", "TD", "Box"},
			downstreamEdgeIDs: map[string]bool{"A": true},
			wantDisconnected:  []string{"B"},
			wantDuplicates:    []string{},
		},
		{
			name:              "parentheses",
			source:            "A(Round)\nB(Circle)",
			wantIDsInOrder:    []string{"A", "B"},
			forbiddenIDs:      []string{"Round", "Circle"},
			downstreamEdgeIDs: map[string]bool{"A": true, "B": true},
			wantDisconnected:  []string{},
			wantDuplicates:    []string{},
		},
		{
			name:              "curly braces",
			source:            "A{Diamond}\nB{Rhombus}",
			wantIDsInOrder:    []string{"A", "B"},
			forbiddenIDs:      []string{"Diamond", "Rhombus"},
			downstreamEdgeIDs: map[string]bool{"A": true},
			wantDisconnected:  []string{"B"},
			wantDuplicates:    []string{},
		},
		{
			name:              "no duplicate A in output",
			source:            "graph TD\nA[First]\nA[Second]\nB --> C\nclassDef foo fill:#f9f,stroke:#333\nsubgraph cluster_1\nend",
			wantIDsInOrder:    []string{"A"},
			forbiddenIDs:      []string{"graph", "TD", "classDef", "foo", "subgraph", "cluster_1", "end", "B", "C", "First", "Second"},
			downstreamEdgeIDs: map[string]bool{},
			wantDisconnected:  []string{"A"},
			wantDuplicates:    []string{"A"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAllNodeIDsFromSource(tt.source)

			if len(got) != len(tt.wantIDsInOrder) {
				t.Fatalf("expected %d node IDs, got %d: %v", len(tt.wantIDsInOrder), len(got), got)
			}
			for i, want := range tt.wantIDsInOrder {
				if got[i] != want {
					t.Fatalf("expected node ID %q at position %d, got %q (all=%v)", want, i, got[i], got)
				}
			}

			gotSet := make(map[string]bool, len(got))
			for _, id := range got {
				gotSet[id] = true
			}
			for _, forbidden := range tt.forbiddenIDs {
				if gotSet[forbidden] {
					t.Fatalf("unexpected non-node token %q captured from source, got IDs %v", forbidden, got)
				}
			}

			var disconnected []string
			for _, id := range got {
				if !tt.downstreamEdgeIDs[id] {
					disconnected = append(disconnected, id)
				}
			}
			normalize := func(ids []string) []string {
				if ids == nil {
					return []string{}
				}
				return ids
			}

			if !reflect.DeepEqual(normalize(disconnected), normalize(tt.wantDisconnected)) {
				t.Fatalf("disconnected-node input mismatch: want=%v got=%v from source IDs=%v", tt.wantDisconnected, disconnected, got)
			}

			duplicates := findDuplicateNodeIDs(tt.source)
			if !reflect.DeepEqual(normalize(duplicates), normalize(tt.wantDuplicates)) {
				t.Fatalf("duplicate-node input mismatch: want=%v got=%v", tt.wantDuplicates, duplicates)
			}
		})
	}
}

func TestFindDuplicateNodeIDs_VariousCounts(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   []string
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
			name:   "no duplicates",
			source: "A[1]\nB[2]\nC[3]",
			want:   []string{},
		},
		{
			name: "hyphenated and mixed styles",
			source: `graph TD
    service-node[First]
    service-node[Second]
    node_2(One)
    node_2(Two)
    alpha123{Single}`,
			want: []string{"node_2", "service-node"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDuplicateNodeIDs(tt.source)

			// The parser contract is to return duplicate IDs in deterministic
			// lexical order, so assert ordering explicitly.
			if !sort.StringsAreSorted(got) {
				t.Fatalf("expected duplicate IDs to be sorted lexically, got %v", got)
			}

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

func TestExtractAllNodeIDsFromSource_HyphenatedAndMixedStyles(t *testing.T) {
	source := `graph TD
    service-node[Service]
    node_2(Round)
    alpha123{Decision}
    service-node[Service Updated]`

	got := extractAllNodeIDsFromSource(source)
	want := []string{"service-node", "node_2", "alpha123"}
	if len(got) != len(want) {
		t.Fatalf("expected %d node IDs, got %d: %v", len(want), len(got), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("expected %q at position %d, got %q", id, i, got[i])
		}
	}
}

func TestEnhanceASTWithSourceAnalysis_HyphenatedDuplicateAndDisconnected(t *testing.T) {
	source := `graph TD
    service-node[Start]
    service-node[Updated]
    service-node --> node_2
    node_2[Worker]
    extra-node[Orphan]`

	diagram := &model.Diagram{
		Type: model.DiagramTypeFlowchart,
		Nodes: []model.Node{
			{ID: "service-node"},
			{ID: "node_2"},
		},
		Edges: []model.Edge{
			{From: "service-node", To: "node_2"},
		},
	}

	EnhanceASTWithSourceAnalysis(diagram, source)

	if len(diagram.DuplicateNodeIDs) != 1 || diagram.DuplicateNodeIDs[0] != "service-node" {
		t.Fatalf("expected duplicate [service-node], got %v", diagram.DuplicateNodeIDs)
	}
	if len(diagram.DisconnectedNodeIDs) != 1 || diagram.DisconnectedNodeIDs[0] != "extra-node" {
		t.Fatalf("expected disconnected [extra-node], got %v", diagram.DisconnectedNodeIDs)
	}
}
func TestEnhanceASTWithSourceAnalysis_NilDiagram_DefensiveNoPanicAndNoBehaviorMutation(t *testing.T) {
	// Defensive API contract:
	// nil diagram is treated as a no-op that returns immediately and never panics.
	// To guard against accidental parser-wide state mutation, verify the nil call
	// does not change behavior of a subsequent non-nil enhancement.
	source := "graph TD\nA[One]\nA[Two]\nservice-node[Three]\nA --> service-node"

	expected := &model.Diagram{Type: model.DiagramTypeFlowchart}
	EnhanceASTWithSourceAnalysis(expected, source)

	assertNoPanic(t, func() {
		EnhanceASTWithSourceAnalysis(nil, "A --> B")
	})

	actual := &model.Diagram{Type: model.DiagramTypeFlowchart}
	EnhanceASTWithSourceAnalysis(actual, source)

	if !reflect.DeepEqual(expected.SourceNodeIDs, actual.SourceNodeIDs) {
		t.Fatalf("expected SourceNodeIDs to be unchanged after nil-call; expected=%v actual=%v", expected.SourceNodeIDs, actual.SourceNodeIDs)
	}
	if !reflect.DeepEqual(expected.DisconnectedNodeIDs, actual.DisconnectedNodeIDs) {
		t.Fatalf("expected DisconnectedNodeIDs to be unchanged after nil-call; expected=%v actual=%v", expected.DisconnectedNodeIDs, actual.DisconnectedNodeIDs)
	}
	if !reflect.DeepEqual(expected.DuplicateNodeIDs, actual.DuplicateNodeIDs) {
		t.Fatalf("expected DuplicateNodeIDs to be unchanged after nil-call; expected=%v actual=%v", expected.DuplicateNodeIDs, actual.DuplicateNodeIDs)
	}
}

func TestEnhanceASTWithSourceAnalysis_EmptySource(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "empty source", source: ""},
		{name: "whitespace only source", source: "  \n\t  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram := &model.Diagram{
				Type: model.DiagramTypeFlowchart,
				Nodes: []model.Node{
					{ID: "A", Label: "Alpha"},
					{ID: "B", Label: "Beta"},
				},
				Edges: []model.Edge{
					{From: "A", To: "B"},
				},
			}

			expectedNodes := append([]model.Node(nil), diagram.Nodes...)
			expectedEdges := append([]model.Edge(nil), diagram.Edges...)

			EnhanceASTWithSourceAnalysis(diagram, tt.source)

			if len(diagram.SourceNodeIDs) != 0 {
				t.Fatalf("expected empty SourceNodeIDs, got %v", diagram.SourceNodeIDs)
			}
			if len(diagram.DuplicateNodeIDs) != 0 {
				t.Fatalf("expected empty DuplicateNodeIDs, got %v", diagram.DuplicateNodeIDs)
			}
			if len(diagram.DisconnectedNodeIDs) != 0 {
				t.Fatalf("expected empty DisconnectedNodeIDs, got %v", diagram.DisconnectedNodeIDs)
			}

			if !reflect.DeepEqual(diagram.Nodes, expectedNodes) {
				t.Fatalf("expected diagram nodes to remain unchanged; expected=%v actual=%v", expectedNodes, diagram.Nodes)
			}
			if !reflect.DeepEqual(diagram.Edges, expectedEdges) {
				t.Fatalf("expected diagram edges to remain unchanged; expected=%v actual=%v", expectedEdges, diagram.Edges)
			}
		})
	}
}
