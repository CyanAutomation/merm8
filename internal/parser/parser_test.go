// Package parser_test tests the Node.js subprocess integration.
package parser_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
process.stdout.write(JSON.stringify({
  valid: true,
  diagram_type: "sequence",
  ast: {
    type: "unknown",
    direction: "TD",
    nodes: [],
    edges: [],
    subgraphs: [],
    suppressions: []
  }
}) + "\n");
process.exit(0);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

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
		scriptBody       string
		wantErrSubstr    string
		wantSyntaxSubstr string
	}{
		{
			name: "ast extraction style syntax failure",
			scriptBody: `#!/usr/bin/env node
process.stdout.write(JSON.stringify({
  valid: false,
  error: { message: "AST extraction failed in parser runtime: synthetic fixture", line: 0, column: 0 }
}) + "\n");
process.exit(0);
`,
			wantSyntaxSubstr: "AST extraction failed",
		},
		{
			name: "malformed json payload",
			scriptBody: `#!/usr/bin/env node
process.stdout.write("{\"valid\":false");
process.exit(0);
`,
			wantErrSubstr: "failed to decode parser output",
		},
		{
			name: "valid false without error object",
			scriptBody: `#!/usr/bin/env node
process.stdout.write(JSON.stringify({ valid: false }) + "\n");
process.exit(0);
`,
			wantErrSubstr: "invalid result missing error for valid=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := repoTempDir(t)
			script := filepath.Join(tempDir, "parse.mjs")
			if err := os.WriteFile(script, []byte(tt.scriptBody), 0o700); err != nil {
				t.Fatalf("failed to write test parser script: %v", err)
			}

			p := mustNewParser(t, script)
			diagram, syntaxErr, err := p.Parse("graph TD; A-->B")

			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
				}
				if !contains(err.Error(), tt.wantErrSubstr) {
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
			if tt.wantSyntaxSubstr != "" && !contains(syntaxErr.Message, tt.wantSyntaxSubstr) {
				t.Fatalf("expected syntaxErr containing %q, got %q", tt.wantSyntaxSubstr, syntaxErr.Message)
			}
		})
	}
}

func TestParser_WorkerPoolValidFalseWithoutErrorReturnsContractError(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"oneshot"}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

import readline from "readline";
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  process.stdout.write(JSON.stringify({
    id: req.id,
    result: { valid: false }
  }) + "\n");
}
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

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
	if !contains(err.Error(), "invalid result missing error for valid=false") {
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
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
process.stdout.write(JSON.stringify({ valid: true }) + "\n");
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

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
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
process.stdout.write(JSON.stringify({
  valid: false,
  error: { message: "internal parser error: exploded", line: 0, column: 0 }
}) + "\n");
process.exit(1);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

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

func TestParser_TimeoutCategory(t *testing.T) {
	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {}, 10000);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

	p := mustNewParser(t, script)
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

func TestParser_WorkerPoolReusesWorkerAcrossRequests(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"oneshot"}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

import readline from "readline";
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  process.stdout.write(JSON.stringify({
    id: req.id,
    result: {
      valid: true,
      diagram_type: "flowchart",
      ast: {
        type: "flowchart",
        direction: "TD",
        nodes: [{ id: "n", label: String(process.pid) }],
        edges: [],
        subgraphs: [],
        suppressions: []
      }
    }
  }) + "\n");
}
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

	p := mustNewParser(t, script)
	first, syntaxErr, err := p.Parse("graph TD; A-->B")
	if err != nil || syntaxErr != nil || first == nil || len(first.Nodes) == 0 {
		t.Fatalf("first parse failed: diagram=%v syntaxErr=%v err=%v", first, syntaxErr, err)
	}
	second, syntaxErr, err := p.Parse("graph TD; B-->C")
	if err != nil || syntaxErr != nil || second == nil || len(second.Nodes) == 0 {
		t.Fatalf("second parse failed: diagram=%v syntaxErr=%v err=%v", second, syntaxErr, err)
	}
	if first.Nodes[0].Label != second.Nodes[0].Label {
		t.Fatalf("expected worker reuse with same pid label, got %q and %q", first.Nodes[0].Label, second.Nodes[0].Label)
	}
}

func TestParser_WorkerPoolTimeoutReplacesWorker(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	counterFile := filepath.Join(tempDir, "counter.txt")
	scriptBody := fmt.Sprintf(`#!/usr/bin/env node
import fs from "fs";
import readline from "readline";
const counterFile = %q;

if (process.argv.includes("--version-info")) {
  process.stdout.write(JSON.stringify({parser_version:"test-1.0.0",mermaid_version:"test-1.0.0"})+"\n");
  process.exit(0);
}

let startCount = 0;
try {
  startCount = parseInt(fs.readFileSync(counterFile, "utf8"), 10) || 0;
} catch (_) {}
startCount += 1;
fs.writeFileSync(counterFile, String(startCount));

if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:String(startCount)}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  if (String(req.code || "").includes("SLOW")) {
    await new Promise((resolve) => setTimeout(resolve, 1500));
  }
  process.stdout.write(JSON.stringify({
    id: req.id,
    result: {
      valid: true,
      diagram_type: "flowchart",
      ast: {
        type: "flowchart",
        direction: "TD",
        nodes: [{ id: "n", label: String(startCount) }],
        edges: [],
        subgraphs: [],
        suppressions: []
      }
    }
  }) + "\n");
}
`, counterFile)
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}
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

func TestParser_WorkerPoolWorkerTimeoutEnvelopeMapsToErrTimeout(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"oneshot"}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

import readline from "readline";
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  process.stdout.write(JSON.stringify({
    id: req.id,
    error: "parser_timeout: exceeded " + String(req.timeout_ms || 0) + "ms"
  }) + "\n");
}
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err == nil || !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout category from worker envelope, got %v", err)
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr on timeout, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
}

func TestParser_WorkerPoolTimeoutReturnsPromptlyWhenWorkerNeverWritesNewline(t *testing.T) {
	t.Setenv("PARSER_MODE", "pool")
	t.Setenv("PARSER_WORKER_POOL_SIZE", "1")

	tempDir := repoTempDir(t)
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"oneshot"}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

import readline from "readline";
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  if (String(req.code || "").includes("NO_NEWLINE")) {
    process.stdout.write("{\"id\":\"" + req.id + "\",\"result\":{\"valid\":true}");
    setInterval(() => {}, 1000);
    continue;
  }
  process.stdout.write(JSON.stringify({
    id: req.id,
    result: {
      valid: true,
      diagram_type: "flowchart",
      ast: { type: "flowchart", direction: "TD", nodes: [{ id: "ok", label: "ok" }], edges: [], subgraphs: [], suppressions: [] }
    }
  }) + "\n");
}
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

	timeout := time.Second
	p, err := parser.NewWithConfig(script, parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to construct parser: %v", err)
	}

	start := time.Now()
	diagram, syntaxErr, err := p.ParseWithConfig("graph TD\nNO_NEWLINE", parser.Config{Timeout: timeout, NodeMaxOldSpaceMB: 256})
	elapsed := time.Since(start)
	if err == nil || !errors.Is(err, parser.ErrTimeout) {
		t.Fatalf("expected timeout from worker without newline response, got %v", err)
	}
	if diagram != nil || syntaxErr != nil {
		t.Fatalf("expected nil diagram/syntaxErr on timeout, got diagram=%v syntaxErr=%v", diagram, syntaxErr)
	}
	if elapsed > timeout+700*time.Millisecond {
		t.Fatalf("expected timeout return near configured bound, elapsed=%s timeout=%s", elapsed, timeout)
	}
}

// Helper to check if string contains substring (Go 1.24 doesn't have strings.Contains in all contexts)
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
func TestParser_ConcurrentParsing(t *testing.T) {
	script := getParserScript(t)
	p := mustNewParser(t, script)

	// Test concurrent parsing
	numGoroutines := 3
	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			code := fmt.Sprintf(`graph TD
  A%d --> B%d`, n, n)

			diagram, syntaxErr, err := p.Parse(code)
			if err != nil {
				done <- err
				return
			}
			if syntaxErr != nil {
				done <- fmt.Errorf("syntax error: %s", syntaxErr.Message)
				return
			}
			if diagram == nil {
				done <- fmt.Errorf("nil diagram")
				return
			}
			done <- nil
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		if err := <-done; err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	t.Logf("all %d goroutines completed successfully", numGoroutines)
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
				script := filepath.Join(tempDir, "parse.mjs")
				if err := os.WriteFile(script, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
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
				target := filepath.Join(tempDir, "parse.mjs")
				if err := os.WriteFile(target, []byte("#!/usr/bin/env node\n"), 0o700); err != nil {
					t.Fatalf("failed to write parser script: %v", err)
				}

				linkDir, err := os.MkdirTemp(".", "parser-link-test-")
				if err != nil {
					t.Fatalf("failed to create local symlink dir: %v", err)
				}
				t.Cleanup(func() {
					_ = os.RemoveAll(linkDir)
				})

				linkPath := filepath.Join(linkDir, "parse.mjs")
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
				return "parser-node/../secrets/parse.mjs"
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
				if !contains(err.Error(), path.expectedSubstring) {
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

	script := filepath.Join(repoTempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
process.exit(0);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p := mustNewParser(t, script)
	err = p.Ready()
	if err != nil {
		t.Fatalf("expected ready check to pass for local script path, got %v", err)
	}
}

func TestParser_RepoRootCachedAcrossWorkingDirectoryChange(t *testing.T) {
	script := getParserScript(t)
	resolveCalls := 0

	p, err := parser.NewWithConfigAndRepoRootResolver(script, parser.ConfigFromEnv(), func() (string, error) {
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
		t.Fatalf("failed to construct parser with resolver: %v", err)
	}

	if resolveCalls != 1 {
		t.Fatalf("expected repo root resolver called once during construction, got %d", resolveCalls)
	}

	if err := p.Ready(); err != nil {
		t.Fatalf("expected Ready to use cached repo root, got %v", err)
	}

	diagram, syntaxErr, err := p.Parse("graph TD\nA-->B")
	if err != nil {
		t.Fatalf("expected Parse to use cached repo root, got %v", err)
	}
	if syntaxErr != nil {
		t.Fatalf("unexpected syntax error: %+v", syntaxErr)
	}
	if diagram == nil {
		t.Fatal("expected diagram, got nil")
	}

	if resolveCalls != 1 {
		t.Fatalf("expected repo root resolver to remain cached after parser operations, got %d calls", resolveCalls)
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
	script := filepath.Join(root, "parse.mjs")
	body := fmt.Sprintf("if (process.argv.includes(\"--version-info\")) { process.stdout.write(%s); process.exit(0); }\n", strconv.Quote(output))
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatalf("write parser version fixture: %v", err)
	}
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
	script := filepath.Join(root, "parse.mjs")
	body := fmt.Sprintf(`import fs from "node:fs";
if (process.argv.includes("--version-info")) {
  fs.appendFileSync(process.env.VERSION_COUNTER, "version\n");
  const marker = process.env.VERSION_FAILURE_MARKER;
  if (%t && marker && !fs.existsSync(marker)) {
    fs.writeFileSync(marker, "failed");
    process.stderr.write("version unavailable");
    process.exit(1);
  }
  process.stdout.write(JSON.stringify({parser_version:"bridge-v2",mermaid_version:"12.0.0"}));
  process.exit(0);
}
fs.appendFileSync(process.env.PARSE_COUNTER, "parse\n");
process.stdin.resume();
process.stdin.on("end", () => process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[],edges:[],subgraphs:[],suppressions:[]}})));
`, failFirstVersion)
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	script := filepath.Join(tempDir, "parse-memory.mjs")
	scriptBody := `#!/usr/bin/env node
process.stderr.write("FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\\n");
process.stdout.write(JSON.stringify({valid:false,error:{message:"internal parser error: oom",line:0,column:0}}));
process.exit(1);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write test parser script: %v", err)
	}

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
	script := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
if (!process.argv.includes("--worker")) {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"oneshot"}],edges:[],subgraphs:[],suppressions:[]}})+"\n");
  process.exit(0);
}

import readline from "readline";
const rl = readline.createInterface({ input: process.stdin, crlfDelay: Infinity, terminal: false });
for await (const line of rl) {
  const req = JSON.parse(line);
  process.stdout.write(JSON.stringify({
    id: req.id,
    result: {
      valid: true,
      diagram_type: "flowchart",
      ast: { type: "flowchart", direction: "TD", nodes: [{id: "a", label: "A"}], edges: [], subgraphs: [], suppressions: [] }
    }
  }) + "\n");
}
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

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
	script := filepath.Join(tempDir, "parse-slow.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {
  process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[{id:"n",label:"ok"}],edges:[],subgraphs:[],suppressions:[]}}));
  process.exit(0);
}, 25);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

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
		if err != nil && errors.Is(err, parser.ErrSubprocess) && contains(err.Error(), "parser is closed") {
			sawClosed = true
			break
		}
	}
	if !sawClosed {
		t.Fatal("expected at least one parse to fail with closed-parser error when racing Close with ParseWithConfig")
	}
}
