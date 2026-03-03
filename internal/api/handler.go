// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

const maxAnalyzeBodyBytes int64 = 1 << 20 // 1 MiB

//go:embed swagger.html
var swaggerHTML []byte

// ParserInterface defines the contract for parsing Mermaid code.
// It allows dependency injection of mock parsers in tests.
type ParserInterface interface {
	Parse(code string) (*model.Diagram, *parser.SyntaxError, error)
}

// ReadinessChecker can be implemented by parser dependencies that support
// lightweight readiness validation (e.g., binary/script availability checks).
type ReadinessChecker interface {
	Ready() error
}

// analyzeRequest is the JSON body accepted by POST /analyze.
type analyzeRequest struct {
	Code   string          `json:"code"`
	Config json.RawMessage `json:"config"`
}

// parseConfig accepts both flat {"rule-id": {...}} and nested
// {"rules": {"rule-id": {...}}} config formats.
func parseConfig(raw json.RawMessage) rules.Config {
	if len(raw) == 0 {
		return rules.Config{}
	}

	// Try nested format first: {"rules": {...}}
	var nested struct {
		Rules rules.Config `json:"rules"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil && len(nested.Rules) > 0 {
		return nested.Rules
	}

	// Fall back to flat format: {"rule-id": {...}}
	var flat rules.Config
	if err := json.Unmarshal(raw, &flat); err == nil {
		return flat
	}
	return rules.Config{}
}

// syntaxErrorResponse mirrors parser.SyntaxError for the JSON response.
type syntaxErrorResponse struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// metricsResponse holds aggregate statistics about the diagram.
type metricsResponse struct {
	NodeCount int `json:"node_count"`
	EdgeCount int `json:"edge_count"`
	MaxFanout int `json:"max_fanout"`
}

// analyzeResponse is returned by POST /analyze.
type analyzeResponse struct {
	Valid         bool                 `json:"valid"`
	DiagramType   model.DiagramType    `json:"diagram_type,omitempty"`
	LintSupported bool                 `json:"lint_supported"`
	SyntaxError   *syntaxErrorResponse `json:"syntax_error"`
	Issues        []model.Issue        `json:"issues"`
	Metrics       *metricsResponse     `json:"metrics,omitempty"`
}

// apiErrorDetails holds machine-readable and human-readable error information.
type apiErrorDetails struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// errorResponse is returned for non-200 responses.
type errorResponse struct {
	Valid  bool            `json:"valid"`
	Issues []model.Issue   `json:"issues"`
	Error  apiErrorDetails `json:"error"`
}

// Handler holds the dependencies needed to serve HTTP requests.
type Handler struct {
	parser              ParserInterface
	engine              *engine.Engine
	mu                  sync.RWMutex
	parserConcurrencyCh chan struct{}
}

// NewHandler creates a Handler with the given parser and engine.
// This constructor allows dependency injection for testing.
func NewHandler(p ParserInterface, e *engine.Engine) *Handler {
	return &Handler{
		parser: p,
		engine: e,
	}
}

// SetParserConcurrencyLimit configures a limit for concurrent parser invocations.
// A value <= 0 disables the limit.
func (h *Handler) SetParserConcurrencyLimit(limit int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if limit <= 0 {
		h.parserConcurrencyCh = nil
		return
	}

	h.parserConcurrencyCh = make(chan struct{}, limit)
}

// NewHandlerWithScript creates a Handler wired with a real parser using the given script path.
// This is the typical constructor for production use.
func NewHandlerWithScript(scriptPath string) (*Handler, error) {
	p, err := parser.New(scriptPath)
	if err != nil {
		return nil, err
	}

	return NewHandler(
		p,
		engine.New(),
	), nil
}

// RegisterRoutes attaches all routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("POST /analyze", h.Analyze)
	mux.HandleFunc("GET /spec", h.ServeSpec)
	mux.HandleFunc("GET /docs", h.ServeSwagger)
}

// Healthz handles GET /healthz and reports process liveness.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /ready and reports dependency readiness.
func (h *Handler) Ready(w http.ResponseWriter, _ *http.Request) {
	if checker, ok := h.parser.(ReadinessChecker); ok {
		if err := checker.Ready(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// Analyze handles POST /analyze.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalyzeBodyBytes)

	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 1 MiB limit")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "missing_code", "field 'code' is required")
		return
	}

	cfg := parseConfig(req.Config)
	normalizedCfg, err := h.engine.NormalizeConfig(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}

	h.mu.RLock()
	parserConcurrencyCh := h.parserConcurrencyCh
	h.mu.RUnlock()

	if parserConcurrencyCh != nil {
		select {
		case parserConcurrencyCh <- struct{}{}:
			defer func() { <-parserConcurrencyCh }()
		default:
			writeError(w, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again")
			return
		}
	}

	diagram, syntaxErr, err := h.parser.Parse(req.Code)
	if err != nil {
		writeParserFailure(w, err)
		return
	}

	if syntaxErr != nil {
		resp := analyzeResponse{
			Valid:         false,
			LintSupported: false,
			SyntaxError: &syntaxErrorResponse{
				Message: syntaxErr.Message,
				Line:    syntaxErr.Line,
				Column:  syntaxErr.Column,
			},
			Issues: []model.Issue{},
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if diagram == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "parser returned nil diagram")
		return
	}

	issues := h.engine.Run(diagram, normalizedCfg)

	resp := analyzeResponse{
		Valid:         true,
		DiagramType:   diagram.Type,
		LintSupported: diagram.Type.Family() == model.DiagramFamilyFlowchart,
		SyntaxError:   nil,
		Issues:        issues,
		Metrics:       computeMetrics(diagram),
	}
	writeJSON(w, http.StatusOK, resp)
}

// computeMetrics derives aggregate metrics from the diagram.
func computeMetrics(d *model.Diagram) *metricsResponse {
	fanout := make(map[string]int)
	for _, e := range d.Edges {
		fanout[e.From]++
	}
	maxFanout := 0
	for _, v := range fanout {
		if v > maxFanout {
			maxFanout = v
		}
	}
	return &metricsResponse{
		NodeCount: len(d.Nodes),
		EdgeCount: len(d.Edges),
		MaxFanout: maxFanout,
	}
}

// ServeSpec serves the OpenAPI specification as JSON.
func (h *Handler) ServeSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")

	data, _ := json.MarshalIndent(openapi, "", "  ")
	w.Write(data)
}

// ServeSwagger serves the Swagger UI HTML page.
func (h *Handler) ServeSwagger(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(swaggerHTML)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Valid:  false,
		Issues: []model.Issue{},
		Error: apiErrorDetails{
			Code:    code,
			Message: message,
		},
	})
}

func writeParserFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, parser.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "parser_timeout", "parser timed out while validating Mermaid code")
	case errors.Is(err, parser.ErrSubprocess):
		writeError(w, http.StatusInternalServerError, "parser_subprocess_error", "parser subprocess failed")
	case errors.Is(err, parser.ErrDecode):
		writeError(w, http.StatusInternalServerError, "parser_decode_error", "parser returned malformed output")
	case errors.Is(err, parser.ErrContract):
		writeError(w, http.StatusInternalServerError, "parser_contract_violation", "parser response violated service contract")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
