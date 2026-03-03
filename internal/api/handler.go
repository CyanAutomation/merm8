// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
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

// parseConfig validates and accepts both flat {"rule-id": {...}} and nested
// {"rules": {"rule-id": {...}}} config formats.
type validationError struct {
	Code      string
	Path      string
	Message   string
	Supported []string
}

// parseConfig validates and accepts both flat {"rule-id": {...}} and nested
// {"rules": {"rule-id": {...}}} config formats.
func parseConfig(raw json.RawMessage, knownRuleIDs map[string]struct{}) (rules.Config, *validationError) {
	if len(raw) == 0 {
		return rules.Config{}, nil
	}

	var configValue any
	if err := json.Unmarshal(raw, &configValue); err != nil {
		return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
	}

	asMap, ok := configValue.(map[string]any)
	if !ok {
		return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "config must be object"}
	}

	cfgRaw := raw
	rulePathPrefix := "config"

	schemaVersionValue, hasSchemaVersion := asMap["schema-version"]
	if !hasSchemaVersion {
		schemaVersionValue, hasSchemaVersion = asMap["schema_version"]
	}
	if hasSchemaVersion {
		schemaVersion, ok := schemaVersionValue.(string)
		if !ok {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config.schema-version", Message: "config.schema-version must be string"}
		}
		if schemaVersion != rules.CurrentConfigSchemaVersion {
			return rules.Config{}, &validationError{
				Code:      "unsupported_schema_version",
				Path:      "config.schema-version",
				Message:   "unsupported config schema-version: " + schemaVersion,
				Supported: []string{rules.CurrentConfigSchemaVersion},
			}
		}
		rulesValue, hasRules := asMap["rules"]
		if !hasRules {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules is required when config.schema-version is set"}
		}
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules must be object"}
		}
		for topLevelKey := range asMap {
			if topLevelKey != "schema-version" && topLevelKey != "schema_version" && topLevelKey != "rules" {
				return rules.Config{}, &validationError{
					Code:      "unknown_option",
					Path:      "config." + topLevelKey,
					Message:   "unknown option: " + topLevelKey,
					Supported: []string{"schema-version", "rules"},
				}
			}
		}
		rawRules, err := json.Marshal(rulesMap)
		if err != nil {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		cfgRaw = rawRules
		asMap = rulesMap
		rulePathPrefix = "config.rules"
	} else if rulesValue, hasRules := asMap["rules"]; hasRules {
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules must be object"}
		}
		rulePathPrefix = "config.rules"
		ruleMapRaw, err := json.Marshal(rulesMap)
		if err != nil {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		cfgRaw = ruleMapRaw
		asMap = rulesMap
	}

	for ruleID, ruleConfig := range asMap {
		ruleConfigMap, ok := ruleConfig.(map[string]any)
		if !ok {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: rulePathPrefix + "." + ruleID, Message: rulePathPrefix + "." + ruleID + " must be object"}
		}
		if _, known := knownRuleIDs[ruleID]; !known {
			return rules.Config{}, &validationError{
				Code:      "unknown_rule",
				Path:      rulePathPrefix + "." + ruleID,
				Message:   "unknown rule: " + ruleID,
				Supported: sortedRuleIDs(knownRuleIDs),
			}
		}

		registry, ok := rules.ConfigRegistry()[ruleID]
		if !ok {
			return rules.Config{}, &validationError{
				Code:      "unknown_rule",
				Path:      rulePathPrefix + "." + ruleID,
				Message:   "unknown rule: " + ruleID,
				Supported: sortedRuleIDs(knownRuleIDs),
			}
		}

		for optionKey, optionValue := range ruleConfigMap {
			canonicalOptionKey := rules.NormalizeOptionKey(optionKey)
			if !contains(registry.AllowedOptionKeys, canonicalOptionKey) {
				return rules.Config{}, &validationError{
					Code:      "unknown_option",
					Path:      rulePathPrefix + "." + ruleID + "." + optionKey,
					Message:   "unknown option: " + optionKey,
					Supported: registry.AllowedOptionKeys,
				}
			}
			if err := rules.ValidateOption(ruleID, optionKey, optionValue); err != nil {
				return rules.Config{}, &validationError{
					Code:    "invalid_option",
					Path:    rulePathPrefix + "." + ruleID + "." + optionKey,
					Message: "invalid option value for " + optionKey,
				}
			}
		}

		ruleConfigRaw, err := json.Marshal(ruleConfigMap)
		if err != nil {
			return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		asMap[ruleID] = json.RawMessage(ruleConfigRaw)
	}

	var cfg rules.Config
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		return rules.Config{}, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
	}
	return cfg, nil
}

func sortedRuleIDs(knownRuleIDs map[string]struct{}) []string {
	supported := make([]string, 0, len(knownRuleIDs))
	for ruleID := range knownRuleIDs {
		supported = append(supported, ruleID)
	}
	sort.Strings(supported)
	return supported
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// syntaxErrorResponse mirrors parser.SyntaxError for the JSON response.
type syntaxErrorResponse struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

// metricsResponse holds aggregate statistics about the diagram.
type metricsResponse struct {
	NodeCount             int                  `json:"node-count"`
	EdgeCount             int                  `json:"edge-count"`
	DisconnectedNodeCount int                  `json:"disconnected-node-count"`
	DuplicateNodeCount    int                  `json:"duplicate-node-count"`
	MaxFanin              int                  `json:"max-fanin"`
	MaxFanout             int                  `json:"max-fanout"`
	DiagramType           model.DiagramType    `json:"diagram-type"`
	Direction             string               `json:"direction,omitempty"`
	IssueCounts           *issueCountsResponse `json:"issue-counts,omitempty"`
}

// issueCountsResponse summarizes issue distribution from lint results.
type issueCountsResponse struct {
	BySeverity map[string]int `json:"by-severity"`
	ByRule     map[string]int `json:"by-rule"`
}

// analyzeResponse is returned by POST /analyze.
type analyzeResponse struct {
	Valid         bool                 `json:"valid"`
	DiagramType   model.DiagramType    `json:"diagram-type,omitempty"`
	LintSupported bool                 `json:"lint-supported"`
	SyntaxError   *syntaxErrorResponse `json:"syntax-error"`
	Issues        []model.Issue        `json:"issues"`
	Error         *apiErrorDetails     `json:"error,omitempty"`
	Metrics       *metricsResponse     `json:"metrics,omitempty"`
}

// ruleOptionResponse describes a configurable option for a lint rule.
type ruleOptionResponse struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Constraints string `json:"constraints,omitempty"`
}

// ruleResponse describes built-in rule metadata exposed by GET /rules.
type ruleResponse struct {
	ID                  string                 `json:"id"`
	Severity            string                 `json:"severity"`
	Description         string                 `json:"description"`
	DefaultConfig       map[string]interface{} `json:"default-config"`
	ConfigurableOptions []ruleOptionResponse   `json:"configurable-options"`
}

// apiErrorDetails holds machine-readable and human-readable error information.
type apiErrorDetails struct {
	Code      string   `json:"code"`
	Message   string   `json:"message"`
	Path      string   `json:"path,omitempty"`
	Supported []string `json:"supported,omitempty"`
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
	mux.HandleFunc("GET /health", h.Healthz)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("GET /rules", h.ListRules)
	mux.HandleFunc("GET /rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("POST /analyze", h.Analyze)
	mux.HandleFunc("GET /spec", h.ServeSpec)
	mux.HandleFunc("GET /docs", h.ServeSwagger)
}

// ListRules handles GET /rules.
func (h *Handler) ListRules(w http.ResponseWriter, _ *http.Request) {
	metadata := rules.ListRuleMetadata()
	resp := make([]ruleResponse, 0, len(metadata))
	for _, rule := range metadata {
		options := make([]ruleOptionResponse, 0, len(rule.ConfigurableOptions))
		for _, option := range rule.ConfigurableOptions {
			options = append(options, ruleOptionResponse{
				Name:        option.Name,
				Type:        option.Type,
				Description: option.Description,
				Constraints: option.Constraints,
			})
		}
		resp = append(resp, ruleResponse{
			ID:                  rule.ID,
			Severity:            rule.Severity,
			Description:         rule.Description,
			DefaultConfig:       rule.DefaultConfig,
			ConfigurableOptions: options,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": resp})
}

// RuleConfigSchema handles GET /rules/schema.
func (h *Handler) RuleConfigSchema(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"schema": rules.ConfigJSONSchema()})
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

	cfg, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs())
	if configValidationErr != nil {
		writeConfigValidationError(w, configValidationErr)
		return
	}

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
		diagramType := defaultDiagramTypeForSyntaxError(req.Code)
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagramType,
			LintSupported: diagramType.Family() == model.DiagramFamilyFlowchart,
			SyntaxError: &syntaxErrorResponse{
				Message: syntaxErr.Message,
				Line:    syntaxErr.Line,
				Column:  syntaxErr.Column,
			},
			Issues:  []model.Issue{},
			Metrics: defaultMetrics(diagramType),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if diagram == nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "parser returned nil diagram")
		return
	}

	family := diagram.Type.Family()
	if family != model.DiagramFamilyFlowchart {
		// Keep metrics in the response for parsed diagrams, even when linting is
		// not currently supported for that Mermaid family.
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagram.Type,
			LintSupported: false,
			SyntaxError:   nil,
			Issues:        []model.Issue{},
			Error: &apiErrorDetails{
				Code:    "unsupported_diagram_type",
				Message: "diagram type is parsed but linting is not supported",
			},
			Metrics: computeMetrics(diagram, []model.Issue{}),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	issues := h.engine.Run(diagram, normalizedCfg)

	resp := analyzeResponse{
		Valid:         true,
		DiagramType:   diagram.Type,
		LintSupported: diagram.Type.Family() == model.DiagramFamilyFlowchart,
		SyntaxError:   nil,
		Issues:        issues,
		Metrics:       computeMetrics(diagram, issues),
	}
	writeJSON(w, http.StatusOK, resp)
}

func defaultDiagramTypeForSyntaxError(code string) model.DiagramType {
	firstLine := strings.TrimSpace(strings.SplitN(code, "\n", 2)[0])
	switch {
	case strings.HasPrefix(firstLine, "graph"), strings.HasPrefix(firstLine, "flowchart"):
		return model.DiagramTypeFlowchart
	case strings.HasPrefix(firstLine, "sequenceDiagram"):
		return model.DiagramTypeSequence
	case strings.HasPrefix(firstLine, "classDiagram"):
		return model.DiagramTypeClass
	case strings.HasPrefix(firstLine, "erDiagram"):
		return model.DiagramTypeER
	case strings.HasPrefix(firstLine, "stateDiagram"):
		return model.DiagramTypeState
	default:
		return model.DiagramTypeUnknown
	}
}

func defaultMetrics(diagramType model.DiagramType) *metricsResponse {
	return &metricsResponse{
		NodeCount:             0,
		EdgeCount:             0,
		DisconnectedNodeCount: 0,
		DuplicateNodeCount:    0,
		MaxFanin:              0,
		MaxFanout:             0,
		DiagramType:           diagramType,
		IssueCounts: &issueCountsResponse{
			BySeverity: map[string]int{},
			ByRule:     map[string]int{},
		},
	}
}

func writeConfigValidationError(w http.ResponseWriter, configValidationErr *validationError) {
	writeJSON(w, http.StatusBadRequest, analyzeResponse{
		Valid: false,
		Error: &apiErrorDetails{
			Code:      configValidationErr.Code,
			Message:   configValidationErr.Message,
			Path:      configValidationErr.Path,
			Supported: configValidationErr.Supported,
		},
		LintSupported: false,
		SyntaxError:   nil,
		Issues:        []model.Issue{},
	})
}

// computeMetrics derives aggregate metrics from the diagram.
func computeMetrics(d *model.Diagram, issues []model.Issue) *metricsResponse {
	fanout := make(map[string]int)
	fanin := make(map[string]int)
	for _, e := range d.Edges {
		fanout[e.From]++
		fanin[e.To]++
	}
	maxFanout := 0
	for _, v := range fanout {
		if v > maxFanout {
			maxFanout = v
		}
	}
	maxFanin := 0
	for _, v := range fanin {
		if v > maxFanin {
			maxFanin = v
		}
	}

	duplicateNodeCount := 0
	nodeOccurrences := make(map[string]int, len(d.Nodes))
	for _, n := range d.Nodes {
		nodeOccurrences[n.ID]++
		if nodeOccurrences[n.ID] > 1 {
			duplicateNodeCount++
		}
	}

	connected := make(map[string]bool, len(d.Nodes))
	for _, e := range d.Edges {
		connected[e.From] = true
		connected[e.To] = true
	}
	disconnectedNodeCount := 0
	for _, n := range d.Nodes {
		if !connected[n.ID] {
			disconnectedNodeCount++
		}
	}

	issueCounts := &issueCountsResponse{
		BySeverity: map[string]int{},
		ByRule:     map[string]int{},
	}
	for _, issue := range issues {
		issueCounts.BySeverity[issue.Severity]++
		issueCounts.ByRule[issue.RuleID]++
	}

	return &metricsResponse{
		NodeCount:             len(d.Nodes),
		EdgeCount:             len(d.Edges),
		DisconnectedNodeCount: disconnectedNodeCount,
		DuplicateNodeCount:    duplicateNodeCount,
		MaxFanin:              maxFanin,
		MaxFanout:             maxFanout,
		DiagramType:           d.Type,
		Direction:             d.Direction,
		IssueCounts:           issueCounts,
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
	writeJSON(w, status, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		SyntaxError:   nil,
		Issues:        []model.Issue{},
		Error: &apiErrorDetails{
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
