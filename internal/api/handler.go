// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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
		Rules map[string]map[string]interface{} `json:"rules"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil && len(nested.Rules) > 0 {
		return toRuleConfig(nested.Rules)
	}

	// Fall back to flat format: {"rule-id": {...}}
	var flat map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &flat); err == nil {
		return toRuleConfig(flat)
	}
	return rules.Config{}
}

func toRuleConfig(raw map[string]map[string]interface{}) rules.Config {
	cfg := make(rules.Config, len(raw))
	for id, values := range raw {
		rc := rules.RuleConfig{Options: map[string]interface{}{}}
		for k, v := range values {
			switch strings.ToLower(k) {
			case "enabled":
				if b, ok := v.(bool); ok {
					rc.Enabled = &b
				}
			case "severity":
				if s, ok := v.(string); ok {
					rc.Severity = s
				}
			case "suppress":
				if arr, ok := v.([]interface{}); ok {
					for _, it := range arr {
						if s, ok := it.(string); ok {
							rc.Suppress = append(rc.Suppress, s)
						}
					}
				}
			default:
				rc.Options[k] = v
			}
		}
		cfg[id] = rc
	}
	return cfg
}

// syntaxErrorResponse mirrors parser.SyntaxError for the JSON response.
type syntaxErrorResponse struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// metricsResponse holds aggregate statistics about the diagram.
type metricsResponse struct {
	NodeCount              int `json:"node_count"`
	EdgeCount              int `json:"edge_count"`
	MaxFanout              int `json:"max_fanout"`
	UnknownRuleConfigCount int `json:"unknown_rule_config_count"`
}

// analyzeResponse is returned by POST /analyze.
type analyzeResponse struct {
	Valid       bool                 `json:"valid"`
	SyntaxError *syntaxErrorResponse `json:"syntax_error"`
	Issues      []model.Issue        `json:"issues"`
	Metrics     *metricsResponse     `json:"metrics,omitempty"`
	Warnings    []string             `json:"warnings,omitempty"`
}

// Handler holds the dependencies needed to serve HTTP requests.
type Handler struct {
	parser ParserInterface
	engine *engine.Engine
}

// NewHandler creates a Handler with the given parser and engine.
// This constructor allows dependency injection for testing.
func NewHandler(p ParserInterface, e *engine.Engine) *Handler {
	return &Handler{
		parser: p,
		engine: e,
	}
}

// NewHandlerWithScript creates a Handler wired with a real parser using the given script path.
// This is the typical constructor for production use.
func NewHandlerWithScript(scriptPath string) *Handler {
	return NewHandler(
		parser.New(scriptPath),
		engine.New(),
	)
}

// RegisterRoutes attaches all routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /analyze", h.Analyze)
	mux.HandleFunc("GET /spec", h.ServeSpec)
	mux.HandleFunc("GET /docs", h.ServeSwagger)
}

// Analyze handles POST /analyze.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAnalyzeBodyBytes)

	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body exceeds 1 MiB limit"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.Code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "field 'code' is required"})
		return
	}

	diagram, syntaxErr, err := h.parser.Parse(req.Code)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if syntaxErr != nil {
		resp := analyzeResponse{
			Valid: false,
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "parser returned nil diagram"})
		return
	}

	cfg := parseConfig(req.Config)
	normalized, warnings := h.engine.NormalizeConfig(cfg)
	issues := h.engine.Run(diagram, normalized)

	resp := analyzeResponse{
		Valid:       true,
		SyntaxError: nil,
		Issues:      issues,
		Metrics:     computeMetrics(diagram, len(warnings)),
		Warnings:    warnings,
	}
	writeJSON(w, http.StatusOK, resp)
}

// computeMetrics derives aggregate metrics from the diagram.
func computeMetrics(d *model.Diagram, unknownRuleConfigCount int) *metricsResponse {
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
		NodeCount:              len(d.Nodes),
		EdgeCount:              len(d.Edges),
		MaxFanout:              maxFanout,
		UnknownRuleConfigCount: unknownRuleConfigCount,
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
