// Package parser provides a bridge between Go and the Node.js Mermaid parser.
// It spawns a stateless Node subprocess, sends Mermaid source on stdin, and
// reads the structured JSON result from stdout.
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultTimeout = 5 * time.Second
const defaultNodeMaxOldSpaceSizeMB = 512

var (
	// ErrTimeout indicates the parser subprocess exceeded the configured timeout.
	ErrTimeout = errors.New("parser timeout")
	// ErrSubprocess indicates a non-timeout Node subprocess failure.
	ErrSubprocess = errors.New("parser subprocess error")
	// ErrDecode indicates parser output could not be decoded as the expected JSON contract.
	ErrDecode = errors.New("parser decode error")
	// ErrContract indicates parser output violated the expected parse contract.
	ErrContract = errors.New("parser contract violation")
)

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
	Direction    string              `json:"direction"`
	Nodes        []parsedNode        `json:"nodes"`
	Edges        []parsedEdge        `json:"edges"`
	Subgraphs    []parsedSubgraph    `json:"subgraphs"`
	Suppressions []parsedSuppression `json:"suppressions"`
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

type parsedSuppression struct {
	RuleID     string `json:"ruleId"`
	Scope      string `json:"scope"`
	Line       int    `json:"line"`
	TargetLine int    `json:"targetLine"`
}

// Parser wraps the Node subprocess invocation.
type Parser struct {
	scriptPath        string
	timeout           time.Duration
	repoRoot          string
	nodeMaxOldSpaceMB int
}

// New returns a Parser that will invoke the given Node.js script path.
func New(scriptPath string) (*Parser, error) {
	root, err := findRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize parser: %w", err)
	}

	return &Parser{
		scriptPath:        scriptPath,
		timeout:           defaultTimeout,
		repoRoot:          root,
		nodeMaxOldSpaceMB: readMaxOldSpaceMB(),
	}, nil
}

// Ready performs lightweight dependency checks used by readiness probes.
func (p *Parser) Ready() error {
	root, err := p.getRepoRoot()
	if err != nil {
		return err
	}

	scriptPath, err := validateScriptPath(p.scriptPath, root)
	if err != nil {
		return err
	}

	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node runtime not found: %w", err)
	}

	cmd := exec.Command("node", append(p.nodeArgs(), "--check", scriptPath)...) //nolint:gosec
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("parser script check failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	return nil
}

func validateScriptPath(scriptPath, root string) (string, error) {
	if scriptPath == "" {
		return "", fmt.Errorf("parser script path is empty")
	}

	// filepath.Clean and filepath.Rel checks below handle traversal validation

	absPath, err := filepath.Abs(filepath.Clean(scriptPath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve parser script path: %w", err)
	}

	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate parser script path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parser script path is outside allowed repository root")
	}

	// Resolve any symbolic links and validate again to prevent symlink escapes.
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks in parser script path: %w", err)
	}

	rel, err = filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to validate parser script path after symlink resolution: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parser script path is outside allowed repository root")
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

func (p *Parser) getRepoRoot() (string, error) {
	if p.repoRoot == "" {
		return "", fmt.Errorf("failed to locate repository root")
	}

	return p.repoRoot, nil
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory: %w", err)
	}

	for {
		gomod := filepath.Join(cwd, "go.mod")
		if _, err := os.Stat(gomod); err == nil {
			return cwd, nil
		}

		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return "", fmt.Errorf("failed to locate repository root")
}

// Parse sends mermaidCode to the Node parser and returns either a Diagram or a
// SyntaxError. A non-nil error means an unexpected failure (e.g. timeout).
func (p *Parser) Parse(mermaidCode string) (*model.Diagram, *SyntaxError, error) {
	root, err := p.getRepoRoot()
	if err != nil {
		return nil, nil, err
	}

	scriptPath, err := validateScriptPath(p.scriptPath, root)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	// Use both a Node heap cap and a process timeout to reduce memory/CPU abuse.
	cmd := exec.CommandContext(ctx, "node", append(p.nodeArgs(), scriptPath)...) //nolint:gosec
	cmd.Stdin = bytes.NewBufferString(mermaidCode)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, fmt.Errorf("%w: after %s", ErrTimeout, p.timeout)
		}
	}

	var result ParseResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		if runErr != nil {
			return nil, nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, runErr, stderr.String())
		}
		return nil, nil, fmt.Errorf("%w: failed to decode parser output: %w", ErrDecode, err)
	}

	if runErr != nil {
		if result.Error == nil || strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Error.Message)), "internal parser error:") {
			return nil, nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, runErr, stderr.String())
		}
	}

	if !result.Valid {
		return nil, result.Error, nil
	}

	if result.AST == nil {
		return nil, nil, fmt.Errorf("%w: valid result missing AST", ErrContract)
	}

	diagram := toDiagram(result.AST)
	return diagram, nil, nil
}

func (p *Parser) nodeArgs() []string {
	return []string{fmt.Sprintf("--max-old-space-size=%d", p.nodeMaxOldSpaceMB)}
}

func readMaxOldSpaceMB() int {
	raw := strings.TrimSpace(os.Getenv("PARSER_MAX_OLD_SPACE_MB"))
	if raw == "" {
		return defaultNodeMaxOldSpaceSizeMB
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultNodeMaxOldSpaceSizeMB
	}

	// Prevent excessive memory allocation
	const maxAllowedMB = 4096
	if value > maxAllowedMB {
		return defaultNodeMaxOldSpaceSizeMB
	}

	return value
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
	for _, suppression := range ast.Suppressions {
		d.Suppressions = append(d.Suppressions, model.SuppressionDirective{
			RuleID:     suppression.RuleID,
			Scope:      suppression.Scope,
			Line:       suppression.Line,
			TargetLine: suppression.TargetLine,
		})
	}
	return d
}
