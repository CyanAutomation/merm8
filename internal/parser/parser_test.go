// Package parser_test tests the Node.js subprocess integration.
package parser_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

// getParserScript returns the path to the Node.js parser script.
// It checks PARSER_SCRIPT env var first, then looks for the script relative to the repo root.
func getParserScript(t *testing.T) string {
	// First try environment variable
	if script := os.Getenv("PARSER_SCRIPT"); script != "" {
		t.Logf("using PARSER_SCRIPT from env: %s", script)
		if _, err := os.Stat(script); err == nil {
			return script
		}
		t.Logf("PARSER_SCRIPT=%s does not exist, will try default", script)
	}

	// Look for the script relative to repo root
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}

	// Try to find the repo root by looking for go.mod
	for {
		gomod := filepath.Join(cwd, "go.mod")
		if _, err := os.Stat(gomod); err == nil {
			// Found go.mod, parser should be here
			script := filepath.Join(cwd, "parser-node", "parse.ts")
			return script
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break // reached root
		}
		cwd = parent
	}

	t.Fatalf("could not locate parser-node/parse.ts. Set PARSER_SCRIPT env var")
	return ""
}

func writeMockParserFixture(t *testing.T, directory, filename string) string {
	t.Helper()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate parser test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	source := filepath.Join(repoRoot, "parser-node", "test-fixtures", "bridge-mock.ts")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read TypeScript parser fixture: %v", err)
	}

	path := filepath.Join(directory, filename)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write TypeScript parser fixture: %v", err)
	}
	return path
}

func mustNewParser(t *testing.T, scriptPath string) *parser.Parser {
	t.Helper()

	t.Setenv("PARSER_MODE", "subprocess")
	p, err := parser.NewWithConfig(scriptPath, parser.Config{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	return p
}

// TestParser_ValidFlowchart tests parsing a valid flowchart.
func TestParser_ValidFlowchart(t *testing.T) {
	script := getParserScript(t)

	p := mustNewParser(t, script)

	mermaidCode := `graph TD
    A[Start]
    B[Process]
    C[End]
    A --> B
    B --> C`

	diagram, syntaxErr, err := p.Parse(mermaidCode)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	// Verify basic diagram structure
	if len(diagram.Nodes) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(diagram.Nodes))
	}
	if len(diagram.Edges) != 2 {
		t.Errorf("expected 2 edges, got %d", len(diagram.Edges))
	}
	if diagram.Direction != "TD" {
		t.Errorf("expected direction=TD, got %v", diagram.Direction)
	}

	// Verify node IDs
	nodeIDs := make(map[string]bool)
	for _, n := range diagram.Nodes {
		nodeIDs[n.ID] = true
	}
	expected := map[string]bool{"A": true, "B": true, "C": true}
	for id := range expected {
		if !nodeIDs[id] {
			t.Errorf("expected node %s not found", id)
		}
	}

	// Verify edges
	if len(diagram.Edges) >= 2 {
		if diagram.Edges[0].From != "A" || diagram.Edges[0].To != "B" {
			t.Errorf("expected edge A -> B, got %s -> %s", diagram.Edges[0].From, diagram.Edges[0].To)
		}
		if diagram.Edges[1].From != "B" || diagram.Edges[1].To != "C" {
			t.Errorf("expected edge b -> C, got %s -> %s", diagram.Edges[1].From, diagram.Edges[1].To)
		}
	}
}

func TestParser_FlowchartIncludesNodeAndEdgeLocations(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	mermaidCode := `graph TD
  A[Start] --> B[End]`
	diagram, syntaxErr, err := p.Parse(mermaidCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	var nodeA *model.Node
	for i := range diagram.Nodes {
		if diagram.Nodes[i].ID == "A" {
			nodeA = &diagram.Nodes[i]
			break
		}
	}
	if nodeA == nil {
		t.Fatalf("expected node A in diagram nodes: %#v", diagram.Nodes)
	}
	if nodeA.Line == nil || *nodeA.Line != 2 {
		t.Fatalf("expected node A line=2, got %v", nodeA.Line)
	}
	if nodeA.Column == nil || *nodeA.Column != 3 {
		t.Fatalf("expected node A column=3, got %v", nodeA.Column)
	}

	if len(diagram.Edges) == 0 {
		t.Fatal("expected at least one edge")
	}
	edge := diagram.Edges[0]
	if edge.Line == nil || *edge.Line != 2 {
		t.Fatalf("expected edge line=2, got %v", edge.Line)
	}
	if edge.Column == nil || *edge.Column != 3 {
		t.Fatalf("expected edge column=3, got %v", edge.Column)
	}
}

func TestParser_FlowchartLocationLookupPreservesCase(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	mermaidCode := `graph TD
  A[Start] --> B[End]`
	diagram, syntaxErr, err := p.Parse(mermaidCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	var nodeA *model.Node
	for i := range diagram.Nodes {
		if diagram.Nodes[i].ID == "A" {
			nodeA = &diagram.Nodes[i]
			break
		}
	}
	if nodeA == nil {
		t.Fatalf("expected node A in diagram nodes: %#v", diagram.Nodes)
	}
	if nodeA.Line == nil || *nodeA.Line != 2 {
		t.Fatalf("expected node line=2 for uppercase DB id against uppercase source, got %v", nodeA.Line)
	}
	if nodeA.Column == nil || *nodeA.Column != 3 {
		t.Fatalf("expected node column=3 for uppercase DB id against uppercase source, got %v", nodeA.Column)
	}

	if len(diagram.Edges) == 0 {
		t.Fatal("expected at least one edge")
	}
	edge := diagram.Edges[0]
	if edge.Line == nil || *edge.Line != 2 {
		t.Fatalf("expected edge line=2 for case-preserved DB ids against source, got %v", edge.Line)
	}
	if edge.Column == nil || *edge.Column != 3 {
		t.Fatalf("expected edge column=3 for case-preserved DB ids against source, got %v", edge.Column)
	}
}

// TestParser_InvalidMermaid tests parsing invalid Mermaid code.

func TestParser_FlowchartPreservesCaseSensitiveNodeIdentity(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	mermaidCode := `graph TD
  A[Upper]
  a[Lower]
  A --> a
  a --> A`

	diagram, syntaxErr, err := p.Parse(mermaidCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	nodeIDs := make(map[string]bool)
	for _, n := range diagram.Nodes {
		nodeIDs[n.ID] = true
	}
	if !nodeIDs["A"] || !nodeIDs["a"] {
		t.Fatalf("expected distinct case-sensitive node IDs A and a, got %#v", diagram.Nodes)
	}

	seenUpperToLower := false
	seenLowerToUpper := false
	for _, e := range diagram.Edges {
		if e.From == "A" && e.To == "a" {
			seenUpperToLower = true
		}
		if e.From == "a" && e.To == "A" {
			seenLowerToUpper = true
		}
	}
	if !seenUpperToLower || !seenLowerToUpper {
		t.Fatalf("expected edges A->a and a->A, got %#v", diagram.Edges)
	}
}

func TestParser_FlowchartLocationWithLeadingAndTrailingBlankLines(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	mermaidCode := `

graph TD
  A[Start] --> B[End]

`

	diagram, syntaxErr, err := p.Parse(mermaidCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	if len(diagram.Edges) == 0 {
		t.Fatal("expected at least one edge")
	}
	edge := diagram.Edges[0]
	if edge.Line == nil || *edge.Line != 4 {
		t.Fatalf("expected edge line=4 with leading blanks preserved, got %v", edge.Line)
	}
	if edge.Column == nil || *edge.Column != 3 {
		t.Fatalf("expected edge column=3, got %v", edge.Column)
	}
}

func TestParser_InvalidMermaid(t *testing.T) {
	script := getParserScript(t)

	p := mustNewParser(t, script)

	mermaidCode := "this is not valid mermaid at all"

	diagram, syntaxErr, err := p.Parse(mermaidCode)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr == nil {
		t.Fatal("expected syntax error for invalid mermaid, got nil")
	}
	if diagram != nil {
		t.Error("expected nil diagram for syntax error")
	}

	// Verify syntax error contains useful info
	if syntaxErr.Message == "" {
		t.Error("expected syntax error message")
	}
	t.Logf("syntax error: %s (line %d, col %d)", syntaxErr.Message, syntaxErr.Line, syntaxErr.Column)
}

// TestParser_EmptyCode tests parsing empty input.
func TestParser_EmptyCode(t *testing.T) {
	script := getParserScript(t)

	p := mustNewParser(t, script)

	diagram, syntaxErr, err := p.Parse("")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr == nil {
		t.Fatal("expected syntax error for empty input, got nil")
	}
	if diagram != nil {
		t.Error("expected nil diagram for empty input")
	}
}

func TestParser_WhitespaceOnlyCode(t *testing.T) {
	script := getParserScript(t)

	p := mustNewParser(t, script)

	diagram, syntaxErr, err := p.Parse(`  
	
  `)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr == nil {
		t.Fatal("expected syntax error for whitespace-only input, got nil")
	}
	if diagram != nil {
		t.Error("expected nil diagram for whitespace-only input")
	}
}

// TestParser_WithDirection tests parsing diagrams with all supported directions.
func TestParser_WithDirection(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	tests := []struct {
		name      string
		code      string
		direction string
	}{
		{"Top-Down (TD)", "graph TD\n  A-->B", "TD"},
		{"Left-Right (LR)", "graph LR\n  A-->B", "LR"},
		{"Bottom-Up (BT)", "graph BT\n  A-->B", "BT"},
		{"Right-Left (RL)", "graph RL\n  A-->B", "RL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram, syntaxErr, err := p.Parse(tt.code)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if syntaxErr != nil {
				t.Fatalf("unexpected syntax error: %+v", syntaxErr)
			}
			if diagram == nil {
				t.Fatal("expected diagram, got nil")
			}
			if diagram.Direction != tt.direction {
				t.Errorf("expected direction=%s, got %s", tt.direction, diagram.Direction)
			}
			// Verify diagram structure is preserved regardless of direction
			if len(diagram.Nodes) != 2 {
				t.Errorf("expected 2 nodes, got %d", len(diagram.Nodes))
			}
			if len(diagram.Edges) != 1 {
				t.Errorf("expected 1 edge, got %d", len(diagram.Edges))
			}
		})
	}
}

// TestParser_MultipleEdges tests parsing diagrams with multiple edges from one node.
func TestParser_MultipleEdges(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	mermaidCode := `graph TD
    A[Start]
    B[Option 1]
    C[Option 2]
    D[Option 3]
    A --> B
    A --> C
    A --> D
    B --> |result1| D
    C --> |result2| D`

	diagram, syntaxErr, err := p.Parse(mermaidCode)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	if len(diagram.Nodes) != 4 {
		t.Errorf("expected 4 nodes, got %d", len(diagram.Nodes))
	}
	if len(diagram.Edges) != 5 {
		t.Errorf("expected 5 edges, got %d", len(diagram.Edges))
	}

	// Count outgoing edges from A
	aOutgoing := 0
	for _, e := range diagram.Edges {
		if e.From == "A" {
			aOutgoing++
		}
	}
	if aOutgoing != 3 {
		t.Errorf("expected 3 edges from A, got %d", aOutgoing)
	}
}

func TestParser_DiagramTypeMetadata(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	tests := []struct {
		name     string
		code     string
		wantType string
	}{
		{name: "flowchart", code: "graph TD\n  A-->B", wantType: "flowchart"},
		{name: "sequence", code: "sequenceDiagram\n  Alice->>Bob: Hi", wantType: "sequence"},
		{name: "class", code: "classDiagram\n  class Animal", wantType: "class"},
		{name: "er", code: "erDiagram\n  CUSTOMER ||--o{ ORDER : places", wantType: "er"},
		{name: "state", code: "stateDiagram-v2\n  [*] --> Active", wantType: "state"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagram, syntaxErr, err := p.Parse(tt.code)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if syntaxErr != nil {
				t.Fatalf("unexpected syntax error: %+v", syntaxErr)
			}
			if diagram == nil {
				t.Fatal("expected diagram, got nil")
			}
			if got := string(diagram.Type); got != tt.wantType {
				t.Fatalf("expected type %q, got %q", tt.wantType, got)
			}
		})
	}
}

func TestParser_SuppressionDirectiveAliasesAndNextLineTarget(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	code := `graph TD
%% merm8-ignore-next-line MAX-FANOUT
A-->B
A-->C
%% merm8-ignore all
A-->D`

	diagram, syntaxErr, err := p.Parse(code)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}
	if len(diagram.Suppressions) != 2 {
		t.Fatalf("expected 2 suppressions, got %#v", diagram.Suppressions)
	}

	nextLine := diagram.Suppressions[0]
	if nextLine.RuleID != "max-fanout" {
		t.Fatalf("expected lowercased next-line rule id, got %q", nextLine.RuleID)
	}
	if nextLine.Scope != "next-line" {
		t.Fatalf("expected next-line scope, got %q", nextLine.Scope)
	}
	if nextLine.Line != 2 {
		t.Fatalf("expected directive line 2, got %d", nextLine.Line)
	}
	if nextLine.TargetLine != 3 {
		t.Fatalf("expected next-line target line 3, got %d", nextLine.TargetLine)
	}

	fileScope := diagram.Suppressions[1]
	if fileScope.RuleID != "all" {
		t.Fatalf("expected all suppression rule id, got %q", fileScope.RuleID)
	}
	if fileScope.Scope != "file" {
		t.Fatalf("expected file scope, got %q", fileScope.Scope)
	}
}

// TestParser_ASTExtractionFailureAndContractMapping verifies deterministic parser
// output mapping for AST extraction-style failures and contract-breaking payloads.

func TestParser_ParsePrefersTopLevelDiagramType(t *testing.T) {
	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "top-level-diagram-type")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)
	diagram, syntaxErr, err := p.Parse("sequenceDiagram\n  Alice->>Bob: hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}
	if diagram.Type != model.DiagramTypeSequence {
		t.Fatalf("expected diagram type %q, got %q", model.DiagramTypeSequence, diagram.Type)
	}
}

func TestParser_ASTExtractionFailureAndContractMapping(t *testing.T) {
	tests := []struct {
		name             string
		mode             string
		rawResponse      string
		wantErrSubstr    string
		wantSyntaxSubstr string
	}{
		{
			name:             "ast extraction style syntax failure",
			mode:             "ast-extraction-error",
			wantSyntaxSubstr: "AST extraction failed",
		},
		{
			name:          "malformed json payload",
			rawResponse:   `{"valid":false`,
			wantErrSubstr: "failed to decode parser output",
		},
		{
			name:          "valid false without error object",
			mode:          "valid-false-without-error",
			wantErrSubstr: "invalid result missing error for valid=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := repoTempDir(t)
			if tt.mode != "" {
				t.Setenv("MERM8_TEST_PARSER_MODE", tt.mode)
			}
			if tt.rawResponse != "" {
				t.Setenv("MERM8_TEST_PARSER_RAW_RESPONSE", tt.rawResponse)
			}
			script := writeMockParserFixture(t, tempDir, "parse.ts")

			p := mustNewParser(t, script)
			diagram, syntaxErr, err := p.Parse("graph TD; A-->B")

			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErrSubstr, err.Error())
				}
				if tt.wantErrSubstr == "failed to decode parser output" && !errors.Is(err, parser.ErrDecode) {
					t.Fatalf("expected decode category error, got %v", err)
				}
				if syntaxErr != nil {
					t.Fatalf("expected nil syntaxErr when err is returned, got %+v", syntaxErr)
				}
				if diagram != nil {
					t.Fatalf("expected nil diagram when err is returned, got %+v", diagram)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diagram != nil {
				t.Fatalf("expected nil diagram, got %+v", diagram)
			}

			if syntaxErr == nil {
				t.Fatal("expected syntaxErr, got nil")
			}
			if tt.wantSyntaxSubstr != "" && !strings.Contains(syntaxErr.Message, tt.wantSyntaxSubstr) {
				t.Fatalf("expected syntaxErr containing %q, got %q", tt.wantSyntaxSubstr, syntaxErr.Message)
			}
		})
	}
}

func TestParser_WorkerPoolValidFalseWithoutErrorReturnsContractError(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-valid-false-without-error")

	tempDir := repoTempDir(t)
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err == nil {
		t.Fatal("expected contract error, got nil")
	}
	if !errors.Is(err, parser.ErrContract) {
		t.Fatalf("expected contract category error, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid result missing error for valid=false") {
		t.Fatalf("expected missing-error contract message, got %q", err.Error())
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr for malformed worker envelope, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
}

// TestParser_LargeGraph tests parsing a reasonably large diagram.
func TestParser_LargeGraph(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	// Build a diagram with 20 nodes and many edges
	mermaidCode := "graph TD\n"
	for i := 0; i < 20; i++ {
		if i > 0 {
			mermaidCode += fmt.Sprintf("  A%d --> A%d\n", i-1, i)
		}
	}

	start := time.Now()
	diagram, syntaxErr, err := p.Parse(mermaidCode)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	if len(diagram.Nodes) != 20 {
		t.Errorf("expected 20 nodes, got %d", len(diagram.Nodes))
	}
	if len(diagram.Edges) != 19 {
		t.Errorf("expected 19 edges for linear chain, got %d", len(diagram.Edges))
	}
	t.Logf("parsed %d nodes, %d edges in %v", len(diagram.Nodes), len(diagram.Edges), elapsed)
}

func TestParser_ValidWithoutASTReturnsInternalError(t *testing.T) {
	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "valid-without-ast")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)
	diagram, syntaxErr, err := p.Parse("graph TD; A-->B")

	if err == nil {
		t.Fatal("expected internal error, got nil")
	}
	if syntaxErr != nil {
		t.Fatalf("expected nil syntaxErr, got %+v", syntaxErr)
	}
	if diagram != nil {
		t.Fatalf("expected nil diagram, got %+v", diagram)
	}
	if !errors.Is(err, parser.ErrContract) {
		t.Fatalf("expected contract category error, got %v", err)
	}
}

func TestParser_SubprocessInternalError(t *testing.T) {
	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "subprocess-internal-error")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)
	diagram, syntaxErr, err := p.Parse("graph TD; A-->B")

	if err == nil {
		t.Fatal("expected parser subprocess error, got nil")
	}
	if syntaxErr != nil {
		t.Fatalf("expected nil syntaxErr, got %+v", syntaxErr)
	}
	if diagram != nil {
		t.Fatalf("expected nil diagram, got %+v", diagram)
	}
	if !errors.Is(err, parser.ErrSubprocess) {
		t.Fatalf("expected subprocess category error, got %v", err)
	}
}

// @spec: PARSER-003: Subprocess timeouts return the parser timeout category within configured bounds.
func TestParser_TimeoutCategory(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")
	minTimeout, _, _, _ := parser.LimitBounds()
	timeout := minTimeout
	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "timeout")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	diagram, syntaxErr, err := p.Parse("graph TD; A-->B")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr on timeout, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
	if !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout category error, got %v", err)
	}
}

// @spec: PARSER-004: A pooled worker returns the correct result for each sequential parse request.
func TestParser_WorkerPoolHandlesSequentialRequests(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-sequential-requests")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 10 * time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct pooled parser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	for _, code := range []string{"graph TD; A-->B", "graph LR; C-->D"} {
		diagram, syntaxErr, err := p.Parse(code)
		if err != nil || syntaxErr != nil || diagram == nil {
			t.Fatalf("parse %q failed: diagram=%v syntaxErr=%v err=%v", code, diagram, syntaxErr, err)
		}
		if len(diagram.Nodes) != 1 || diagram.Nodes[0].Label != code {
			t.Errorf("parse %q returned nodes %#v; want one node echoing its request", code, diagram.Nodes)
		}
	}
}

// @spec: PARSER-003: A timed-out worker is replaced before the next parse request.
func TestParser_WorkerPoolTimeoutReplacesWorker(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")
	minTimeout, _, _, _ := parser.LimitBounds()
	timeout := minTimeout
	slowDelay := timeout + 300*time.Millisecond

	tempDir := repoTempDir(t)
	counterFile := filepath.Join(tempDir, "counter.txt")
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-replacement")
	t.Setenv("MERM8_TEST_PARSER_START_COUNTER", counterFile)
	t.Setenv("MERM8_TEST_PARSER_SLOW_DELAY_MS", strconv.FormatInt(slowDelay.Milliseconds(), 10))
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if _, _, err := p.Parse("graph TD\nSLOW"); err == nil || !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout from slow request, got %v", err)
	}

	diagram, syntaxErr, err := p.Parse("graph TD\nFAST")
	if err != nil {
		t.Fatalf("expected second parse success, got %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error on second parse: %+v", syntaxErr)
	}
	if diagram == nil || len(diagram.Nodes) == 0 {
		t.Fatalf("expected diagram with node metadata, got %#v", diagram)
	}
	if diagram.Nodes[0].Label != "2" {
		t.Fatalf("expected replacement worker start count 2, got %q", diagram.Nodes[0].Label)
	}
}

// @spec: PARSER-003: A worker timeout envelope maps to the parser timeout error category.
func TestParser_WorkerPoolWorkerTimeoutEnvelopeMapsToErrTimeout(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-timeout-envelope")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err == nil || !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout category from worker envelope, got %v", err)
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr on timeout, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
}

// @spec: PARSER-003: A worker response without a newline cannot exceed the parser timeout bound.
func TestParser_WorkerPoolTimeoutReturnsPromptlyWhenWorkerNeverWritesNewline(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")
	minTimeout, _, _, _ := parser.LimitBounds()

	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-no-newline")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	timeout := minTimeout
	p, err := parser.NewWithConfig(script, parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	start := time.Now()
	diagram, syntaxErr, err := p.ParseWithConfig("graph TD\nNO_NEWLINE", parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	elapsed := time.Since(start)
	if err == nil || !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout from worker without newline response, got %v", err)
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr on timeout, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
	if elapsed > timeout+500*time.Millisecond {
		t.Fatalf("expected timeout return near configured bound, elapsed=%s timeout=%s", elapsed, timeout)
	}
}

func repoTempDir(t *testing.T) string {
	t.Helper()

	tempDir, err := os.MkdirTemp(".", "parser-test-")
	if err != nil {
		t.Fatalf("failed to create repo temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	return tempDir
}

// TestParser_ConcurrentParsing tests that the parser handles concurrent requests.
// Run with race detector to ensure thread-safety: go test -race ./internal/parser
// The race detector verifies that:
//   - Parser state is not modified concurrently
//   - Subprocess communication is properly synchronized
//   - No data races in AST extraction or error handling
//
// @spec: PARSER-005: Concurrent parse responses retain the diagram from each request.
func TestParser_ConcurrentParsing(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	const numGoroutines = 3
	type parseResult struct {
		inputID int
		diagram *model.Diagram
		err     error
	}
	done := make(chan parseResult, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			code := fmt.Sprintf(`graph TD
  A%d --> B%d`, n, n)

			diagram, syntaxErr, err := p.Parse(code)
			if err != nil {
				done <- parseResult{inputID: n, err: err}
				return
			}
			if syntaxErr != nil {
				done <- parseResult{inputID: n, err: fmt.Errorf("syntax error: %s", syntaxErr.Message)}
				return
			}
			done <- parseResult{inputID: n, diagram: diagram}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		result := <-done
		if result.err != nil {
			t.Errorf("goroutine %d: %v", result.inputID, result.err)
			continue
		}
		if result.diagram == nil {
			t.Errorf("goroutine %d returned a nil diagram", result.inputID)
			continue
		}
		wantFrom := fmt.Sprintf("A%d", result.inputID)
		wantTo := fmt.Sprintf("B%d", result.inputID)
		if len(result.diagram.Nodes) < 2 {
			t.Errorf("goroutine %d returned %d nodes, want at least 2", result.inputID, len(result.diagram.Nodes))
			continue
		}
		gotNodes := map[string]bool{}
		for _, node := range result.diagram.Nodes {
			gotNodes[node.ID] = true
		}
		if !gotNodes[wantFrom] || !gotNodes[wantTo] {
			t.Errorf("goroutine %d received nodes %v, want %s and %s", result.inputID, gotNodes, wantFrom, wantTo)
		}
	}
	t.Logf("validated results for %d concurrent parse requests", numGoroutines)
}

func TestParser_PathRejectionMatrix(t *testing.T) {
	type operation struct {
		name       string
		invoke     func(*parser.Parser) (*model.Diagram, *parser.SyntaxError, error)
		requireNil bool
	}
	type pathType struct {
		name              string
		buildScriptPath   func(*testing.T) string
		expectedErr       error
		expectedSubstring string
	}

	operations := []operation{
		{
			name: "Ready",
			invoke: func(p *parser.Parser) (*model.Diagram, *parser.SyntaxError, error) {
				return nil, nil, p.Ready()
			},
		},
		{
			name: "Parse",
			invoke: func(p *parser.Parser) (*model.Diagram, *parser.SyntaxError, error) {
				return p.Parse("graph TD; A-->B")
			},
			requireNil: true,
		},
	}

	pathTypes := []pathType{
		{
			name: "outside path",
			buildScriptPath: func(t *testing.T) string {
				t.Helper()

				tempDir := t.TempDir()
				script := filepath.Join(tempDir, "parse.ts")
				if err := os.WriteFile(script, nil, 0o600); err != nil {
					t.Fatalf("failed to write parser script: %v", err)
				}

				return script
			},
			expectedSubstring: "outside allowed repository root",
		},
		{
			name: "symlink outside",
			buildScriptPath: func(t *testing.T) string {
				t.Helper()

				tempDir := t.TempDir()
				target := filepath.Join(tempDir, "parse.ts")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatalf("failed to write parser script: %v", err)
				}

				linkDir, err := os.MkdirTemp(".", "parser-link-test-")
				if err != nil {
					t.Fatalf("failed to create local symlink dir: %v", err)
				}
				t.Cleanup(func() {
					_ = os.RemoveAll(linkDir)
				})

				linkPath := filepath.Join(linkDir, "parse.ts")
				if err := os.Symlink(target, linkPath); err != nil {
					t.Fatalf("failed to create symlink: %v", err)
				}

				return linkPath
			},
			expectedSubstring: "outside allowed repository root",
		},
		{
			name: "traversal",
			buildScriptPath: func(_ *testing.T) string {
				return "parser-node/../secrets/parse.ts"
			},
			expectedErr:       os.ErrNotExist,
			expectedSubstring: "resolve symlinks",
		},
	}

	for _, op := range operations {
		for _, path := range pathTypes {
			t.Run(op.name+"/"+path.name, func(t *testing.T) {
				scriptPath := path.buildScriptPath(t)
				p := mustNewParser(t, scriptPath)

				diagram, syntaxErr, err := op.invoke(p)
				if err == nil {
					t.Fatalf("expected validation error for %s with %s, got nil", op.name, path.name)
				}
				if path.expectedErr != nil && !errors.Is(err, path.expectedErr) {
					t.Fatalf("expected %v category error, got %v", path.expectedErr, err)
				}
				if errors.Is(err, parser.ErrTimeout) ||
					errors.Is(err, parser.ErrSubprocess) ||
					errors.Is(err, parser.ErrDecode) ||
					errors.Is(err, parser.ErrContract) ||
					errors.Is(err, parser.ErrMemoryLimit) {
					t.Fatalf("expected path-validation error category, got parser runtime category: %v", err)
				}
				if !strings.Contains(err.Error(), path.expectedSubstring) {
					t.Fatalf("expected error containing %q, got %v", path.expectedSubstring, err)
				}

				if op.requireNil && (diagram != nil || syntaxErr != nil) {
					t.Fatalf("expected nil diagram and syntaxErr, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
				}
			})
		}
	}
}

func TestParser_ReadyAcceptsPathInsideWorkingDirectory(t *testing.T) {
	repoTempDir, err := os.MkdirTemp(".", "parser-ready-test-")
	if err != nil {
		t.Fatalf("failed to create repo temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(repoTempDir)
	})

	script := writeMockParserFixture(t, repoTempDir, "parse.ts")

	p := mustNewParser(t, script)
	err = p.Ready()
	if err != nil {
		t.Fatalf("expected ready check to pass for local script path, got %v", err)
	}
}

// @spec: PARSER-006: Parser operations remain rooted correctly after the caller changes directories.
func TestParser_RepoRootCachedAcrossWorkingDirectoryChange(t *testing.T) {
	const (
		childEnv  = "MERM8_TEST_ROOT_CACHE_CHILD"
		targetEnv = "MERM8_TEST_ROOT_CACHE_TARGET"
	)
	if os.Getenv(childEnv) == "1" {
		resolveCalls := 0
		p, err := parser.NewWithConfigAndRepoRootResolver(os.Getenv("PARSER_SCRIPT"), parser.ConfigFromEnv(), func() (string, error) {
			resolveCalls++
			wd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			for {
				if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
					return wd, nil
				}
				parent := filepath.Dir(wd)
				if parent == wd {
					break
				}
				wd = parent
			}
			return "", fmt.Errorf("failed to locate repository root")
		})
		if err != nil {
			t.Fatalf("construct parser before changing directories: %v", err)
		}
		if resolveCalls != 1 {
			t.Fatalf("repo root resolver called %d times during construction, want 1", resolveCalls)
		}
		if err := os.Chdir(os.Getenv(targetEnv)); err != nil {
			t.Fatalf("change to temporary working directory: %v", err)
		}
		if err := p.Ready(); err != nil {
			t.Fatalf("Ready failed after changing directories: %v", err)
		}
		diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
		if err != nil {
			t.Fatalf("Parse failed after changing directories: %v", err)
		}
		if syntaxErr != nil {
			t.Fatalf("unexpected syntax error after changing directories: %+v", syntaxErr)
		}
		if diagram == nil {
			t.Fatal("expected diagram after changing directories")
		}
		if resolveCalls != 1 {
			t.Fatalf("repo root resolver called %d times after parser operations, want 1", resolveCalls)
		}
		return
	}

	script, err := filepath.Abs(getParserScript(t))
	if err != nil {
		t.Fatalf("resolve parser script path: %v", err)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get current working directory: %v", err)
	}
	targetDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestParser_RepoRootCachedAcrossWorkingDirectoryChange$")
	cmd.Dir = workingDir
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, childEnv+"=") &&
			!strings.HasPrefix(entry, targetEnv+"=") &&
			!strings.HasPrefix(entry, "PARSER_MODE=") &&
			!strings.HasPrefix(entry, "PARSER_SCRIPT=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		childEnv+"=1",
		targetEnv+"="+targetDir,
		"PARSER_MODE=subprocess",
		"PARSER_SCRIPT="+script,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("repo-root cache child process failed: %v\n%s", err, output)
	}
}

func TestParser_NewFailsWhenRepoRootMissing(t *testing.T) {
	const repoRootMissingChildEnv = "MERM8_TEST_REPO_ROOT_MISSING_CHILD"
	if os.Getenv(repoRootMissingChildEnv) == "1" {
		p, err := parser.New("parser-node/parse.ts")
		if err == nil {
			t.Fatal("expected New to fail when repository root cannot be located")
		}
		if p != nil {
			t.Fatal("expected nil parser when New fails")
		}
		if !strings.Contains(err.Error(), "failed to locate repository root") {
			t.Fatalf("expected repository-root discovery error, got %v", err)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestParser_NewFailsWhenRepoRootMissing$")
	cmd.Dir = t.TempDir()
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, repoRootMissingChildEnv+"=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, repoRootMissingChildEnv+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated repo-root test process failed: %v\n%s", err, output)
	}
}

func TestParser_VersionInfo(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")
	t.Setenv("VERSION_COUNTER", filepath.Join(t.TempDir(), "versions.log"))
	script, root := writeVersionCacheTestScript(t, false)
	p, err := parser.NewWithConfigAndRepoRootResolver(script, parser.Config{Timeout: 10 * time.Second}, func() (string, error) {
		return root, nil
	})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	info, err := p.VersionInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil version info")
	}
	if info.ParserVersion != "bridge-v2" {
		t.Fatalf("parser version = %q, want bridge-v2", info.ParserVersion)
	}
	if info.MermaidVersion != "12.0.0" {
		t.Fatalf("Mermaid version = %q, want 12.0.0", info.MermaidVersion)
	}

	second, err := p.VersionInfo()
	if err != nil {
		t.Fatalf("second version lookup failed: %v", err)
	}
	if *second != *info {
		t.Fatalf("cached version info = %#v, want %#v", second, info)
	}
	assertFileLineCount(t, os.Getenv("VERSION_COUNTER"), 1)
}

func TestParser_VersionInfoRejectsMalformedOrIncompleteOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr error
	}{
		{name: "malformed JSON", output: "{not-json", wantErr: parser.ErrDecode},
		{name: "missing Mermaid version", output: `{"parser_version":"bridge-v2"}`, wantErr: parser.ErrContract},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script, root := writeVersionInfoOutputScript(t, tt.output)
			p, err := parser.NewWithConfigAndRepoRootResolver(script, parser.Config{Timeout: 5 * time.Second}, func() (string, error) {
				return root, nil
			})
			if err != nil {
				t.Fatalf("failed to construct parser: %v", err)
			}

			info, err := p.VersionInfo()
			if info != nil {
				t.Fatalf("expected no version info for invalid output, got %#v", info)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("VersionInfo error = %v, want category %v", err, tt.wantErr)
			}
		})
	}
}

func writeVersionInfoOutputScript(t *testing.T, output string) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module versioninfotest\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatalf("write temporary go.mod: %v", err)
	}
	t.Setenv("MERM8_TEST_VERSION_OUTPUT", output)
	script := writeMockParserFixture(t, root, "parse.ts")
	return script, root
}

func TestParser_ConcurrentFirstParsesResolveVersionOnce(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")
	t.Setenv("VERSION_COUNTER", filepath.Join(t.TempDir(), "versions.log"))
	t.Setenv("PARSE_COUNTER", filepath.Join(t.TempDir(), "parses.log"))
	script, root := writeVersionCacheTestScript(t, false)
	p, err := parser.NewWithConfigAndRepoRootResolver(script, parser.Config{Timeout: 10 * time.Second}, func() (string, error) { return root, nil })
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	const count = 8
	var start sync.WaitGroup
	start.Add(1)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Wait()
			if diagram, syntaxErr, parseErr := p.Parse("graph TD\nA-->B"); parseErr != nil || syntaxErr != nil || diagram == nil {
				t.Errorf("unexpected parse result: diagram=%v syntax=%v err=%v", diagram, syntaxErr, parseErr)
			}
		}()
	}
	start.Done()
	wg.Wait()

	assertFileLineCount(t, os.Getenv("VERSION_COUNTER"), 1)
	assertFileLineCount(t, os.Getenv("PARSE_COUNTER"), 1)
}

func TestParser_FailedVersionLookupCannotAdmitCacheEntry(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")
	t.Setenv("VERSION_COUNTER", filepath.Join(t.TempDir(), "versions.log"))
	t.Setenv("PARSE_COUNTER", filepath.Join(t.TempDir(), "parses.log"))
	t.Setenv("VERSION_FAILURE_MARKER", filepath.Join(t.TempDir(), "failed.marker"))
	script, root := writeVersionCacheTestScript(t, true)
	p, err := parser.NewWithConfigAndRepoRootResolver(script, parser.Config{Timeout: 10 * time.Second}, func() (string, error) { return root, nil })
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	code := "graph TD\nA-->B"
	for i := 0; i < 3; i++ {
		if diagram, syntaxErr, parseErr := p.Parse(code); parseErr != nil || syntaxErr != nil || diagram == nil {
			t.Fatalf("parse %d failed: diagram=%v syntax=%v err=%v", i+1, diagram, syntaxErr, parseErr)
		}
	}

	// The failed lookup makes the first parse uncacheable. The second lookup
	// succeeds and admits its result, so the third parse is a cache hit.
	assertFileLineCount(t, os.Getenv("VERSION_COUNTER"), 2)
	assertFileLineCount(t, os.Getenv("PARSE_COUNTER"), 2)
}

func writeVersionCacheTestScript(t *testing.T, failFirstVersion bool) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module versioncachetest\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MERM8_TEST_PARSER_MODE", "version-cache")
	if failFirstVersion {
		t.Setenv("MERM8_TEST_FAIL_FIRST_VERSION", "1")
	} else {
		t.Setenv("MERM8_TEST_FAIL_FIRST_VERSION", "0")
	}
	script := writeMockParserFixture(t, root, "parse.ts")
	return script, root
}

func assertFileLineCount(t *testing.T, path string, want int) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read counter %s: %v", path, err)
	}
	if got := strings.Count(string(contents), "\n"); got != want {
		t.Fatalf("counter %s has %d lines, want %d", path, got, want)
	}
}

func TestParser_MemoryLimitCategory(t *testing.T) {
	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "memory-limit")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)
	diagram, syntaxErr, err := p.Parse("graph TD; A-->B")
	if err == nil {
		t.Fatal("expected memory-limit error, got nil")
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
	if !errors.Is(err, parser.ErrMemoryLimit) {
		t.Fatalf("expected memory-limit category error, got %v", err)
	}
	meta, ok := parser.MetadataFromError(err)
	if !ok {
		t.Fatalf("expected parser metadata for memory-limit error")
	}
	if meta.Limit == "" || meta.ObservedSizeByte == 0 {
		t.Fatalf("expected metadata limit and observed size, got %+v", meta)
	}
}

func TestParser_ClosePreventsFutureBorrows(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "worker-close")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)
	if err := p.Close(); err != nil {
		t.Fatalf("close parser: %v", err)
	}

	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err == nil {
		t.Fatal("expected parse error after parser close")
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil outputs after parser close, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
	if !errors.Is(err, parser.ErrSubprocess) {
		t.Fatalf("expected ErrSubprocess after parser close, got %v", err)
	}
}

func TestParser_SubprocessCloseRacingWithParseReturnsClosedError(t *testing.T) {
	t.Setenv("PARSER_MODE", "subprocess")

	tempDir := repoTempDir(t)
	t.Setenv("MERM8_TEST_PARSER_MODE", "delayed-success")
	script := writeMockParserFixture(t, tempDir, "parse.ts")

	p := mustNewParser(t, script)

	const goroutines = 24
	start := make(chan struct{})
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := p.ParseWithConfig("graph TD\nA-->B", parser.Config{Timeout: time.Second, NodeMaxOldSpaceMB: 256})
			errCh <- err
		}()
	}

	closeDone := make(chan struct{})
	go func() {
		<-start
		_ = p.Close()
		close(closeDone)
	}()

	close(start)
	wg.Wait()
	<-closeDone
	close(errCh)

	var sawClosed bool
	for err := range errCh {
		if err != nil && errors.Is(err, parser.ErrSubprocess) && strings.Contains(err.Error(), "parser is closed") {
			sawClosed = true
			break
		}
	}
	if !sawClosed {
		t.Fatal("expected at least one parse to fail with closed-parser error when racing Close with ParseWithConfig")
	}
}
