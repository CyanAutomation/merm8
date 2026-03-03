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
	"sync/atomic"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/output/sarif"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
	"github.com/CyanAutomation/merm8/internal/telemetry"
)

const maxAnalyzeBodyBytes int64 = 1 << 20 // 1 MiB

const (
	legacyConfigDeprecationMessage = "legacy config format is deprecated; migrate to {\"schema-version\":\"v1\",\"rules\":{...}} with kebab-case keys"
	legacyConfigDeprecationHeader  = "299 - \"legacy config format is deprecated and will be rejected in a future phase\""
)

var strictConfigSchema = false

//go:embed swagger.html
var swaggerHTML []byte

// ParserInterface defines the contract for parsing Mermaid code.
// It allows dependency injection of mock parsers in tests.
type ParserInterface interface {
	Parse(code string) (*model.Diagram, *parser.SyntaxError, error)
}

// VersionInfoProvider can be implemented by parser dependencies that expose runtime versions.
type VersionInfoProvider interface {
	VersionInfo() (*parser.VersionInfo, error)
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

type validationError struct {
	Code      string
	Path      string
	Message   string
	Supported []string
}

// parseConfig validates config payloads.
// In strict mode, only canonical versioned payloads are accepted:
// {"schema-version":"v1","rules":{...}}.
func parseConfig(raw json.RawMessage, knownRuleIDs map[string]struct{}, strict bool) (rules.Config, []string, *validationError) {
	if len(raw) == 0 {
		return rules.Config{}, nil, nil
	}

	var configValue any
	if err := json.Unmarshal(raw, &configValue); err != nil {
		return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
	}

	asMap, ok := configValue.(map[string]any)
	if !ok {
		return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "config must be object"}
	}

	cfgRaw := raw
	rulePathPrefix := "config"
	deprecations := make([]string, 0, 2)

	schemaVersionValue, hasSchemaVersion := asMap["schema-version"]
	if legacySchemaVersionValue, hasLegacySchemaVersion := asMap["schema_version"]; hasLegacySchemaVersion {
		if strict {
			return rules.Config{}, nil, &validationError{Code: "deprecated_config_format", Path: "config.schema_version", Message: "config.schema_version is deprecated; use config.schema-version"}
		}
		schemaVersionValue = legacySchemaVersionValue
		hasSchemaVersion = true
		deprecations = append(deprecations, legacyConfigDeprecationMessage)
	}

	if strict && !hasSchemaVersion {
		return rules.Config{}, nil, &validationError{Code: "deprecated_config_format", Path: "config", Message: "legacy unversioned config shape is deprecated; use config.schema-version and config.rules"}
	}

	if hasSchemaVersion {
		schemaVersion, ok := schemaVersionValue.(string)
		if !ok {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config.schema-version", Message: "config.schema-version must be string"}
		}
		if schemaVersion != rules.CurrentConfigSchemaVersion {
			return rules.Config{}, nil, &validationError{
				Code:      "unsupported_schema_version",
				Path:      "config.schema-version",
				Message:   "unsupported config schema-version: " + schemaVersion,
				Supported: []string{rules.CurrentConfigSchemaVersion},
			}
		}

		rulesValue, hasRules := asMap["rules"]
		if !hasRules {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules is required when config.schema-version is set"}
		}
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules must be object"}
		}
		for topLevelKey := range asMap {
			if topLevelKey != "schema-version" && topLevelKey != "rules" && !(topLevelKey == "schema_version" && !strict) {
				return rules.Config{}, nil, &validationError{
					Code:      "unknown_option",
					Path:      "config." + topLevelKey,
					Message:   "unknown option: " + topLevelKey,
					Supported: []string{"schema-version", "rules"},
				}
			}
		}
		rawRules, err := json.Marshal(rulesMap)
		if err != nil {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		cfgRaw = rawRules
		asMap = rulesMap
		rulePathPrefix = "config.rules"
	} else if rulesValue, hasRules := asMap["rules"]; hasRules {
		if strict {
			return rules.Config{}, nil, &validationError{Code: "deprecated_config_format", Path: "config", Message: "legacy unversioned config shape is deprecated; use config.schema-version and config.rules"}
		}
		deprecations = append(deprecations, legacyConfigDeprecationMessage)
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config.rules", Message: "config.rules must be object"}
		}
		rulePathPrefix = "config.rules"
		ruleMapRaw, err := json.Marshal(rulesMap)
		if err != nil {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		cfgRaw = ruleMapRaw
		asMap = rulesMap
	} else if strict {
		return rules.Config{}, nil, &validationError{Code: "deprecated_config_format", Path: "config", Message: "legacy unversioned config shape is deprecated; use config.schema-version and config.rules"}
	} else {
		deprecations = append(deprecations, legacyConfigDeprecationMessage)
	}

	for ruleID, ruleConfig := range asMap {
		ruleConfigMap, ok := ruleConfig.(map[string]any)
		if !ok {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: rulePathPrefix + "." + ruleID, Message: rulePathPrefix + "." + ruleID + " must be object"}
		}
		if _, known := knownRuleIDs[ruleID]; !known {
			return rules.Config{}, nil, &validationError{
				Code:      "unknown_rule",
				Path:      rulePathPrefix + "." + ruleID,
				Message:   "unknown rule: " + ruleID,
				Supported: sortedRuleIDs(knownRuleIDs),
			}
		}

		registry, ok := rules.ConfigRegistry()[ruleID]
		if !ok {
			return rules.Config{}, nil, &validationError{
				Code:      "unknown_rule",
				Path:      rulePathPrefix + "." + ruleID,
				Message:   "unknown rule: " + ruleID,
				Supported: sortedRuleIDs(knownRuleIDs),
			}
		}

		for optionKey, optionValue := range ruleConfigMap {
			canonicalOptionKey := rules.NormalizeOptionKey(optionKey)
			if !contains(registry.AllowedOptionKeys, canonicalOptionKey) {
				return rules.Config{}, nil, &validationError{
					Code:      "unknown_option",
					Path:      rulePathPrefix + "." + ruleID + "." + optionKey,
					Message:   "unknown option: " + optionKey,
					Supported: registry.AllowedOptionKeys,
				}
			}
			if err := rules.ValidateOption(ruleID, optionKey, optionValue); err != nil {
				return rules.Config{}, nil, &validationError{
					Code:    "invalid_option",
					Path:    rulePathPrefix + "." + ruleID + "." + optionKey,
					Message: "invalid option value for " + optionKey,
				}
			}
		}

		ruleConfigRaw, err := json.Marshal(ruleConfigMap)
		if err != nil {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
		asMap[ruleID] = json.RawMessage(ruleConfigRaw)
	}

	var cfg rules.Config
	if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
		return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
	}
	return cfg, deprecations, nil
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
	Warnings      []string             `json:"warnings,omitempty"`
	Error         *apiErrorDetails     `json:"error,omitempty"`
	Metrics       *metricsResponse     `json:"metrics"`
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

type diagramTypesResponse struct {
	ParserRecognized []model.DiagramType   `json:"parser-recognized"`
	LintSupported    []model.DiagramFamily `json:"lint-supported"`
}

// apiErrorDetails holds machine-readable and human-readable error information.
type apiErrorDetails struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Handler holds the dependencies needed to serve HTTP requests.
type Handler struct {
	parser              ParserInterface
	engine              *engine.Engine
	logger              Logger
	serviceVersion      string
	metricsHandler      http.Handler
	telemetryMetrics    *telemetry.Metrics
	analyzeCounters     analyzeOutcomeCounters
	mu                  sync.RWMutex
	parserConcurrencyCh chan struct{}
}

type analyzeOutcomeCounters struct {
	validSuccess        atomic.Uint64
	syntaxError         atomic.Uint64
	parserTimeout       atomic.Uint64
	parserSubprocess    atomic.Uint64
	parserDecode        atomic.Uint64
	parserContract      atomic.Uint64
	parserInternalError atomic.Uint64
}

type analyzeOutcomeMetricsResponse struct {
	Analyze map[string]uint64 `json:"analyze"`
	Parser  map[string]uint64 `json:"parser"`
}

type requestRuleMetricsSink struct {
	mu     sync.Mutex
	byRule map[string]engine.RuleMetrics
}

func newRequestRuleMetricsSink() *requestRuleMetricsSink {
	return &requestRuleMetricsSink{byRule: make(map[string]engine.RuleMetrics)}
}

func (s *requestRuleMetricsSink) RecordRuleMetrics(metrics engine.RuleMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()

	aggregated := s.byRule[metrics.RuleID]
	aggregated.RuleID = metrics.RuleID
	aggregated.Executions += metrics.Executions
	aggregated.IssuesEmitted += metrics.IssuesEmitted
	aggregated.TotalDurationNS += metrics.TotalDurationNS
	s.byRule[metrics.RuleID] = aggregated
}

func (s *requestRuleMetricsSink) Snapshot() []engine.RuleMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]engine.RuleMetrics, 0, len(s.byRule))
	for _, metrics := range s.byRule {
		out = append(out, metrics)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

type infoResponse struct {
	ServiceVersion   string                `json:"service_version,omitempty"`
	ParserVersion    string                `json:"parser_version,omitempty"`
	MermaidVersion   string                `json:"mermaid_version,omitempty"`
	ParserRecognized []model.DiagramType   `json:"parser_recognized"`
	LintSupported    []model.DiagramFamily `json:"lint_supported"`
}

// NewHandler creates a Handler with the given parser and engine.
// This constructor allows dependency injection for testing.
func NewHandler(p ParserInterface, e *engine.Engine) *Handler {
	return &Handler{
		parser: p,
		engine: e,
		logger: normalizeLogger(NewLogger("api")),
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

// SetMetricsHandler configures the exporter used by GET /metrics.
func (h *Handler) SetMetricsHandler(metricsHandler http.Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.metricsHandler = metricsHandler
}

// SetTelemetryMetrics configures application telemetry collectors.
func (h *Handler) SetTelemetryMetrics(metrics *telemetry.Metrics) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.telemetryMetrics = metrics
}

// SetServiceVersion configures a service/app version for informational endpoints.
func (h *Handler) SetServiceVersion(version string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.serviceVersion = strings.TrimSpace(version)
}

// SetLogger configures structured logging for API handlers.
func (h *Handler) SetLogger(logger Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logger = normalizeLogger(logger)
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

// SetStrictConfigSchemaForTesting toggles strict config schema enforcement.
// It is intended for tests that need to validate future rejection behavior.
func SetStrictConfigSchemaForTesting(strict bool) {
	strictConfigSchema = strict
}

// RegisterRoutes attaches all routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Canonical versioned API routes.
	mux.HandleFunc("GET /v1/healthz", h.Healthz)
	mux.HandleFunc("GET /v1/ready", h.Ready)
	mux.HandleFunc("GET /v1/info", h.Info)
	mux.HandleFunc("GET /v1/metrics", h.Metrics)
	mux.HandleFunc("GET /v1/internal/metrics", h.InternalMetrics)
	mux.HandleFunc("GET /v1/rules", h.ListRules)
	mux.HandleFunc("GET /v1/rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("GET /v1/diagram-types", h.DiagramTypes)
	mux.HandleFunc("POST /v1/analyze", h.Analyze)
	mux.HandleFunc("POST /v1/analyze/sarif", h.AnalyzeSARIF)
	mux.HandleFunc("GET /v1/spec", h.ServeSpec)
	mux.HandleFunc("GET /v1/docs", h.ServeSwagger)

	// Legacy unversioned compatibility aliases.
	mux.HandleFunc("GET /health", h.Healthz)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("GET /info", h.Info)
	mux.HandleFunc("GET /metrics", h.Metrics)
	mux.HandleFunc("GET /internal/metrics", h.InternalMetrics)
	mux.HandleFunc("GET /rules", h.ListRules)
	mux.HandleFunc("GET /rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("GET /diagram-types", h.DiagramTypes)
	mux.HandleFunc("POST /analyze", h.Analyze)
	mux.HandleFunc("POST /analyze/sarif", h.AnalyzeSARIF)
	mux.HandleFunc("GET /spec", h.ServeSpec)
	mux.HandleFunc("GET /docs", h.ServeSwagger)
}

// InternalMetrics handles GET /internal/metrics and returns analyze outcome counters.
func (h *Handler) InternalMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, analyzeOutcomeMetricsResponse{
		Analyze: map[string]uint64{
			"valid_success": h.analyzeCounters.validSuccess.Load(),
			"syntax_error":  h.analyzeCounters.syntaxError.Load(),
		},
		Parser: map[string]uint64{
			"timeout":    h.analyzeCounters.parserTimeout.Load(),
			"subprocess": h.analyzeCounters.parserSubprocess.Load(),
			"decode":     h.analyzeCounters.parserDecode.Load(),
			"contract":   h.analyzeCounters.parserContract.Load(),
			"internal":   h.analyzeCounters.parserInternalError.Load(),
		},
	})
}

func (h *Handler) incrementAnalyzeOutcomeCounter(outcome string) {
	switch outcome {
	case telemetry.OutcomeLintSuccess:
		h.analyzeCounters.validSuccess.Add(1)
	case telemetry.OutcomeSyntaxError:
		h.analyzeCounters.syntaxError.Add(1)
	case telemetry.OutcomeParserTimeout:
		h.analyzeCounters.parserTimeout.Add(1)
	case telemetry.OutcomeParserSubprocessErr:
		h.analyzeCounters.parserSubprocess.Add(1)
	case telemetry.OutcomeParserDecodeErr:
		h.analyzeCounters.parserDecode.Add(1)
	case telemetry.OutcomeParserContractErr:
		h.analyzeCounters.parserContract.Add(1)
	case telemetry.OutcomeInternalError:
		h.analyzeCounters.parserInternalError.Add(1)
	}
}

// Metrics handles GET /metrics and serves exporter output when configured.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	metricsHandler := h.metricsHandler
	h.mu.RUnlock()

	if metricsHandler == nil {
		writeError(w, http.StatusNotImplemented, "metrics_not_configured", "metrics exporter is not configured")
		return
	}

	metricsHandler.ServeHTTP(w, r)
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

// DiagramTypes handles GET /diagram-types.
func (h *Handler) DiagramTypes(w http.ResponseWriter, _ *http.Request) {
	families := h.lintSupportedFamilies()
	writeJSON(w, http.StatusOK, diagramTypesResponse{
		ParserRecognized: model.RecognizedDiagramTypes(),
		LintSupported:    families,
	})
}

func (h *Handler) lintSupportedFamilies() []model.DiagramFamily {
	if h.engine == nil {
		return []model.DiagramFamily{}
	}
	return h.engine.DiagramFamilies()
}

// Healthz handles GET /healthz and reports process liveness.
func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready handles GET /ready and reports dependency readiness.
func (h *Handler) Ready(w http.ResponseWriter, _ *http.Request) {
	if checker, ok := h.parser.(ReadinessChecker); ok {
		if err := checker.Ready(); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"error": &apiErrorDetails{
					Code:    "not_ready",
					Message: err.Error(),
				},
			})
			return
		}
	}

	resp := map[string]string{"status": "ready"}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			resp["parser_version"] = info.ParserVersion
			resp["mermaid_version"] = info.MermaidVersion
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// Info handles GET /info and returns service/parser capability metadata.
func (h *Handler) Info(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	serviceVersion := h.serviceVersion
	h.mu.RUnlock()

	resp := infoResponse{
		ServiceVersion:   serviceVersion,
		ParserRecognized: model.RecognizedDiagramTypes(),
		LintSupported:    h.lintSupportedFamilies(),
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			resp.ParserVersion = info.ParserVersion
			resp.MermaidVersion = info.MermaidVersion
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// Analyze handles POST /analyze.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	h.analyzeWithCallback(w, r, func(resp analyzeResponse) {
		writeJSON(w, http.StatusOK, resp)
	})
}

func (h *Handler) analyzeWithCallback(w http.ResponseWriter, r *http.Request, onValid func(resp analyzeResponse)) {
	observeAnalyzeOutcome := func(outcome string) {
		h.mu.RLock()
		metrics := h.telemetryMetrics
		h.mu.RUnlock()
		if metrics != nil {
			metrics.ObserveAnalyzeOutcome(outcome)
		}
		h.incrementAnalyzeOutcomeCounter(outcome)
	}

	h.mu.RLock()
	logger := normalizeLogger(h.logger)
	h.mu.RUnlock()

	r.Body = http.MaxBytesReader(w, r.Body, maxAnalyzeBodyBytes)

	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			observeAnalyzeOutcome("request_too_large")
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 1 MiB limit")
			return
		}
		observeAnalyzeOutcome("invalid_json")
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON body")
		return
	}
	if req.Code == "" {
		observeAnalyzeOutcome("missing_code")
		writeError(w, http.StatusBadRequest, "missing_code", "field 'code' is required")
		return
	}

	cfg, deprecationWarnings, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs(), strictConfigSchema)
	if configValidationErr != nil {
		observeAnalyzeOutcome(configValidationErr.Code)
		writeConfigValidationError(w, configValidationErr)
		return
	}

	deprecationWarnings = uniqueStrings(deprecationWarnings)
	if len(deprecationWarnings) > 0 {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Warning", legacyConfigDeprecationHeader)
	}

	normalizedCfg, err := h.engine.NormalizeConfig(cfg)
	if err != nil {
		observeAnalyzeOutcome("invalid_config")
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
			observeAnalyzeOutcome("server_busy")
			writeError(w, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again")
			return
		}
	}

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parser.Parse(req.Code)
	parseDuration := time.Since(parseStart)
	if err != nil {
		outcome := parserFailureOutcome(err)
		h.mu.RLock()
		metrics := h.telemetryMetrics
		h.mu.RUnlock()
		if metrics != nil {
			metrics.ObserveParserDuration(outcome, parseDuration)
		}
		observeAnalyzeOutcome(outcome)
		setAnalyzeLogFields(r.Context(), outcome, string(model.DiagramTypeUnknown))
		logger.Error("parser failure", "request_id", RequestIDFromContext(r.Context()), "route", r.URL.Path, "method", r.Method, "parser_outcome", outcome, "error", err.Error())
		writeParserFailure(w, r.Context(), logger, err)
		return
	}

	if syntaxErr != nil {
		h.mu.RLock()
		metrics := h.telemetryMetrics
		h.mu.RUnlock()
		if metrics != nil {
			metrics.ObserveParserDuration(telemetry.OutcomeSyntaxError, parseDuration)
		}
		observeAnalyzeOutcome(telemetry.OutcomeSyntaxError)
		diagramType := defaultDiagramTypeForSyntaxError(req.Code)
		setAnalyzeLogFields(r.Context(), telemetry.OutcomeSyntaxError, string(diagramType))
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagramType,
			LintSupported: diagramType.Family() == model.DiagramFamilyFlowchart,
			Warnings:      deprecationWarnings,
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
		h.mu.RLock()
		metrics := h.telemetryMetrics
		h.mu.RUnlock()
		if metrics != nil {
			metrics.ObserveParserDuration(telemetry.OutcomeInternalError, parseDuration)
		}
		observeAnalyzeOutcome(telemetry.OutcomeInternalError)
		setAnalyzeLogFields(r.Context(), telemetry.OutcomeInternalError, string(model.DiagramTypeUnknown))
		logger.Error("parser returned nil diagram", "request_id", RequestIDFromContext(r.Context()), "route", r.URL.Path, "method", r.Method)
		writeError(w, http.StatusInternalServerError, "internal_error", "parser returned nil diagram")
		return
	}

	h.mu.RLock()
	metrics := h.telemetryMetrics
	h.mu.RUnlock()
	if metrics != nil {
		metrics.ObserveParserDuration(telemetry.OutcomeLintSuccess, parseDuration)
	}

	family := diagram.Type.Family()
	if family != model.DiagramFamilyFlowchart {
		observeAnalyzeOutcome("unsupported_diagram_type")
		setAnalyzeLogFields(r.Context(), "unsupported_diagram_type", string(diagram.Type))
		// Keep metrics in the response for parsed diagrams, even when linting is
		// not currently supported for that Mermaid family.
		unsupportedIssue := model.Issue{
			RuleID:   "unsupported-diagram-type",
			Severity: "info",
			Message:  "diagram type \"" + string(diagram.Type) + "\" is parsed but lint rules are not available yet",
		}
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagram.Type,
			LintSupported: false,
			SyntaxError:   nil,
			Issues:        []model.Issue{unsupportedIssue},
			Warnings:      deprecationWarnings,
			Error: &apiErrorDetails{
				Code:    "unsupported_diagram_type",
				Message: "diagram type is parsed but linting is not supported",
			},
			Metrics: computeMetrics(diagram, []model.Issue{unsupportedIssue}),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	ruleMetricsSink := newRequestRuleMetricsSink()
	issues := h.engine.RunWithInstrumentation(diagram, normalizedCfg, ruleMetricsSink)
	for _, ruleMetrics := range ruleMetricsSink.Snapshot() {
		logger.Info(
			"engine rule metrics",
			"request_id", RequestIDFromContext(r.Context()),
			"rule_id", ruleMetrics.RuleID,
			"executions", ruleMetrics.Executions,
			"issues_emitted", ruleMetrics.IssuesEmitted,
			"total_duration_ns", ruleMetrics.TotalDurationNS,
		)
	}
	observeAnalyzeOutcome(telemetry.OutcomeLintSuccess)
	setAnalyzeLogFields(r.Context(), telemetry.OutcomeLintSuccess, string(diagram.Type))

	resp := analyzeResponse{
		Valid:         true,
		DiagramType:   diagram.Type,
		LintSupported: diagram.Type.Family() == model.DiagramFamilyFlowchart,
		SyntaxError:   nil,
		Issues:        issues,
		Warnings:      deprecationWarnings,
		Metrics:       computeMetrics(diagram, issues),
	}
	onValid(resp)
}

// AnalyzeSARIF handles POST /analyze/sarif.
func (h *Handler) AnalyzeSARIF(w http.ResponseWriter, r *http.Request) {
	h.analyzeWithCallback(w, r, func(resp analyzeResponse) {
		requestURI := "/analyze/sarif"
		if r.URL != nil {
			requestURI = r.URL.Path
		}
		report := sarif.Transform(resp.Issues, sarif.RequestMetadata{
			RequestURI:  requestURI,
			ArtifactURI: "",
		})
		writeSARIF(w, http.StatusOK, report)
	})
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
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
	details := map[string]any{}
	if configValidationErr.Path != "" {
		details["path"] = configValidationErr.Path
	}
	if len(configValidationErr.Supported) > 0 {
		details["supported"] = configValidationErr.Supported
	}
	if len(details) == 0 {
		details = nil
	}

	writeJSON(w, http.StatusBadRequest, analyzeResponse{
		Valid: false,
		Error: &apiErrorDetails{
			Code:    configValidationErr.Code,
			Message: configValidationErr.Message,
			Details: details,
		},
		LintSupported: false,
		SyntaxError:   nil,
		Issues:        []model.Issue{},
		Metrics:       defaultMetrics(model.DiagramTypeUnknown),
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

	data, _ := json.MarshalIndent(OpenAPISpec(), "", "  ")
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

func writeSARIF(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/sarif+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		SyntaxError:   nil,
		Issues:        []model.Issue{},
		Metrics:       defaultMetrics(model.DiagramTypeUnknown),
		Error: &apiErrorDetails{
			Code:    code,
			Message: message,
		},
	})
}

func writeParserFailure(w http.ResponseWriter, ctx context.Context, logger Logger, err error) {
	requestID := RequestIDFromContext(ctx)
	logger = normalizeLogger(logger)
	switch {
	case errors.Is(err, parser.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserTimeout, "error", err.Error())
		writeError(w, http.StatusGatewayTimeout, "parser_timeout", "parser timed out while validating Mermaid code")
	case errors.Is(err, parser.ErrSubprocess):
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserSubprocessErr, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "parser_subprocess_error", "parser subprocess failed")
	case errors.Is(err, parser.ErrDecode):
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserDecodeErr, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "parser_decode_error", "parser returned malformed output")
	case errors.Is(err, parser.ErrContract):
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserContractErr, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "parser_contract_violation", "parser response violated service contract")
	default:
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeInternalError, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func parserFailureOutcome(err error) string {
	switch {
	case errors.Is(err, parser.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return telemetry.OutcomeParserTimeout
	case errors.Is(err, parser.ErrSubprocess):
		return telemetry.OutcomeParserSubprocessErr
	case errors.Is(err, parser.ErrDecode):
		return telemetry.OutcomeParserDecodeErr
	case errors.Is(err, parser.ErrContract):
		return telemetry.OutcomeParserContractErr
	default:
		return telemetry.OutcomeInternalError
	}
}
