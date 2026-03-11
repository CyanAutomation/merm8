// Package parser provides a bridge between Go and the Node.js Mermaid parser.
// It spawns a stateless Node subprocess, sends Mermaid source on stdin, and
// reads the structured JSON result from stdout.
package parser

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

const defaultTimeout = 5 * time.Second
const minTimeout = 1 * time.Second
const maxTimeout = 60 * time.Second
const defaultParserSourceEnhancementEnabled = true
const defaultNodeMaxOldSpaceSizeMB = 512
const minNodeMaxOldSpaceSizeMB = 128
const maxNodeMaxOldSpaceSizeMB = 4096
const defaultParserMode = "pool"
const parserModeAuto = "auto"
const parserModePool = "pool"
const parserModeSubprocess = "subprocess"
const defaultWorkerPoolSize = 4
const minWorkerPoolSize = 1
const maxWorkerPoolSize = 64

var (
	// ErrTimeout indicates the parser subprocess exceeded the configured timeout.
	ErrTimeout = errors.New("parser timeout")
	// ErrSubprocess indicates a non-timeout Node subprocess failure.
	ErrSubprocess = errors.New("parser subprocess error")
	// ErrDecode indicates parser output could not be decoded as the expected JSON contract.
	ErrDecode = errors.New("parser decode error")
	// ErrContract indicates parser output violated the expected parse contract.
	ErrContract = errors.New("parser contract violation")
	// ErrMemoryLimit indicates the parser subprocess exceeded its memory limit.
	ErrMemoryLimit = errors.New("parser memory limit exceeded")
)

// Config captures parser execution limits for subprocess invocations.
type Config struct {
	Timeout               time.Duration
	NodeMaxOldSpaceMB     int
	SourceEnhancement     *bool
	NeedSourceEnhancement bool
}

// EffectiveConfig returns validated parser execution limits.
func (c Config) EffectiveConfig() Config {
	effective := Config{
		Timeout:           defaultTimeout,
		NodeMaxOldSpaceMB: defaultNodeMaxOldSpaceSizeMB,
		SourceEnhancement: boolPtr(defaultParserSourceEnhancementEnabled),
	}
	if c.Timeout > 0 {
		effective.Timeout = c.Timeout
		if effective.Timeout < minTimeout {
			effective.Timeout = minTimeout
		}
		if effective.Timeout > maxTimeout {
			effective.Timeout = maxTimeout
		}
	}
	if c.NodeMaxOldSpaceMB > 0 {
		effective.NodeMaxOldSpaceMB = c.NodeMaxOldSpaceMB
		if effective.NodeMaxOldSpaceMB < minNodeMaxOldSpaceSizeMB {
			effective.NodeMaxOldSpaceMB = minNodeMaxOldSpaceSizeMB
		}
		if effective.NodeMaxOldSpaceMB > maxNodeMaxOldSpaceSizeMB {
			effective.NodeMaxOldSpaceMB = maxNodeMaxOldSpaceSizeMB
		}
	}
	if c.SourceEnhancement != nil {
		effective.SourceEnhancement = boolPtr(*c.SourceEnhancement)
	}
	effective.NeedSourceEnhancement = c.NeedSourceEnhancement
	return effective
}

// DefaultConfig returns the service defaults for parser execution limits.
func DefaultConfig() Config {
	return Config{Timeout: defaultTimeout, NodeMaxOldSpaceMB: defaultNodeMaxOldSpaceSizeMB, SourceEnhancement: boolPtr(defaultParserSourceEnhancementEnabled)}
}

// LimitBounds returns hard bounds for timeout and memory parser options.
func LimitBounds() (time.Duration, time.Duration, int, int) {
	return minTimeout, maxTimeout, minNodeMaxOldSpaceSizeMB, maxNodeMaxOldSpaceSizeMB
}

// ConfigFromEnv returns parser config derived from service environment variables.
func ConfigFromEnv() Config {
	return Config{
		Timeout:           readTimeoutSeconds(),
		NodeMaxOldSpaceMB: readMaxOldSpaceMB(),
		SourceEnhancement: readSourceEnhancementEnabled(),
	}
}

// ErrorMetadata provides optional machine-readable parser failure context.
type ErrorMetadata struct {
	Suggestion       string
	Limit            string
	ObservedSizeByte int
}

type parserExecutionError struct {
	err      error
	metadata ErrorMetadata
}

func (e *parserExecutionError) Error() string { return e.err.Error() }
func (e *parserExecutionError) Unwrap() error { return e.err }

// MetadataFromError extracts optional parser execution metadata.
func MetadataFromError(err error) (ErrorMetadata, bool) {
	var parseErr *parserExecutionError
	if !errors.As(err, &parseErr) {
		return ErrorMetadata{}, false
	}
	return parseErr.metadata, true
}

// SyntaxError describes a parse failure reported by the Node.js parser.
type SyntaxError struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// ParseResult is the raw JSON envelope returned by the Node parser script.
type ParseResult struct {
	Valid       bool         `json:"valid"`
	DiagramType string       `json:"diagram_type,omitempty"`
	AST         *parsedAST   `json:"ast,omitempty"`
	Error       *SyntaxError `json:"error,omitempty"`
}

// VersionInfo describes parser/runtime version metadata reported by parser-node/parse.mjs.
type VersionInfo struct {
	ParserVersion  string `json:"parser_version"`
	MermaidVersion string `json:"mermaid_version"`
}

// parsedAST mirrors the simplified AST returned by parser-node/parse.mjs.
type parsedAST struct {
	Type         string              `json:"type"`
	Direction    string              `json:"direction"`
	Nodes        []parsedNode        `json:"nodes"`
	Edges        []parsedEdge        `json:"edges"`
	Subgraphs    []parsedSubgraph    `json:"subgraphs"`
	Suppressions []parsedSuppression `json:"suppressions"`
}

type parsedNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Line   *int   `json:"line,omitempty"`
	Column *int   `json:"column,omitempty"`
}

type parsedEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Line   *int   `json:"line,omitempty"`
	Column *int   `json:"column,omitempty"`
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
	SubgraphID string `json:"subgraphId,omitempty"`
}

// Parser wraps the Node subprocess invocation.
type Parser struct {
	scriptPath        string
	timeout           time.Duration
	repoRoot          string
	nodeMaxOldSpaceMB int
	mode              string
	workerPoolSize    int
	poolMu            sync.Mutex
	workerPools       map[int]*workerPool
	cache             *parseCache
	inflightMu        sync.Mutex
	inflightParses    map[string]*inflightParse
	versionMu         sync.Mutex
	parserVersion     string
	versionResolved   bool
}

type inflightParse struct {
	done      chan struct{}
	diagram   *model.Diagram
	syntaxErr *SyntaxError
	err       error
}

// New returns a Parser that will invoke the given Node.js script path.
func New(scriptPath string) (*Parser, error) {
	return NewWithConfig(scriptPath, ConfigFromEnv())
}

// NewWithConfig returns a Parser configured with explicit execution limits.
func NewWithConfig(scriptPath string, cfg Config) (*Parser, error) {
	return NewWithConfigAndRepoRootResolver(scriptPath, cfg, findRepoRoot)
}

// NewWithConfigAndRepoRootResolver returns a Parser configured with explicit
// execution limits and a caller-provided repository root resolver.
func NewWithConfigAndRepoRootResolver(scriptPath string, cfg Config, resolveRepoRoot func() (string, error)) (*Parser, error) {
	if resolveRepoRoot == nil {
		return nil, fmt.Errorf("failed to initialize parser: repo root resolver is nil")
	}

	root, err := resolveRepoRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize parser: %w", err)
	}

	effective := cfg.EffectiveConfig()

	return &Parser{
		scriptPath:        scriptPath,
		timeout:           effective.Timeout,
		repoRoot:          root,
		nodeMaxOldSpaceMB: effective.NodeMaxOldSpaceMB,
		mode:              readParserMode(),
		workerPoolSize:    readWorkerPoolSize(),
		workerPools:       make(map[int]*workerPool),
		cache:             newParseCache(),
		inflightParses:    make(map[string]*inflightParse),
	}, nil
}

// SetCacheMetrics configures parser-cache telemetry collectors.
func (p *Parser) SetCacheMetrics(metrics CacheMetricsObserver) {
	if p == nil || p.cache == nil {
		return
	}
	p.cache.setMetrics(metrics)
}

// Timeout returns the configured parser timeout duration.
func (p *Parser) Timeout() time.Duration {
	return p.timeout
}

// ParserConfig returns the parser's effective execution settings.
func (p *Parser) ParserConfig() Config {
	return Config{Timeout: p.timeout, NodeMaxOldSpaceMB: p.nodeMaxOldSpaceMB}
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

// VersionInfo returns parser script and Mermaid runtime version metadata.
func (p *Parser) VersionInfo() (*VersionInfo, error) {
	root, err := p.getRepoRoot()
	if err != nil {
		return nil, err
	}

	scriptPath, err := validateScriptPath(p.scriptPath, root)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", append(p.nodeArgs(), scriptPath, "--version-info")...) //nolint:gosec
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%w: after %s", ErrTimeout, p.timeout)
		}
		return nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, runErr, stderr.String())
	}

	var info VersionInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("%w: failed to decode parser version output: %w", ErrDecode, err)
	}
	if strings.TrimSpace(info.ParserVersion) == "" || strings.TrimSpace(info.MermaidVersion) == "" {
		return nil, fmt.Errorf("%w: version info missing parser_version or mermaid_version", ErrContract)
	}

	p.versionMu.Lock()
	p.parserVersion = strings.TrimSpace(info.ParserVersion)
	p.versionResolved = true
	p.versionMu.Unlock()

	return &info, nil
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
	return p.parseWithConfig(mermaidCode, Config{Timeout: p.timeout, NodeMaxOldSpaceMB: p.nodeMaxOldSpaceMB, SourceEnhancement: boolPtr(defaultParserSourceEnhancementEnabled), NeedSourceEnhancement: true}.EffectiveConfig())
}

// ParseWithConfig parses Mermaid code using explicit execution limits.
func (p *Parser) ParseWithConfig(mermaidCode string, cfg Config) (*model.Diagram, *SyntaxError, error) {
	return p.parseWithConfig(mermaidCode, cfg.EffectiveConfig())
}

func (p *Parser) parseWithConfig(mermaidCode string, cfg Config) (*model.Diagram, *SyntaxError, error) {
	cacheKey, ok := p.cacheKey(mermaidCode, cfg)
	var inFlight *inflightParse
	if ok {
		if diagram, syntaxErr, hit := p.cache.get(cacheKey); hit {
			return diagram, syntaxErr, nil
		}

		p.inflightMu.Lock()
		if existing := p.inflightParses[cacheKey]; existing != nil {
			p.inflightMu.Unlock()
			<-existing.done

			p.inflightMu.Lock()
			diagram := existing.diagram
			syntaxErr := existing.syntaxErr
			err := existing.err
			p.inflightMu.Unlock()

			return cloneDiagram(diagram), cloneSyntaxError(syntaxErr), err
		}
		inFlight = &inflightParse{done: make(chan struct{})}
		p.inflightParses[cacheKey] = inFlight
		p.inflightMu.Unlock()
	}

	var (
		diagram   *model.Diagram
		syntaxErr *SyntaxError
		err       error
	)
	if p.mode == parserModePool {
		diagram, syntaxErr, err = p.parseWithWorkerPool(mermaidCode, cfg)
	} else {
		diagram, syntaxErr, err = p.parseWithSubprocess(mermaidCode, cfg)
	}

	if ok && err == nil {
		if diagram != nil {
			p.cache.putSuccess(cacheKey, diagram)
		} else if syntaxErr != nil {
			p.cache.putSyntax(cacheKey, syntaxErr)
		}
	}

	if inFlight != nil {
		p.inflightMu.Lock()
		inFlight.diagram = cloneDiagram(diagram)
		inFlight.syntaxErr = cloneSyntaxError(syntaxErr)
		inFlight.err = err
		delete(p.inflightParses, cacheKey)
		close(inFlight.done)
		p.inflightMu.Unlock()

		return cloneDiagram(inFlight.diagram), cloneSyntaxError(inFlight.syntaxErr), inFlight.err
	}

	return diagram, syntaxErr, err
}

func (p *Parser) cacheKey(code string, cfg Config) (string, bool) {
	version, ok := p.getParserVersion()
	if !ok {
		return "", false
	}
	sourceEnhancement := defaultParserSourceEnhancementEnabled
	if cfg.SourceEnhancement != nil {
		sourceEnhancement = *cfg.SourceEnhancement
	}
	payload := strings.Join([]string{code, cfg.Timeout.String(), strconv.Itoa(cfg.NodeMaxOldSpaceMB), strconv.FormatBool(sourceEnhancement), strconv.FormatBool(cfg.NeedSourceEnhancement), version}, "\x00")
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:]), true
}

func (p *Parser) getParserVersion() (string, bool) {
	p.versionMu.Lock()
	if p.versionResolved && strings.TrimSpace(p.parserVersion) != "" {
		version := strings.TrimSpace(p.parserVersion)
		p.versionMu.Unlock()
		return version, true
	}
	if !p.versionResolved {
		p.versionResolved = true
		p.parserVersion = "unknown"
	}
	version := strings.TrimSpace(p.parserVersion)
	p.versionMu.Unlock()
	if version == "" {
		version = "unknown"
	}
	return version, true
}

func (p *Parser) parseWithSubprocess(mermaidCode string, cfg Config) (*model.Diagram, *SyntaxError, error) {
	root, err := p.getRepoRoot()
	if err != nil {
		return nil, nil, err
	}

	scriptPath, err := validateScriptPath(p.scriptPath, root)
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	// Use both a Node heap cap and a process timeout to reduce memory/CPU abuse.
	cmd := exec.CommandContext(ctx, "node", []string{fmt.Sprintf("--max-old-space-size=%d", cfg.NodeMaxOldSpaceMB), scriptPath}...) //nolint:gosec
	cmd.Stdin = bytes.NewBufferString(mermaidCode)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: after %s", ErrTimeout, cfg.Timeout), metadata: ErrorMetadata{Suggestion: "reduce diagram size or increase PARSER_TIMEOUT_SECONDS", Limit: cfg.Timeout.String()}}
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
			if isLikelyNodeMemoryLimit(stderr.String()) {
				return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: %w (stderr: %s)", ErrMemoryLimit, runErr, stderr.String()), metadata: ErrorMetadata{Suggestion: "reduce diagram size, batch requests, or increase PARSER_MAX_OLD_SPACE_MB", Limit: fmt.Sprintf("%d MiB", cfg.NodeMaxOldSpaceMB), ObservedSizeByte: len(mermaidCode)}}
			}
			return nil, nil, fmt.Errorf("%w: %w (stderr: %s)", ErrSubprocess, runErr, stderr.String())
		}
	}

	if !result.Valid {
		// Check if this is a memory limit error indicated by the parser-node script
		if result.Error != nil && strings.HasPrefix(result.Error.Message, "parser_memory_limit:") {
			return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: reported by parser-node", ErrMemoryLimit), metadata: ErrorMetadata{Suggestion: "reduce diagram size, batch requests, or increase PARSER_MAX_OLD_SPACE_MB", Limit: fmt.Sprintf("%d MiB", cfg.NodeMaxOldSpaceMB), ObservedSizeByte: len(mermaidCode)}}
		}
		return nil, result.Error, nil
	}

	if result.AST == nil {
		return nil, nil, fmt.Errorf("%w: valid result missing AST", ErrContract)
	}

	diagram := toDiagram(result)

	// Enhance the diagram with source-level analysis to preserve node information
	// that the Mermaid parser may drop during normalization
	if shouldEnhanceSourceAnalysis(diagram, cfg) {
		EnhanceASTWithSourceAnalysis(diagram, mermaidCode)
	}

	return diagram, nil, nil
}

func (p *Parser) parseWithWorkerPool(mermaidCode string, cfg Config) (*model.Diagram, *SyntaxError, error) {
	root, err := p.getRepoRoot()
	if err != nil {
		return nil, nil, err
	}

	scriptPath, err := validateScriptPath(p.scriptPath, root)
	if err != nil {
		return nil, nil, err
	}

	pool := p.poolForMemory(scriptPath, cfg.NodeMaxOldSpaceMB)
	worker, err := pool.borrow()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrSubprocess, err)
	}

	timedOut := make(chan struct{})
	respCh := make(chan *workerResponseEnvelope, 1)
	errCh := make(chan error, 1)
	req := workerRequestEnvelope{
		ID:      newWorkerRequestID(),
		Code:    mermaidCode,
		Timeout: cfg.Timeout.Milliseconds(),
		Limits:  &workerLimits{NodeMaxOldSpaceMB: cfg.NodeMaxOldSpaceMB},
	}

	go func() {
		resp, reqErr := worker.do(req)
		select {
		case <-timedOut:
			return
		default:
		}
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		respCh <- resp
	}()

	timer := time.NewTimer(cfg.Timeout)
	defer timer.Stop()

	select {
	case <-timer.C:
		close(timedOut)
		pool.release(worker, false)
		return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: after %s", ErrTimeout, cfg.Timeout), metadata: ErrorMetadata{Suggestion: "reduce diagram size or increase PARSER_TIMEOUT_SECONDS", Limit: cfg.Timeout.String()}}
	case reqErr := <-errCh:
		pool.release(worker, false)
		if errors.Is(reqErr, ErrDecode) || errors.Is(reqErr, ErrContract) {
			return nil, nil, reqErr
		}
		if isLikelyNodeMemoryLimit(reqErr.Error()) {
			return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: %w", ErrMemoryLimit, reqErr), metadata: ErrorMetadata{Suggestion: "reduce diagram size, batch requests, or increase PARSER_MAX_OLD_SPACE_MB", Limit: fmt.Sprintf("%d MiB", cfg.NodeMaxOldSpaceMB), ObservedSizeByte: len(mermaidCode)}}
		}
		return nil, nil, fmt.Errorf("%w: %w", ErrSubprocess, reqErr)
	case resp := <-respCh:
		pool.release(worker, true)
		if strings.TrimSpace(resp.Error) != "" {
			if isLikelyNodeMemoryLimit(resp.Error) {
				return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: %s", ErrMemoryLimit, resp.Error), metadata: ErrorMetadata{Suggestion: "reduce diagram size, batch requests, or increase PARSER_MAX_OLD_SPACE_MB", Limit: fmt.Sprintf("%d MiB", cfg.NodeMaxOldSpaceMB), ObservedSizeByte: len(mermaidCode)}}
			}
			return nil, nil, fmt.Errorf("%w: %s", ErrSubprocess, resp.Error)
		}
		return p.mapParseResult(resp.Result, mermaidCode, cfg)
	}
}

func newWorkerRequestID() string {
	random := make([]byte, 8)
	if _, err := crand.Read(random); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("req-%d-%s", time.Now().UnixNano(), hex.EncodeToString(random))
}

func (p *Parser) mapParseResult(result ParseResult, mermaidCode string, cfg Config) (*model.Diagram, *SyntaxError, error) {
	if !result.Valid {
		if result.Error != nil && strings.HasPrefix(result.Error.Message, "parser_memory_limit:") {
			return nil, nil, &parserExecutionError{err: fmt.Errorf("%w: reported by parser-node", ErrMemoryLimit), metadata: ErrorMetadata{Suggestion: "reduce diagram size, batch requests, or increase PARSER_MAX_OLD_SPACE_MB", Limit: fmt.Sprintf("%d MiB", cfg.NodeMaxOldSpaceMB), ObservedSizeByte: len(mermaidCode)}}
		}
		return nil, result.Error, nil
	}

	if result.AST == nil {
		return nil, nil, fmt.Errorf("%w: valid result missing AST", ErrContract)
	}

	diagram := toDiagram(result)
	if shouldEnhanceSourceAnalysis(diagram, cfg) {
		EnhanceASTWithSourceAnalysis(diagram, mermaidCode)
	}
	return diagram, nil, nil
}

func shouldEnhanceSourceAnalysis(diagram *model.Diagram, cfg Config) bool {
	if diagram == nil || cfg.SourceEnhancement == nil || !*cfg.SourceEnhancement || !cfg.NeedSourceEnhancement {
		return false
	}
	return diagram.Type.Family() == model.DiagramFamilyFlowchart
}

func (p *Parser) poolForMemory(scriptPath string, memoryMB int) *workerPool {
	p.poolMu.Lock()
	defer p.poolMu.Unlock()
	if pool, ok := p.workerPools[memoryMB]; ok {
		return pool
	}
	p.workerPools[memoryMB] = newWorkerPool(p.workerPoolSize, func() (*parserWorker, error) {
		return startParserWorker(scriptPath, []string{fmt.Sprintf("--max-old-space-size=%d", memoryMB)})
	})
	return p.workerPools[memoryMB]
}

func (p *Parser) nodeArgs() []string {
	return []string{fmt.Sprintf("--max-old-space-size=%d", p.nodeMaxOldSpaceMB)}
}

func readTimeoutSeconds() time.Duration {
	raw := strings.TrimSpace(os.Getenv("PARSER_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultTimeout
	}

	timeout := time.Duration(value) * time.Second
	if timeout < minTimeout || timeout > maxTimeout {
		return defaultTimeout
	}

	return timeout
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

	if value < minNodeMaxOldSpaceSizeMB || value > maxNodeMaxOldSpaceSizeMB {
		return defaultNodeMaxOldSpaceSizeMB
	}

	return value
}

func boolPtr(v bool) *bool {
	return &v
}

func readSourceEnhancementEnabled() *bool {
	raw := strings.TrimSpace(os.Getenv("PARSER_SOURCE_ENHANCEMENT"))
	if raw == "" {
		return boolPtr(defaultParserSourceEnhancementEnabled)
	}

	value, err := strconv.ParseBool(raw)
	if err != nil {
		return boolPtr(defaultParserSourceEnhancementEnabled)
	}
	return boolPtr(value)
}

// ModeFromEnv returns the normalized parser execution mode from PARSER_MODE.
func ModeFromEnv() string {
	return readParserMode()
}

// WorkerPoolSizeFromEnv returns the normalized worker pool size from PARSER_WORKER_POOL_SIZE.
func WorkerPoolSizeFromEnv() int {
	return readWorkerPoolSize()
}

func readParserMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PARSER_MODE")))
	switch mode {
	case parserModePool, parserModeAuto, "":
		return parserModePool
	case parserModeSubprocess:
		return parserModeSubprocess
	default:
		return defaultParserMode
	}
}

func readWorkerPoolSize() int {
	raw := strings.TrimSpace(os.Getenv("PARSER_WORKER_POOL_SIZE"))
	if raw == "" {
		return defaultWorkerPoolSize
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultWorkerPoolSize
	}
	if value < minWorkerPoolSize {
		return minWorkerPoolSize
	}
	if value > maxWorkerPoolSize {
		return maxWorkerPoolSize
	}
	return value
}

func isLikelyNodeMemoryLimit(stderr string) bool {
	msg := strings.ToLower(stderr)
	return strings.Contains(msg, "heap out of memory") || strings.Contains(msg, "allocation failed") || strings.Contains(msg, "javascript heap out of memory")
}

func normalizeDiagramType(rawType string) model.DiagramType {
	switch strings.ToLower(strings.TrimSpace(rawType)) {
	case string(model.DiagramTypeFlowchart):
		return model.DiagramTypeFlowchart
	case string(model.DiagramTypeSequence):
		return model.DiagramTypeSequence
	case string(model.DiagramTypeClass):
		return model.DiagramTypeClass
	case string(model.DiagramTypeER):
		return model.DiagramTypeER
	case string(model.DiagramTypeState):
		return model.DiagramTypeState
	default:
		return model.DiagramTypeUnknown
	}
}

// toDiagram converts the raw AST into the internal model.
func toDiagram(result ParseResult) *model.Diagram {
	ast := result.AST
	if ast == nil {
		return &model.Diagram{}
	}
	diagramType := normalizeDiagramType(result.DiagramType)
	if diagramType == model.DiagramTypeUnknown {
		diagramType = normalizeDiagramType(ast.Type)
	}

	d := &model.Diagram{
		Type:      diagramType,
		Direction: ast.Direction,
	}

	for _, n := range ast.Nodes {
		d.Nodes = append(d.Nodes, model.Node{
			ID:     n.ID,
			Label:  n.Label,
			Line:   n.Line,
			Column: n.Column,
		})
	}
	for _, e := range ast.Edges {
		d.Edges = append(d.Edges, model.Edge{
			From:   e.From,
			To:     e.To,
			Type:   e.Type,
			Line:   e.Line,
			Column: e.Column,
		})
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
			SubgraphID: suppression.SubgraphID,
		})
	}
	return d
}
