// Package parser provides a bridge between Go and the Node.js Mermaid parser.
// It spawns a stateless Node subprocess, sends Mermaid source on stdin, and
// reads the structured JSON result from stdout.
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultTimeout = 2 * time.Second

// SyntaxError describes a parse failure reported by the Node.js parser.
type SyntaxError struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// ParseResult is the raw JSON envelope returned by the Node parser script.
type ParseResult struct {
	Valid bool         `json:"valid"`
	AST   *parsedAST   `json:"ast,omitempty"`
	Error *SyntaxError `json:"error,omitempty"`
}

// parsedAST mirrors the simplified AST returned by parser-node/parse.mjs.
type parsedAST struct {
	Direction string           `json:"direction"`
	Nodes     []parsedNode     `json:"nodes"`
	Edges     []parsedEdge     `json:"edges"`
	Subgraphs []parsedSubgraph `json:"subgraphs"`
}

type parsedNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type parsedEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

type parsedSubgraph struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Nodes []string `json:"nodes"`
}

// Parser wraps the Node subprocess invocation.
type Parser struct {
	scriptPath string
	timeout    time.Duration
}

// New returns a Parser that will invoke the given Node.js script path.
func New(scriptPath string) *Parser {
	return &Parser{scriptPath: scriptPath, timeout: defaultTimeout}
}

// Ready performs lightweight dependency checks used by readiness probes.
func (p *Parser) Ready() error {
	scriptPath, err := validateScriptPath(p.scriptPath)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node runtime not found: %w", err)
	}

	cmd := exec.Command("node", "--check", scriptPath) //nolint:gosec
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("parser script check failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func validateScriptPath(scriptPath string) (string, error) {
	if scriptPath == "" {
		return "", fmt.Errorf("parser script path is empty")
	}

	for _, part := range strings.Split(filepath.ToSlash(scriptPath), "/") {
		if part == ".." {
			return "", fmt.Errorf("parser script path contains traversal segment")
		}
	}

	absPath, err := filepath.Abs(filepath.Clean(scriptPath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve parser script path: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory: %w", err)
	}

	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate parser script path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parser script path is outside allowed working directory")
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("parser script path is not accessible: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("parser script path points to a directory")
	}

	return absPath, nil
}

// Parse sends mermaidCode to the Node parser and returns either a Diagram or a
// SyntaxError. A non-nil error means an unexpected failure (e.g. timeout).
func (p *Parser) Parse(mermaidCode string) (*model.Diagram, *SyntaxError, error) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", p.scriptPath) //nolint:gosec
	cmd.Stdin = bytes.NewBufferString(mermaidCode)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, fmt.Errorf("parser timeout after %s", p.timeout)
		}
	}

	var result ParseResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		if runErr != nil {
			return nil, nil, fmt.Errorf("parser subprocess error: %w (stderr: %s)", runErr, stderr.String())
		}
		return nil, nil, fmt.Errorf("failed to decode parser output: %w", err)
	}

	if runErr != nil {
		if result.Error == nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Error.Message)), "internal parser error:") {
			return nil, nil, fmt.Errorf("parser subprocess error: %w (stderr: %s)", runErr, stderr.String())
		}
	}

	if !result.Valid {
		return nil, result.Error, nil
	}

	if result.AST == nil {
		return nil, nil, fmt.Errorf("parser contract violation: valid result missing AST")
	}

	diagram := toDiagram(result.AST)
	return diagram, nil, nil
}

// toDiagram converts the raw AST into the internal model.
func toDiagram(ast *parsedAST) *model.Diagram {
	if ast == nil {
		return &model.Diagram{}
	}
	d := &model.Diagram{Direction: ast.Direction}

	for _, n := range ast.Nodes {
		d.Nodes = append(d.Nodes, model.Node{ID: n.ID, Label: n.Label})
	}
	for _, e := range ast.Edges {
		d.Edges = append(d.Edges, model.Edge{From: e.From, To: e.To, Type: e.Type})
	}
	for _, s := range ast.Subgraphs {
		d.Subgraphs = append(d.Subgraphs, model.Subgraph{
			ID:    s.ID,
			Label: s.Label,
			Nodes: s.Nodes,
		})
	}
	return d
}
