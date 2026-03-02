// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

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
	NodeCount  int `json:"node_count"`
	EdgeCount  int `json:"edge_count"`
	MaxFanout  int `json:"max_fanout"`
}

// analyzeResponse is returned by POST /analyze.
type analyzeResponse struct {
	Valid       bool                 `json:"valid"`
	SyntaxError *syntaxErrorResponse `json:"syntax_error"`
	Issues      []model.Issue        `json:"issues"`
	Metrics     *metricsResponse     `json:"metrics,omitempty"`
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
}

// Analyze handles POST /analyze.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	cfg := parseConfig(req.Config)
	issues := h.engine.Run(diagram, cfg)

	resp := analyzeResponse{
		Valid:       true,
		SyntaxError: nil,
		Issues:      issues,
		Metrics:     computeMetrics(diagram),
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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
