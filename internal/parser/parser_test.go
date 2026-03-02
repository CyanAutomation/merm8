// Package parser_test tests the Node.js subprocess integration.
package parser_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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
			script := filepath.Join(cwd, "parser-node", "parse.mjs")
			return script
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break // reached root
		}
		cwd = parent
	}

	t.Fatalf("could not locate parser-node/parse.mjs. Set PARSER_SCRIPT env var")
	return ""
}

// TestParser_ValidFlowchart tests parsing a valid flowchart.
func TestParser_ValidFlowchart(t *testing.T) {
	script := getParserScript(t)

	p := parser.New(script)

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
			t.Errorf("expected edge B -> C, got %s -> %s", diagram.Edges[1].From, diagram.Edges[1].To)
		}
	}
}

// TestParser_InvalidMermaid tests parsing invalid Mermaid code.
func TestParser_InvalidMermaid(t *testing.T) {
	script := getParserScript(t)

	p := parser.New(script)

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

	p := parser.New(script)

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

// TestParser_WithDirection tests parsing diagrams with different directions.
func TestParser_WithDirection(t *testing.T) {
	script := getParserScript(t)
	p := parser.New(script)

	tests := []struct {
		name      string
		code      string
		direction string
	}{
		{"Top-Down", "graph TD\n  A-->B", "TD"},
		{"Left-Right", "graph LR\n  A-->B", "LR"},
		// Note: Different direction formats may vary based on Mermaid version
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
		})
	}
}

// TestParser_MultipleEdges tests parsing diagrams with multiple edges from one node.
func TestParser_MultipleEdges(t *testing.T) {
	script := getParserScript(t)
	p := parser.New(script)

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
	if aOutgoing != 2 {
		t.Errorf("expected 2 edges from A, got %d", aOutgoing)
	}
}

// TestParser_ASTExtractionFailure verifies parser-runtime AST extraction failures
// are returned as syntax errors instead of silently succeeding with empty metrics.
func TestParser_ASTExtractionFailure(t *testing.T) {
	script := getParserScript(t)
	p := parser.New(script)

	t.Setenv("MERM8_FORCE_AST_DB_NULL", "1")

	mermaidCode := `graph TD
    A[Start]
    B[End]
    A --> B`

	diagram, syntaxErr, err := p.Parse(mermaidCode)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syntaxErr == nil {
		t.Fatal("expected syntax error when AST extraction fails, got nil")
	}
	if diagram != nil {
		t.Fatal("expected nil diagram on AST extraction failure")
	}
	if !contains(syntaxErr.Message, "AST extraction failed") {
		t.Fatalf("expected message mentioning AST extraction failure, got %q", syntaxErr.Message)
	}
}

// TestParser_LargeGraph tests parsing a reasonably large diagram.
func TestParser_LargeGraph(t *testing.T) {
	script := getParserScript(t)
	p := parser.New(script)

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

func TestParser_SubprocessInternalError(t *testing.T) {
	tempDir := t.TempDir()
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

	p := parser.New(script)
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

// TestParser_ConcurrentParsing tests that the parser handles concurrent requests.
func TestParser_ConcurrentParsing(t *testing.T) {
	script := getParserScript(t)
	p := parser.New(script)

	// Test concurrent parsing
	numGoroutines := 5
	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			code := fmt.Sprintf(`graph TD
    A%d[Node A]
    B%d[Node B]
    A%d --> B%d`, n, n, n, n)

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
