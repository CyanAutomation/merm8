// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
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
// serverBusyRetryAfterSeconds defines the stable API contract for 503 server_busy
// responses on analyze endpoints. Clients should combine this floor with
// jittered exponential backoff to avoid synchronized retries.
const serverBusyRetryAfterSeconds = 1

const (
	legacyAnalyzeSunsetHeader     = "Tue, 30 Jun 2026 23:59:59 GMT"
	legacyAnalyzeSuccessorDocLink = `</v1/docs#/Linting/post_v1_analyze>; rel="successor-version"`
)

const (
	legacySchemaVersionWarningMessage = `legacy key config.schema_version is deprecated; use config.schema-version. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyUnversionedRulesWarning     = `legacy unversioned config shape is deprecated; add config.schema-version and keep rules under config.rules. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyFlatConfigWarning           = `legacy flat config shape is deprecated; move rule settings under config.rules and add config.schema-version. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyOptionKeyWarningTemplate    = `legacy key config.%s.%s is deprecated; use config.%s.%s. Example: {"%s": ["node:A"]}`
)

var strictConfigSchema = false

var errInvalidRequest = errors.New("invalid request")

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

// TimeoutProvider can be implemented by parser dependencies that expose configured timeout.
type TimeoutProvider interface {
	Timeout() time.Duration
}

// ReadinessChecker can be implemented by parser dependencies that support
// lightweight readiness validation (e.g., binary/script availability checks).
type ReadinessChecker interface {
	Ready() error
}

// ParserWithConfig can parse Mermaid with per-request execution settings.
type ParserWithConfig interface {
	ParseWithConfig(string, parser.Config) (*model.Diagram, *parser.SyntaxError, error)
}

// analyzeRequest is the JSON body accepted by POST /analyze.
type analyzeRequest struct {
	Code   string                 `json:"code"`
	Config json.RawMessage        `json:"config"`
	Parser *analyzeParserSettings `json:"parser,omitempty"`
}

type analyzeParserSettings struct {
	TimeoutSeconds *int `json:"timeout_seconds,omitempty"`
	MaxOldSpaceMB  *int `json:"max_old_space_mb,omitempty"`
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
		deprecations = append(deprecations, legacySchemaVersionWarningMessage)
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
		deprecations = append(deprecations, legacyUnversionedRulesWarning)
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
		deprecations = append(deprecations, legacyFlatConfigWarning)
	}

	registryByRuleID := rules.ConfigRegistryForRuleIDs(knownRuleIDs)

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

		registry, ok := registryByRuleID[ruleID]
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
			if optionKey != canonicalOptionKey {
				deprecations = append(deprecations, fmt.Sprintf(legacyOptionKeyWarningTemplate, ruleID, optionKey, ruleID, canonicalOptionKey, canonicalOptionKey))
			}
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

	// Validate suppression selectors reference known rules
	for _, ruleConfig := range cfg {
		if suppSelectors, hasSuppression := ruleConfig["suppression-selectors"]; hasSuppression {
			// Convert interface{} to []string if possible
			if selectorArray, ok := suppSelectors.([]interface{}); ok {
				selectors := make([]string, 0, len(selectorArray))
				for _, sel := range selectorArray {
					if selStr, ok := sel.(string); ok {
						selectors = append(selectors, selStr)
					}
				}
				warnings := rules.ValidateSuppressionSelectors(selectors, knownRuleIDs)
				deprecations = append(deprecations, warnings...)
			}
		}
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

// readBody reads the entire request body into a byte slice.
func readBody(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return io.ReadAll(body)
}

// parseRawMermaidInput attempts to parse JSON first, falling back to treating the entire input as raw mermaid code.
func parseRawMermaidInput(body []byte) (string, error) {
	var req analyzeRequest
	if err := json.Unmarshal(body, &req); err == nil && req.Code != "" {
		return req.Code, nil
	}
	// Not JSON or missing code field - treat entire body as raw mermaid code
	return string(body), nil
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
type responseWarning struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Replacement string `json:"replacement"`
}

type responseMeta struct {
	Warnings []responseWarning `json:"warnings,omitempty"`
}

type analyzeResponse struct {
	Valid         bool                 `json:"valid"`
	DiagramType   model.DiagramType    `json:"diagram-type,omitempty"`
	LintSupported bool                 `json:"lint-supported"`
	SyntaxError   *syntaxErrorResponse `json:"syntax-error"`
	Issues        []model.Issue        `json:"issues"`
	Suggestions   []string             `json:"suggestions,omitempty"`
	Warnings      []string             `json:"warnings,omitempty"`
	Meta          *responseMeta        `json:"meta,omitempty"`
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
	State               string                 `json:"state"`
	Availability        string                 `json:"availability,omitempty"`
	Severity            string                 `json:"severity"`
	Description         string                 `json:"description"`
	DefaultConfig       map[string]interface{} `json:"default-config"`
	ConfigurableOptions []ruleOptionResponse   `json:"configurable-options"`
	DiagramExamples     []string               `json:"diagram-examples,omitempty"`
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
	buildCommit         string
	buildTime           string
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
	ServiceVersion       string                `json:"service-version,omitempty"`
	ParserVersion        string                `json:"parser-version,omitempty"`
	MermaidVersion       string                `json:"mermaid-version,omitempty"`
	ParserTimeoutSeconds int                   `json:"parser-timeout-seconds,omitempty"`
	ParserRecognized     []model.DiagramType   `json:"parser-recognized"`
	LintSupported        []model.DiagramFamily `json:"lint-supported"`
	SupportedRules       []string              `json:"supported-rules"`
	SupportedRuleIDs     []string              `json:"supported-rule-ids"`
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

// SetBuildMetadata configures build metadata for informational endpoints.
func (h *Handler) SetBuildMetadata(commit string, buildTime string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buildCommit = strings.TrimSpace(commit)
	h.buildTime = strings.TrimSpace(buildTime)
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

// SetStrictConfigSchema toggles strict config schema enforcement.
// When strict mode is enabled, legacy config formats are rejected.
// Default is false for v1.0 compatibility; should be true for production deployments.
func SetStrictConfigSchema(strict bool) {
	strictConfigSchema = strict
}

// SetStrictConfigSchemaForTesting is a deprecated alias for SetStrictConfigSchema.
// It is kept for backward compatibility with existing tests.
func SetStrictConfigSchemaForTesting(strict bool) {
	SetStrictConfigSchema(strict)
}

// RegisterRoutes attaches all routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Canonical versioned API routes.
	mux.HandleFunc("GET /v1/healthz", h.Healthz)
	mux.HandleFunc("GET /v1/health", h.Healthz)
	mux.HandleFunc("GET /v1/ready", h.Ready)
	mux.HandleFunc("GET /v1/info", h.Info)
	mux.HandleFunc("GET /v1/metrics", h.Metrics)
	mux.HandleFunc("GET /v1/internal/metrics", h.InternalMetrics)
	mux.HandleFunc("GET /v1/rules", h.ListRules)
	mux.HandleFunc("GET /v1/rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("GET /v1/diagram-types", h.DiagramTypes)
	mux.HandleFunc("GET /v1/analyze/help", h.AnalyzeHelp)
	mux.HandleFunc("POST /v1/analyze", h.Analyze)
	mux.HandleFunc("POST /v1/analyze/raw", h.AnalyzeRaw)
	mux.HandleFunc("POST /v1/analyze/sarif", h.AnalyzeSARIF)
	mux.HandleFunc("GET /v1/spec", h.ServeSpec)
	mux.HandleFunc("GET /v1/docs", h.ServeSwagger)

	// Legacy unversioned compatibility aliases (including probe-friendly root path).
	mux.HandleFunc("GET /", h.Healthz)
	mux.HandleFunc("GET /health", h.Healthz)
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.HandleFunc("GET /ready", h.Ready)
	mux.HandleFunc("GET /v1/version", h.Version)
	mux.HandleFunc("GET /version", h.Version)
	mux.HandleFunc("GET /info", h.Info)
	mux.HandleFunc("GET /metrics", h.Metrics)
	mux.HandleFunc("GET /internal/metrics", h.InternalMetrics)
	mux.HandleFunc("GET /rules", h.ListRules)
	mux.HandleFunc("GET /rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("GET /diagram-types", h.DiagramTypes)
	mux.HandleFunc("GET /analyze/help", h.AnalyzeHelp)
	mux.HandleFunc("POST /analyze", h.Analyze)
	mux.HandleFunc("POST /analyze/raw", h.AnalyzeRaw)
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

func getRuleExamples(ruleID string) []string {
	// Return example diagrams that trigger violations for each rule.
	examples := map[string][]string{
		"core/no-duplicate-node-ids": {
			"graph TD\n  A[Node A]\n  B[Another Node]\n  A[Duplicate ID]\n  A --> B",
		},
		"core/no-disconnected-nodes": {
			"graph TD\n  A[Connected] --> B[Also Connected]\n  C[Lonely Node]\n  D[Orphaned]",
		},
		"core/max-fanout": {
			"graph TD\n  Root[Hub]\n  Root --> A[Branch 1]\n  Root --> B[Branch 2]\n  Root --> C[Branch 3]\n  Root --> D[Branch 4]\n  Root --> E[Branch 5]",
		},
		"core/max-depth": {
			"graph TD\n  A[Level 1] --> B[Level 2]\n  B --> C[Level 3]\n  C --> D[Level 4]\n  D --> E[Level 5]\n  E --> F[Level 6]",
		},
		"core/no-cycles": {
			"graph TD\n  A[Start] --> B[Middle]\n  B --> C[End]\n  C --> A",
		},
	}

	if diagrams, exists := examples[ruleID]; exists {
		return diagrams
	}
	return []string{}
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
			State:               rule.State,
			Availability:        rule.Availability,
			Severity:            rule.Severity,
			Description:         rule.Description,
			DefaultConfig:       rule.DefaultConfig,
			ConfigurableOptions: options,
			DiagramExamples:     getRuleExamples(rule.ID),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": resp})
}

// RuleConfigSchema handles GET /rules/schema.
func (h *Handler) RuleConfigSchema(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"schema": rules.ConfigJSONSchemaForRuleIDs(h.engine.KnownRuleIDs())})
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

func (h *Handler) isLintSupported(family model.DiagramFamily) bool {
	for _, f := range h.lintSupportedFamilies() {
		if f == family {
			return true
		}
	}
	return false
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
		SupportedRules:   supportedRuleIDs(),
		SupportedRuleIDs: h.supportedRuleIDs(),
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			resp.ParserVersion = info.ParserVersion
			resp.MermaidVersion = info.MermaidVersion
		}
	}
	// Retrieve parser timeout if available
	if timeoutProvider, ok := h.parser.(TimeoutProvider); ok {
		resp.ParserTimeoutSeconds = int(timeoutProvider.Timeout().Seconds())
	}

	writeJSON(w, http.StatusOK, resp)
}

func supportedRuleIDs() []string {
	metadata := rules.ListRuleMetadata()
	ruleIDs := make([]string, 0, len(metadata))
	for _, rule := range metadata {
		if rule.State != "implemented" {
			continue
		}
		ruleIDs = append(ruleIDs, rule.ID)
	}
	return ruleIDs
}

func (h *Handler) supportedRuleIDs() []string {
	if h.engine == nil {
		return []string{}
	}

	known := h.engine.KnownRuleIDs()
	ruleIDs := make([]string, 0, len(known))
	for ruleID := range known {
		ruleIDs = append(ruleIDs, ruleID)
	}
	sort.Strings(ruleIDs)
	return ruleIDs
}

// Version handles GET /version and returns app/build version metadata.
func (h *Handler) Version(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	serviceVersion := h.serviceVersion
	buildCommit := h.buildCommit
	buildTime := h.buildTime
	h.mu.RUnlock()

	resp := map[string]string{}
	if serviceVersion != "" {
		resp["version"] = serviceVersion
	}
	if buildCommit != "" {
		resp["build_commit"] = buildCommit
	}
	if buildTime != "" {
		resp["build_time"] = buildTime
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			if info.ParserVersion != "" {
				resp["parser_version"] = info.ParserVersion
			}
			if info.MermaidVersion != "" {
				resp["mermaid_version"] = info.MermaidVersion
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// AnalyzeHelp handles GET /analyze/help and returns diagram templates and common error guidance.
func (h *Handler) AnalyzeHelp(w http.ResponseWriter, r *http.Request) {
	helpResponse := map[string]any{
		"diagram-types": map[string]map[string]string{
			"flowchart": {
				"description": "Directed acyclic graph for processes, workflows, and decision trees",
				"example":     "flowchart TD\n    Start([Start]) --> Process[Do Something]\n    Process --> End([End])",
			},
			"sequence": {
				"description": "Interactions between participants over time",
				"example":     "sequenceDiagram\n    Alice->>Bob: Hello!\n    Bob-->>Alice: Hi there!",
			},
			"class": {
				"description": "Object-oriented class hierarchy and relationships",
				"example":     "classDiagram\n    class Animal {\n        +String name\n        +eat()\n    }",
			},
			"er": {
				"description": "Entity-relationship diagrams for data models",
				"example":     "erDiagram\n    CUSTOMER ||--o{ ORDER : places\n    ORDER ||--|{ ITEM : contains",
			},
			"state": {
				"description": "State machines and workflows with state transitions",
				"example":     "stateDiagram-v2\n    [*] --> Active\n    Active --> Inactive\n    Inactive --> [*]",
			},
		},
		"common-errors": []map[string]string{
			{
				"pattern": "No diagram type detected",
				"fix":     "Start your diagram with a type keyword: flowchart, sequenceDiagram, classDiagram, erDiagram, or stateDiagram-v2",
				"example": "flowchart TD\n  A[Start] --> B[End]",
			},
			{
				"pattern": "Looks like Graphviz syntax",
				"fix":     "Use Mermaid syntax instead. Replace 'digraph' with 'flowchart TD' and '->' with '-->'",
				"example": "flowchart TD\n  A --> B",
			},
			{
				"pattern": "Unexpected token",
				"fix":     "Check syntax: correct arrow operators, bracket matching, and indentation (use spaces, not tabs)",
				"example": "flowchart TD\n  A[Valid Label] --> B[Another]",
			},
			{
				"pattern": "Tab indentation detected",
				"fix":     "Replace tabs with spaces (2-4 spaces per indentation level)",
				"example": "flowchart TD\n    A --> B",
			},
		},
		"arrow-syntax": map[string]string{
			"flowchart": "-->, -..-, -.->, or ===",
			"sequence":  "->, -->, ->>, ->>",
			"class":     "<|--, *--, o--",
			"er":        "||, |o, o|, ||",
		},
		"resources": map[string]string{
			"documentation": "https://mermaid.js.org/intro/",
			"syntax-guide":  "https://mermaid.js.org/syntax/flowchart.html",
		},
	}

	writeJSON(w, http.StatusOK, helpResponse)
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
	emitLegacyConfigWarnings(r.Context(), logger, w, deprecationWarnings)

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
			setServerBusyRetryAfterHeader(w)
			writeError(w, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again")
			return
		}
	}

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			writeError(w, http.StatusBadRequest, "invalid_option", strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "))
			return
		}
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
		suggestions := suggestionsForSyntaxError(syntaxErr, req.Code)
		// On syntax errors, only set lint-supported=true if we detected a diagram type
		// AND that type's family supports linting. If no type detected, always false.
		var lintSupported bool
		if diagramType == model.DiagramTypeUnknown {
			lintSupported = false
		} else {
			lintSupported = h.isLintSupported(diagramType.Family())
		}
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagramType,
			LintSupported: lintSupported,
			Suggestions:   suggestions,
			Warnings:      deprecationWarnings,
			Meta:          responseMetaForWarnings(deprecationWarnings),
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
			Meta:          responseMetaForWarnings(deprecationWarnings),
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
		// Record rule metrics to application telemetry (Prometheus)
		h.mu.RLock()
		telemetry := h.telemetryMetrics
		h.mu.RUnlock()
		if telemetry != nil {
			telemetry.RecordRuleMetrics(ruleMetrics)
		}
	}
	observeAnalyzeOutcome(telemetry.OutcomeLintSuccess)
	setAnalyzeLogFields(r.Context(), telemetry.OutcomeLintSuccess, string(diagram.Type))

	resp := analyzeResponse{
		Valid:         true,
		DiagramType:   diagram.Type,
		LintSupported: h.isLintSupported(diagram.Type.Family()),
		SyntaxError:   nil,
		Issues:        issues,
		Warnings:      deprecationWarnings,
		Meta:          responseMetaForWarnings(deprecationWarnings),
		Metrics:       computeMetrics(diagram, issues),
	}
	onValid(resp)
}

// AnalyzeRaw handles POST /analyze/raw.
// Accepts raw mermaid code (plain text) directly in the request body.
// Auto-detects format: tries JSON with "code" field first, falls back to treating body as raw mermaid.
// Does NOT support lint configuration; use /analyze for that.
func (h *Handler) AnalyzeRaw(w http.ResponseWriter, r *http.Request) {
	h.analyzeRawWithCallback(w, r, func(resp analyzeResponse) {
		writeJSON(w, http.StatusOK, resp)
	})
}

func (h *Handler) analyzeRawWithCallback(w http.ResponseWriter, r *http.Request, onValid func(resp analyzeResponse)) {
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

	body, err := readBody(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			observeAnalyzeOutcome("request_too_large")
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 1 MiB limit")
			return
		}
		observeAnalyzeOutcome("invalid_json")
		writeError(w, http.StatusBadRequest, "invalid_json", "failed to read request body")
		return
	}

	code, err := parseRawMermaidInput(body)
	if err != nil || code == "" {
		observeAnalyzeOutcome("missing_code")
		writeError(w, http.StatusBadRequest, "missing_code", "request body is empty or does not contain mermaid code")
		return
	}

	// Create a minimal analyzeRequest for parseWithRequestSettings
	req := analyzeRequest{Code: code}

	h.mu.RLock()
	parserConcurrencyCh := h.parserConcurrencyCh
	h.mu.RUnlock()

	if parserConcurrencyCh != nil {
		select {
		case parserConcurrencyCh <- struct{}{}:
			defer func() { <-parserConcurrencyCh }()
		default:
			observeAnalyzeOutcome("server_busy")
			setServerBusyRetryAfterHeader(w)
			writeError(w, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again")
			return
		}
	}

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			writeError(w, http.StatusBadRequest, "invalid_option", strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "))
			return
		}
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
		suggestions := suggestionsForSyntaxError(syntaxErr, req.Code)
		// On syntax errors, only set lint-supported=true if we detected a diagram type
		// AND that type's family supports linting. If no type detected, always false.
		var lintSupported bool
		if diagramType == model.DiagramTypeUnknown {
			lintSupported = false
		} else {
			lintSupported = h.isLintSupported(diagramType.Family())
		}
		resp := analyzeResponse{
			Valid:         false,
			DiagramType:   diagramType,
			LintSupported: lintSupported,
			Suggestions:   suggestions,
			Warnings:      nil,
			Meta:          nil,
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
			Warnings:      nil,
			Meta:          nil,
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
	issues := h.engine.RunWithInstrumentation(diagram, rules.Config{}, ruleMetricsSink)
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
		LintSupported: h.isLintSupported(diagram.Type.Family()),
		SyntaxError:   nil,
		Issues:        issues,
		Warnings:      nil,
		Meta:          nil,
		Metrics:       computeMetrics(diagram, issues),
	}
	onValid(resp)
}

// AnalyzeSARIF handles POST /analyze/sarif.
// Differs from Analyze in that all responses (including errors) are returned
// in SARIF 2.1.0 format with appropriate HTTP status codes.
func (h *Handler) AnalyzeSARIF(w http.ResponseWriter, r *http.Request) {
	analyzeForSARIF(w, r, h)
}

// analyzeForSARIF is the SARIF-specific analyzer that mirrors analyzeWithCallback
// but returns SARIF-formatted errors instead of JSON errors.
func analyzeForSARIF(w http.ResponseWriter, r *http.Request, h *Handler) {
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

	requestURI := "/analyze/sarif"
	if r.URL != nil {
		requestURI = r.URL.Path
	}
	meta := sarif.RequestMetadata{
		RequestURI:  requestURI,
		ArtifactURI: "",
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAnalyzeBodyBytes)

	var req analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			observeAnalyzeOutcome("request_too_large")
			report := sarif.TransformError(sarif.ErrorInfo{
				Code:    "request_too_large",
				Message: "request body exceeds 1 MiB limit",
			}, meta)
			writeSARIF(w, http.StatusRequestEntityTooLarge, report)
			return
		}
		observeAnalyzeOutcome("invalid_json")
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "invalid_json",
			Message: "invalid JSON body",
		}, meta)
		writeSARIF(w, http.StatusBadRequest, report)
		return
	}
	if req.Code == "" {
		observeAnalyzeOutcome("missing_code")
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "missing_code",
			Message: "field 'code' is required",
		}, meta)
		writeSARIF(w, http.StatusBadRequest, report)
		return
	}

	cfg, deprecationWarnings, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs(), strictConfigSchema)
	if configValidationErr != nil {
		observeAnalyzeOutcome(configValidationErr.Code)
		statusCode := http.StatusBadRequest
		if configValidationErr.Code == "deprecated_config_format" {
			statusCode = http.StatusBadRequest
		}
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    configValidationErr.Code,
			Message: configValidationErr.Message,
		}, meta)
		writeSARIF(w, statusCode, report)
		return
	}

	deprecationWarnings = uniqueStrings(deprecationWarnings)
	emitLegacyConfigWarnings(r.Context(), logger, w, deprecationWarnings)

	normalizedCfg, err := h.engine.NormalizeConfig(cfg)
	if err != nil {
		observeAnalyzeOutcome("invalid_config")
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "invalid_config",
			Message: err.Error(),
		}, meta)
		writeSARIF(w, http.StatusBadRequest, report)
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
			setServerBusyRetryAfterHeader(w)
			report := sarif.TransformError(sarif.ErrorInfo{
				Code:    "server_busy",
				Message: "parser concurrency limit reached; try again",
			}, meta)
			writeSARIF(w, http.StatusServiceUnavailable, report)
			return
		}
	}

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			writeError(w, http.StatusBadRequest, "invalid_option", strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "))
			return
		}
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
		statusCode, errorCode, errorMsg := parserFailureDetails(err)
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    errorCode,
			Message: errorMsg,
		}, meta)
		writeSARIF(w, statusCode, report)
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

		// For syntax errors, include the error in SARIF format
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "syntax_error",
			Message: syntaxErr.Message,
		}, meta)
		writeSARIF(w, http.StatusOK, report)
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
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "internal_error",
			Message: "parser returned nil diagram",
		}, meta)
		writeSARIF(w, http.StatusInternalServerError, report)
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
		// For unsupported diagram types, return an error in SARIF format
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "unsupported_diagram_type",
			Message: "diagram type \"" + string(diagram.Type) + "\" is parsed but lint rules are not available yet",
		}, meta)
		writeSARIF(w, http.StatusOK, report)
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

	// For successful analysis, return SARIF with lint results
	requestURI = "/analyze/sarif"
	if r.URL != nil {
		requestURI = r.URL.Path
	}
	report := sarif.Transform(issues, sarif.RequestMetadata{
		RequestURI:  requestURI,
		ArtifactURI: "",
	})
	writeSARIF(w, http.StatusOK, report)
}

func setLegacyAnalyzeDeprecationHeaders(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.URL == nil {
		return
	}
	if !isLegacyAnalyzeAliasPath(r.URL.Path) {
		return
	}
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", legacyAnalyzeSunsetHeader)
	w.Header().Set("Link", legacyAnalyzeSuccessorDocLink)
}

func isLegacyAnalyzeAliasPath(path string) bool {
	return path == "/analyze" || strings.HasPrefix(path, "/analyze/")
}

func emitLegacyConfigWarnings(ctx context.Context, logger Logger, w http.ResponseWriter, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	w.Header().Set("Deprecation", "true")
	for _, warning := range warnings {
		w.Header().Add("Warning", "299 - \""+warning+"\"")
	}
	for _, warning := range warnings {
		logger.Warn(
			"legacy config format received",
			"request_id", RequestIDFromContext(ctx),
			"warning", warning,
		)
	}
}

func responseMetaForWarnings(warnings []string) *responseMeta {
	structured := buildResponseWarnings(warnings)
	if len(structured) == 0 {
		return nil
	}
	return &responseMeta{Warnings: structured}
}

func buildResponseWarnings(warnings []string) []responseWarning {
	if len(warnings) == 0 {
		return nil
	}
	respWarnings := make([]responseWarning, 0, len(warnings))
	for _, warning := range warnings {
		replacement := ""
		if parts := strings.SplitN(warning, "Example: ", 2); len(parts) == 2 {
			replacement = strings.TrimSpace(parts[1])
		}
		respWarnings = append(respWarnings, responseWarning{
			Code:        "legacy_config_format",
			Message:     warning,
			Replacement: replacement,
		})
	}
	return respWarnings
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

// suggestionsForSyntaxError analyzes a syntax error and code to provide smart, actionable hints
func suggestionsForSyntaxError(syntaxErr *parser.SyntaxError, code string) []string {
	suggestions := []string{}

	// Detect Graphviz syntax
	if strings.Contains(code, "digraph") || strings.Contains(code, "rankdir") {
		suggestions = append(suggestions, "This looks like Graphviz syntax. Use Mermaid syntax instead: 'flowchart TD' for directed graphs.")
	}

	// Detect YAML frontmatter
	if strings.HasPrefix(strings.TrimSpace(code), "---") {
		suggestions = append(suggestions, "Remove YAML frontmatter (---); Mermaid code should start directly with the diagram type.")
	}

	// Detect tabs instead of spaces
	if strings.Contains(code, "\t") {
		suggestions = append(suggestions, "Replace tabs with spaces (Mermaid uses space indentation).")
	}

	// Detect arrow syntax issues based on error message and code content
	if strings.Contains(code, "->") && !strings.Contains(code, "-->") {
		firstLine := strings.TrimSpace(strings.SplitN(code, "\n", 2)[0])
		if strings.HasPrefix(firstLine, "flowchart") || strings.HasPrefix(firstLine, "graph") {
			suggestions = append(suggestions, "Use '-->' for flowchart connections, not '->'.")
		}
	}

	// Detect missing diagram type keyword
	if strings.Contains(syntaxErr.Message, "No diagram type") || strings.Contains(syntaxErr.Message, "Unexpected") {
		firstLine := strings.TrimSpace(strings.SplitN(code, "\n", 2)[0])
		if !strings.Contains(firstLine, "flowchart") && !strings.Contains(firstLine, "sequenceDiagram") &&
			!strings.Contains(firstLine, "classDiagram") && !strings.Contains(firstLine, "erDiagram") &&
			!strings.Contains(firstLine, "stateDiagram") && !strings.Contains(firstLine, "graph") {
			suggestions = append(suggestions, "Start your diagram with a type keyword: 'flowchart TD', 'sequenceDiagram', 'classDiagram', 'erDiagram', or 'stateDiagram-v2'.")
		}
	}

	return suggestions
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

func setServerBusyRetryAfterHeader(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.Itoa(serverBusyRetryAfterSeconds))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorWithDetails(w, status, code, message, nil)
}

func writeErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		SyntaxError:   nil,
		Issues:        []model.Issue{},
		Metrics:       defaultMetrics(model.DiagramTypeUnknown),
		Error: &apiErrorDetails{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

func (h *Handler) parseWithRequestSettings(req analyzeRequest) (*model.Diagram, *parser.SyntaxError, error) {
	cfg := parser.ConfigFromEnv().EffectiveConfig()
	if req.Parser == nil {
		return h.parser.Parse(req.Code)
	}
	minTimeout, maxTimeout, minMem, maxMem := parser.LimitBounds()
	if req.Parser.TimeoutSeconds != nil {
		timeout := time.Duration(*req.Parser.TimeoutSeconds) * time.Second
		if timeout < minTimeout || timeout > maxTimeout {
			return nil, nil, fmt.Errorf("%w: parser.timeout_seconds must be between %d and %d", errInvalidRequest, int(minTimeout.Seconds()), int(maxTimeout.Seconds()))
		}
		cfg.Timeout = timeout
	}
	if req.Parser.MaxOldSpaceMB != nil {
		if *req.Parser.MaxOldSpaceMB < minMem || *req.Parser.MaxOldSpaceMB > maxMem {
			return nil, nil, fmt.Errorf("%w: parser.max_old_space_mb must be between %d and %d", errInvalidRequest, minMem, maxMem)
		}
		cfg.NodeMaxOldSpaceMB = *req.Parser.MaxOldSpaceMB
	}
	if parserWithConfig, ok := h.parser.(ParserWithConfig); ok {
		return parserWithConfig.ParseWithConfig(req.Code, cfg)
	}
	return h.parser.Parse(req.Code)
}

func writeParserFailure(w http.ResponseWriter, ctx context.Context, logger Logger, err error) {
	requestID := RequestIDFromContext(ctx)
	logger = normalizeLogger(logger)
	details := parserErrorDetails(err)
	switch details.code {
	case "parser_timeout":
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserTimeout, "error", err.Error())
	case "parser_memory_limit":
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserSubprocessErr, "error", err.Error())
	case "parser_subprocess_error":
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserSubprocessErr, "error", err.Error())
	case "parser_decode_error":
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserDecodeErr, "error", err.Error())
	case "parser_contract_violation":
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeParserContractErr, "error", err.Error())
	default:
		logger.Error("write parser failure response", "request_id", requestID, "parser_outcome", telemetry.OutcomeInternalError, "error", err.Error())
	}
	writeErrorWithDetails(w, details.statusCode, details.code, details.message, details.details)
}

func parserFailureOutcome(err error) string {
	switch {
	case errors.Is(err, parser.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return telemetry.OutcomeParserTimeout
	case errors.Is(err, parser.ErrSubprocess), errors.Is(err, parser.ErrMemoryLimit):
		return telemetry.OutcomeParserSubprocessErr
	case errors.Is(err, parser.ErrDecode):
		return telemetry.OutcomeParserDecodeErr
	case errors.Is(err, parser.ErrContract):
		return telemetry.OutcomeParserContractErr
	default:
		return telemetry.OutcomeInternalError
	}
}

type parserFailureDetail struct {
	statusCode int
	code       string
	message    string
	details    map[string]any
}

// parserFailureDetails returns HTTP status code, error code, and error message
// for a given parser error.
func parserFailureDetails(err error) (statusCode int, errorCode, errorMsg string) {
	details := parserErrorDetails(err)
	return details.statusCode, details.code, details.message
}

func parserErrorDetails(err error) parserFailureDetail {
	failure := parserFailureDetail{statusCode: http.StatusInternalServerError, code: "internal_error", message: "internal server error"}
	switch {
	case errors.Is(err, parser.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		failure = parserFailureDetail{statusCode: http.StatusGatewayTimeout, code: "parser_timeout", message: "parser timed out while validating Mermaid code"}
	case errors.Is(err, parser.ErrMemoryLimit):
		failure = parserFailureDetail{statusCode: http.StatusInternalServerError, code: "parser_memory_limit", message: "parser exceeded memory limit while validating Mermaid code"}
	case errors.Is(err, parser.ErrSubprocess):
		failure = parserFailureDetail{statusCode: http.StatusInternalServerError, code: "parser_subprocess_error", message: "parser subprocess failed"}
	case errors.Is(err, parser.ErrDecode):
		failure = parserFailureDetail{statusCode: http.StatusInternalServerError, code: "parser_decode_error", message: "parser returned malformed output"}
	case errors.Is(err, parser.ErrContract):
		failure = parserFailureDetail{statusCode: http.StatusInternalServerError, code: "parser_contract_violation", message: "parser response violated service contract"}
	}
	if meta, ok := parser.MetadataFromError(err); ok {
		info := map[string]any{}
		if meta.Suggestion != "" {
			info["suggestion"] = meta.Suggestion
		}
		if meta.Limit != "" {
			info["limit"] = meta.Limit
		}
		if meta.ObservedSizeByte > 0 {
			info["observed_size"] = meta.ObservedSizeByte
		}
		if len(info) > 0 {
			failure.details = info
		}
	}
	return failure
}
