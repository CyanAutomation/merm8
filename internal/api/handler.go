// Package api implements the HTTP handler for the mermaid-lint service.
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
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

const (
	defaultBenchmarkHTMLPath      = "/app/benchmark.html"
	benchmarkHTMLPathEnvVar       = "MERM8_BENCHMARK_HTML_PATH"
	benchmarkStatusHeader         = "X-Merm8-Benchmark-Status"
	benchmarkStatusGenerated      = "generated"
	benchmarkStatusPlaceholder    = "placeholder"
	benchmarkPlaceholderSignature = "benchmark.html was not pre-generated"
)

// serverBusyRetryAfterSeconds defines the stable API contract for 503 server_busy
// responses on analyze endpoints. Clients should combine this floor with
// jittered exponential backoff to avoid synchronized retries.
const serverBusyRetryAfterSeconds = 1

const (
	legacyAnalyzeSunsetHeader      = "Tue, 30 Jun 2026 23:59:59 GMT"
	legacyAnalyzeSuccessorDocLink  = `</v1/docs#/Linting/post_v1_analyze>; rel="successor-version"`
	v1AnalyseDeprecationWarning    = `299 - "POST /v1/analyse is deprecated; use POST /v1/analyze. Planned removal in v1.2.0 (Q2 2026)."`
	v1AnalyseRawDeprecationWarning = `299 - "POST /v1/analyse/raw is deprecated; use POST /v1/analyze/raw. Planned removal in v1.2.0 (Q2 2026)."`
)

const (
	legacySchemaVersionWarningMessage = `legacy key config.schema_version is deprecated; use config.schema-version. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyUnversionedRulesWarning     = `legacy unversioned config shape is deprecated; add config.schema-version and keep rules under config.rules. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyFlatConfigWarning           = `legacy flat config shape is deprecated; move rule settings under config.rules and add config.schema-version. Example: {"config":{"schema-version":"v1","rules":{"max-fanout":{"limit":3}}}}`
	legacyOptionKeyWarningTemplate    = `legacy key config.%s.%s is deprecated; use config.%s.%s. Example: {"%s": ["node:A"]}`
)

var defaultStrictConfigSchema atomic.Bool

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
	var nestedRulesCfg rules.Config
	useNestedRulesCfg := false

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

	rulesValue, hasTopLevelRules := asMap["rules"]

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

		if !hasTopLevelRules {
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
	} else if hasTopLevelRules {
		if strict {
			return rules.Config{}, nil, &validationError{Code: "deprecated_config_format", Path: "config", Message: "legacy unversioned config shape is deprecated; use config.schema-version and config.rules"}
		}
		deprecations = append(deprecations, legacyUnversionedRulesWarning)

		var nested struct {
			Rules rules.Config `json:"rules"`
		}
		if err := json.Unmarshal(raw, &nested); err == nil {
			nestedRulesCfg = nested.Rules
			useNestedRulesCfg = true
		}

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
	if useNestedRulesCfg {
		cfg = nestedRulesCfg
	} else {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return rules.Config{}, nil, &validationError{Code: "invalid_option", Path: "config", Message: "invalid config object"}
		}
	}

	// Validate suppression selectors reference known rules (warnings, not errors)
	for ruleID, ruleConfig := range cfg {
		if suppSelectors, hasSuppression := ruleConfig["suppression-selectors"]; hasSuppression {
			// Convert interface{} to []string if possible
			if selectorArray, ok := suppSelectors.([]interface{}); ok {
				selectors := make([]string, 0, len(selectorArray))
				for _, sel := range selectorArray {
					if selStr, ok := sel.(string); ok {
						selectors = append(selectors, selStr)
					}
				}
				// Validate each selector
				for _, selector := range selectors {
					parsed, ok := rules.ParseSuppressionSelector(selector)
					if !ok {
						return rules.Config{}, nil, &validationError{
							Code:    "invalid_suppression_selector",
							Path:    rulePathPrefix + "." + ruleID + ".suppression-selectors",
							Message: "invalid suppression selector format: " + selector,
						}
					}

					// Validate rule selectors reference known rules (warnings)
					if parsed.Prefix == "rule" && parsed.Value != "*" {
						if _, exists := knownRuleIDs[parsed.Value]; !exists {
							deprecations = append(deprecations, "suppression selector references unknown rule: "+parsed.Value)
						}
					}
				}
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
// It reports whether JSON decoding succeeded, whether JSON mode is missing a code value,
// and any JSON decode error encountered.
func parseRawMermaidInput(body []byte) (analyzeRequest, bool, bool, error) {
	var req analyzeRequest
	if err := json.Unmarshal(body, &req); err == nil {
		return req, true, req.Code == "", nil
	} else {
		return analyzeRequest{Code: string(body)}, false, false, err
	}
	// Not JSON - treat entire body as raw mermaid code
	return analyzeRequest{Code: string(body)}, false, false, nil
}

// likelyJSONIntendedBody reports whether the trimmed request body appears to
// intentionally be JSON content.
func likelyJSONIntendedBody(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
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

// helpSuggestion provides structured remediation guidance with code examples.
type helpSuggestion struct {
	Title          string `json:"title"`           // e.g., "Arrow operator syntax"
	Explanation    string `json:"explanation"`     // e.g., "Mermaid uses '-->' for connections"
	WrongExample   string `json:"wrong-example"`   // Incorrect code snippet (multiline)
	CorrectExample string `json:"correct-example"` // Correct code snippet (multiline)
	DocLink        string `json:"doc-link"`        // URL fragment (e.g., "#arrow-syntax")
	FixAction      string `json:"fix-action"`      // Brief action to take
}

type responseHintAppliesTo struct {
	Line        int               `json:"line,omitempty"`
	Column      int               `json:"column,omitempty"`
	DiagramType model.DiagramType `json:"diagram-type,omitempty"`
}

// responseHint provides structured, machine-readable syntax remediation hints.
type responseHint struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	Severity   string                 `json:"severity"`
	Confidence float64                `json:"confidence"`
	AppliesTo  *responseHintAppliesTo `json:"applies-to,omitempty"`
	FixExample string                 `json:"fix-example,omitempty"`
}

type analyzeResponse struct {
	Valid          bool                 `json:"valid"`
	DiagramType    model.DiagramType    `json:"diagram-type,omitempty"`
	LintSupported  bool                 `json:"lint-supported"`
	RequestID      string               `json:"request-id,omitempty"`
	Timestamp      int64                `json:"timestamp,omitempty"`
	SyntaxError    *syntaxErrorResponse `json:"syntax-error"`
	Issues         []model.Issue        `json:"issues"`
	Hints          []responseHint       `json:"hints,omitempty"`
	Suggestions    []string             `json:"suggestions,omitempty"` // Deprecated: use hints.
	HelpSuggestion *helpSuggestion      `json:"help-suggestion,omitempty"`
	Warnings       []string             `json:"warnings,omitempty"`
	Meta           *responseMeta        `json:"meta,omitempty"`
	Error          *apiErrorDetails     `json:"error,omitempty"`
	Metrics        *metricsResponse     `json:"metrics"`
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
	parser             ParserInterface
	engine             *engine.Engine
	logger             Logger
	serviceVersion     string
	buildCommit        string
	buildTime          string
	metricsHandler     http.Handler
	telemetryMetrics   *telemetry.Metrics
	analyzeCounters    analyzeOutcomeCounters
	startTime          time.Time
	mu                 sync.RWMutex
	strictConfigSchema bool
	// parserConcurrency is initialized once in NewHandler and intentionally never
	// replaced, so all requests share one stable limiter instance.
	parserConcurrency *parserConcurrencyLimiter
}

type parserConcurrencyLimiter struct {
	mu       sync.Mutex
	limit    int
	inFlight int
}

func newParserConcurrencyLimiter() *parserConcurrencyLimiter {
	return &parserConcurrencyLimiter{}
}

func (l *parserConcurrencyLimiter) SetLimit(limit int) {
	if limit < 0 {
		limit = 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
}

func (l *parserConcurrencyLimiter) TryAcquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.limit <= 0 {
		return true
	}

	if l.inFlight >= l.limit {
		return false
	}

	l.inFlight++
	return true
}

func (l *parserConcurrencyLimiter) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.inFlight == 0 {
		return
	}

	l.inFlight--
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

// healthMetricsResponse provides extended health and metrics information.
type healthMetricsResponse struct {
	Status              string                `json:"status"`
	Timestamp           int64                 `json:"timestamp"`
	Uptime              float64               `json:"uptime-seconds"`
	BuildCommit         string                `json:"build-commit,omitempty"`
	BuildTime           string                `json:"build-time,omitempty"`
	ParserReady         bool                  `json:"parser-ready"`
	ParserVersion       string                `json:"parser-version,omitempty"`
	LintSupported       []model.DiagramFamily `json:"lint-supported"`
	TotalRequests       uint64                `json:"total-requests"`
	SuccessfulAnalyses  healthMetricsOutcome  `json:"successful-analyses"`
	FailedAnalyses      healthMetricsOutcome  `json:"failed-analyses"`
	MedianParserLatency float64               `json:"median-parser-latency-ms"`
	P95ParserLatency    float64               `json:"p95-parser-latency-ms"`
}

// healthMetricsOutcome breaks down analyses by outcome for health endpoint.
type healthMetricsOutcome struct {
	Total          uint64 `json:"total"`
	SyntaxErrors   uint64 `json:"syntax-errors,omitempty"`
	LintSuccess    uint64 `json:"lint-success,omitempty"`
	ParserTimeout  uint64 `json:"parser-timeout,omitempty"`
	ParserErrors   uint64 `json:"parser-errors,omitempty"`
	InternalErrors uint64 `json:"internal-errors,omitempty"`
}

// NewHandler creates a Handler with the given parser and engine.
// This constructor allows dependency injection for testing.
func NewHandler(p ParserInterface, e *engine.Engine) *Handler {
	return &Handler{
		parser:             p,
		engine:             e,
		logger:             normalizeLogger(NewLogger("api")),
		startTime:          time.Now(),
		strictConfigSchema: defaultStrictConfigSchema.Load(),
		parserConcurrency:  newParserConcurrencyLimiter(),
	}
}

// SetStrictConfigSchema toggles strict config schema enforcement for this handler.
// When strict mode is enabled, legacy config formats are rejected.
func (h *Handler) SetStrictConfigSchema(strict bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.strictConfigSchema = strict
}

func (h *Handler) strictConfigSchemaEnabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.strictConfigSchema
}

// SetParserConcurrencyLimit configures a limit for concurrent parser invocations.
// A value <= 0 disables the limit.
func (h *Handler) SetParserConcurrencyLimit(limit int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.parserConcurrency.SetLimit(limit)
}

func (h *Handler) tryAcquireParserSlot() (func(), bool) {
	if !h.parserConcurrency.TryAcquire() {
		return nil, false
	}

	return h.parserConcurrency.Release, true
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

// SetStrictConfigSchema sets the default strict config schema enforcement for newly
// constructed handlers.
// Deprecated: prefer per-handler configuration via (*Handler).SetStrictConfigSchema.
func SetStrictConfigSchema(strict bool) {
	defaultStrictConfigSchema.Store(strict)
}

// SetStrictConfigSchemaForTesting is a deprecated alias for SetStrictConfigSchema.
func SetStrictConfigSchemaForTesting(strict bool) {
	SetStrictConfigSchema(strict)
}

// RegisterRoutes attaches all routes to mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Canonical versioned API routes.
	mux.HandleFunc("GET /v1/healthz", h.Healthz)
	mux.HandleFunc("GET /v1/health", h.Healthz)
	mux.HandleFunc("GET /v1/health/metrics", h.HealthMetrics)
	mux.HandleFunc("GET /v1/ready", h.Ready)
	mux.HandleFunc("GET /v1/info", h.Info)
	mux.HandleFunc("GET /v1/metrics", h.Metrics)
	mux.HandleFunc("GET /v1/internal/metrics", h.InternalMetrics)
	mux.HandleFunc("GET /v1/rules", h.ListRules)
	mux.HandleFunc("GET /v1/rules/schema", h.RuleConfigSchema)
	mux.HandleFunc("GET /v1/diagram-types", h.DiagramTypes)
	mux.HandleFunc("GET /v1/analyze/help", h.AnalyzeHelp)
	mux.HandleFunc("POST /v1/analyze", h.Analyze)
	mux.HandleFunc("POST /v1/analyse", h.Analyze)
	mux.HandleFunc("POST /v1/analyze/raw", h.AnalyzeRaw)
	mux.HandleFunc("POST /v1/analyse/raw", h.AnalyzeRaw)
	mux.HandleFunc("POST /v1/analyze/sarif", h.AnalyzeSARIF)
	mux.HandleFunc("GET /v1/spec", h.ServeSpec)
	mux.HandleFunc("GET /v1/docs", h.ServeSwagger)
	mux.HandleFunc("GET /v1/benchmark.html", h.ServeBenchmark)
	mux.HandleFunc("GET /v1/config-versions", h.ConfigVersions)

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
	mux.HandleFunc("GET /benchmark.html", h.ServeBenchmark)
	mux.HandleFunc("GET /config-versions", h.ConfigVersions)
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
	h.mu.RLock()
	buildCommit := h.buildCommit
	buildTime := h.buildTime
	h.mu.RUnlock()

	resp := map[string]string{"status": "ok"}
	if buildCommit != "" {
		resp["build-commit"] = buildCommit
	}
	if buildTime != "" {
		resp["build-time"] = buildTime
	}
	writeJSON(w, http.StatusOK, resp)
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

	h.mu.RLock()
	buildCommit := h.buildCommit
	buildTime := h.buildTime
	h.mu.RUnlock()

	resp := map[string]string{"status": "ready"}
	if buildCommit != "" {
		resp["build-commit"] = buildCommit
	}
	if buildTime != "" {
		resp["build-time"] = buildTime
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			resp["parser-version"] = info.ParserVersion
			resp["mermaid-version"] = info.MermaidVersion
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HealthMetrics handles GET /v1/health/metrics and returns extended health status with metrics.
func (h *Handler) HealthMetrics(w http.ResponseWriter, _ *http.Request) {
	h.mu.RLock()
	buildCommit := h.buildCommit
	buildTime := h.buildTime
	startTime := h.startTime
	h.mu.RUnlock()

	uptime := time.Since(startTime).Seconds()

	// Check parser readiness
	parserReady := true
	parserVersion := ""
	if checker, ok := h.parser.(ReadinessChecker); ok {
		if err := checker.Ready(); err != nil {
			parserReady = false
		}
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			parserVersion = info.ParserVersion
		}
	}

	// Get lint-supported diagram families
	lintSupported := []model.DiagramFamily{}
	if h.engine != nil {
		lintSupported = h.engine.DiagramFamilies()
	}

	// Aggregate analyze outcome counters
	totalRequests := h.analyzeCounters.validSuccess.Load() +
		h.analyzeCounters.syntaxError.Load() +
		h.analyzeCounters.parserTimeout.Load() +
		h.analyzeCounters.parserSubprocess.Load() +
		h.analyzeCounters.parserDecode.Load() +
		h.analyzeCounters.parserContract.Load() +
		h.analyzeCounters.parserInternalError.Load()

	successfulAnalyses := healthMetricsOutcome{
		Total:       h.analyzeCounters.validSuccess.Load(),
		LintSuccess: h.analyzeCounters.validSuccess.Load(),
	}

	failedAnalyses := healthMetricsOutcome{
		Total:          totalRequests - successfulAnalyses.Total,
		SyntaxErrors:   h.analyzeCounters.syntaxError.Load(),
		ParserTimeout:  h.analyzeCounters.parserTimeout.Load(),
		ParserErrors:   h.analyzeCounters.parserSubprocess.Load() + h.analyzeCounters.parserDecode.Load() + h.analyzeCounters.parserContract.Load(),
		InternalErrors: h.analyzeCounters.parserInternalError.Load(),
	}

	// TODO: Add real P50/P95 latency data when histogram metrics are collected
	resp := healthMetricsResponse{
		Status:              "ok",
		Timestamp:           time.Now().UnixMilli(),
		Uptime:              uptime,
		BuildCommit:         buildCommit,
		BuildTime:           buildTime,
		ParserReady:         parserReady,
		ParserVersion:       parserVersion,
		LintSupported:       lintSupported,
		TotalRequests:       totalRequests,
		SuccessfulAnalyses:  successfulAnalyses,
		FailedAnalyses:      failedAnalyses,
		MedianParserLatency: 0, // TODO: populate from histogram
		P95ParserLatency:    0, // TODO: populate from histogram
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
		resp["build-commit"] = buildCommit
	}
	if buildTime != "" {
		resp["build-time"] = buildTime
	}
	if provider, ok := h.parser.(VersionInfoProvider); ok {
		if info, err := provider.VersionInfo(); err == nil {
			if info.ParserVersion != "" {
				resp["parser-version"] = info.ParserVersion
			}
			if info.MermaidVersion != "" {
				resp["mermaid-version"] = info.MermaidVersion
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ConfigVersions handles GET /v1/config-versions and returns config schema version compatibility info.
func (h *Handler) ConfigVersions(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"current":   rules.CurrentConfigSchemaVersion,
		"supported": []string{rules.CurrentConfigSchemaVersion},
		"deprecations": []map[string]interface{}{
			{
				"version":         "unversioned",
				"status":          "deprecated",
				"sunset-date":     "2026-12-31T23:59:59Z",
				"replacement":     "Use config.schema-version: v1 with config.rules structure",
				"migration-notes": "Legacy flat config shapes and unversioned config structures must be migrated to the v1 schema.",
			},
			{
				"version":         "schema_version (underscore)",
				"status":          "deprecated",
				"sunset-date":     "2026-09-30T23:59:59Z",
				"replacement":     "Use config.schema-version (hyphenated) instead",
				"migration-notes": "The underscore variant config.schema_version is deprecated; migrate to config.schema-version.",
			},
		},
		"compatibility": map[string]interface{}{
			"api-version":            "1.0",
			"accepts-accept-version": true,
			"version-negotiation":    "Use Accept-Version header to request specific API versions. Response includes Content-Version header.",
			"rate-limiting":          "Rate limit info available in X-RateLimit-* response headers.",
		},
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
	emitAnalyseAliasWarning(w, r)
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

	cfg, deprecationWarnings, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs(), h.strictConfigSchemaEnabled())
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

	releaseParserSlot, ok := h.tryAcquireParserSlot()
	if !ok {
		observeAnalyzeOutcome("server_busy")
		setServerBusyRetryAfterHeader(w)
		writeErrorWithDetailsAndContext(w, r, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again", nil)
		return
	}
	defer releaseParserSlot()

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			writeErrorWithDetailsAndContext(w, r, http.StatusBadRequest, "invalid_option", strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "), nil)
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
		hints := hintsForSyntaxError(syntaxErr, req.Code)
		suggestions := suggestionsFromHints(hints)
		helpSugg := helpForSyntaxError(syntaxErr, req.Code)
		// On syntax errors, lint-supported is always false because the diagram cannot be linted
		resp := analyzeResponse{
			Valid:          false,
			DiagramType:    diagramType,
			LintSupported:  false,
			RequestID:      RequestIDFromContext(r.Context()),
			Timestamp:      time.Now().UnixMilli(),
			Hints:          hints,
			Suggestions:    suggestions,
			HelpSuggestion: helpSugg,
			Warnings:       deprecationWarnings,
			Meta:           responseMetaForWarnings(deprecationWarnings),
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
	if !h.isLintSupported(family) {
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
			Valid:         true,
			DiagramType:   diagram.Type,
			LintSupported: false,
			RequestID:     RequestIDFromContext(r.Context()),
			Timestamp:     time.Now().UnixMilli(),
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
		RequestID:     RequestIDFromContext(r.Context()),
		Timestamp:     time.Now().UnixMilli(),
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
// JSON requests can also include config and parser settings, matching /analyze behavior.
func (h *Handler) AnalyzeRaw(w http.ResponseWriter, r *http.Request) {
	emitAnalyseAliasWarning(w, r)
	h.analyzeRawWithCallback(w, r, func(resp analyzeResponse) {
		writeJSON(w, http.StatusOK, resp)
	})
}

func emitAnalyseAliasWarning(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.URL == nil {
		return
	}

	switch r.URL.Path {
	case "/v1/analyse":
		w.Header().Set("Deprecation", "true")
		w.Header().Add("Warning", v1AnalyseDeprecationWarning)
	case "/v1/analyse/raw":
		w.Header().Set("Deprecation", "true")
		w.Header().Add("Warning", v1AnalyseRawDeprecationWarning)
	}
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

	req, jsonDecoded, missingCode, jsonDecodeErr := parseRawMermaidInput(body)

	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	jsonContentType := contentType == "application/json" || strings.HasSuffix(contentType, "+json")

	requestHints := make([]responseHint, 0, 1)
	if jsonContentType && !jsonDecoded && jsonDecodeErr != nil && likelyJSONIntendedBody(body) {
		requestHints = append(requestHints, responseHint{
			Code:       "raw_json_decode_failed_fallback_to_text",
			Message:    "request Content-Type is JSON but body failed JSON decoding; falling back to treating body as raw mermaid text",
			Severity:   "info",
			Confidence: 1.0,
		})
	}

	if jsonDecoded && missingCode {
		observeAnalyzeOutcome("missing_code")
		writeError(w, http.StatusBadRequest, "missing_code", "field 'code' is required")
		return
	}

	if req.Code == "" {
		observeAnalyzeOutcome("missing_code")
		writeError(w, http.StatusBadRequest, "missing_code", "request body is empty or does not contain mermaid code")
		return
	}

	cfg, deprecationWarnings, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs(), h.strictConfigSchemaEnabled())
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

	releaseParserSlot, ok := h.tryAcquireParserSlot()
	if !ok {
		observeAnalyzeOutcome("server_busy")
		setServerBusyRetryAfterHeader(w)
		writeErrorWithDetailsAndContext(w, r, http.StatusServiceUnavailable, "server_busy", "parser concurrency limit reached; try again", nil)
		return
	}
	defer releaseParserSlot()

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			writeErrorWithDetailsAndContext(w, r, http.StatusBadRequest, "invalid_option", strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "), nil)
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
		hints := append(requestHints, hintsForSyntaxError(syntaxErr, req.Code)...)
		suggestions := suggestionsFromHints(hints)
		helpSugg := helpForSyntaxError(syntaxErr, req.Code)
		// On syntax errors, only set lint-supported=true if we detected a diagram type
		// AND that type's family supports linting. If no type detected, always false.
		var lintSupported bool
		if diagramType == model.DiagramTypeUnknown {
			lintSupported = false
		} else {
			lintSupported = h.isLintSupported(diagramType.Family())
		}
		resp := analyzeResponse{
			Valid:          false,
			DiagramType:    diagramType,
			LintSupported:  lintSupported,
			RequestID:      RequestIDFromContext(r.Context()),
			Timestamp:      time.Now().UnixMilli(),
			Hints:          hints,
			Suggestions:    suggestions,
			HelpSuggestion: helpSugg,
			Warnings:       deprecationWarnings,
			Meta:           responseMetaForWarnings(deprecationWarnings),
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
	if !h.isLintSupported(family) {
		observeAnalyzeOutcome("unsupported_diagram_type")
		setAnalyzeLogFields(r.Context(), "unsupported_diagram_type", string(diagram.Type))
		unsupportedIssue := model.Issue{
			RuleID:   "unsupported-diagram-type",
			Severity: "info",
			Message:  "diagram type \"" + string(diagram.Type) + "\" is parsed but lint rules are not available yet",
		}
		resp := analyzeResponse{
			Valid:         true,
			DiagramType:   diagram.Type,
			LintSupported: false,
			RequestID:     RequestIDFromContext(r.Context()),
			Timestamp:     time.Now().UnixMilli(),
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
	}
	observeAnalyzeOutcome(telemetry.OutcomeLintSuccess)
	setAnalyzeLogFields(r.Context(), telemetry.OutcomeLintSuccess, string(diagram.Type))

	resp := analyzeResponse{
		Valid:         true,
		DiagramType:   diagram.Type,
		LintSupported: h.isLintSupported(diagram.Type.Family()),
		RequestID:     RequestIDFromContext(r.Context()),
		Timestamp:     time.Now().UnixMilli(),
		SyntaxError:   nil,
		Issues:        issues,
		Hints:         requestHints,
		Warnings:      deprecationWarnings,
		Meta:          responseMetaForWarnings(deprecationWarnings),
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

	cfg, deprecationWarnings, configValidationErr := parseConfig(req.Config, h.engine.KnownRuleIDs(), h.strictConfigSchemaEnabled())
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

	releaseParserSlot, ok := h.tryAcquireParserSlot()
	if !ok {
		observeAnalyzeOutcome("server_busy")
		setServerBusyRetryAfterHeader(w)
		report := sarif.TransformError(sarif.ErrorInfo{
			Code:    "server_busy",
			Message: "parser concurrency limit reached; try again",
		}, meta)
		writeSARIF(w, http.StatusServiceUnavailable, report)
		return
	}
	defer releaseParserSlot()

	parseStart := time.Now()
	diagram, syntaxErr, err := h.parseWithRequestSettings(req)
	parseDuration := time.Since(parseStart)
	if err != nil {
		if errors.Is(err, errInvalidRequest) {
			observeAnalyzeOutcome("invalid_option")
			report := sarif.TransformError(sarif.ErrorInfo{
				Code:    "invalid_option",
				Message: strings.TrimPrefix(err.Error(), errInvalidRequest.Error()+": "),
			}, meta)
			writeSARIF(w, http.StatusBadRequest, report)
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
	if !h.isLintSupported(family) {
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

type syntaxInputSignals struct {
	trimmedCode               string
	firstLine                 string
	diagramType               model.DiagramType
	hasGraphviz               bool
	hasYAMLFrontmatter        bool
	hasTabs                   bool
	hasFlowchartSingleArrow   bool
	missingDiagramTypeLikely  bool
	hasMarkdownFence          bool
	hasStateDiagramKeyword    bool
	stateSyntaxErrorMentioned bool
	hasSmartQuotes            bool
	hasUnicodeArrowDash       bool
	hasFlowchartLowercaseEnd  bool
	hasMalformedBracketClose  bool
}

func analyzeInputSignals(code string, syntaxErr *parser.SyntaxError) syntaxInputSignals {
	trimmed := strings.TrimSpace(code)
	firstLine := strings.TrimSpace(strings.SplitN(code, "\n", 2)[0])
	lowerCode := strings.ToLower(code)
	lowerErr := ""
	if syntaxErr != nil {
		lowerErr = strings.ToLower(syntaxErr.Message)
	}

	openBrackets := strings.Count(code, "[")
	closeBrackets := strings.Count(code, "]")

	missingDiagramTypeLikely := false
	if syntaxErr != nil && (strings.Contains(syntaxErr.Message, "No diagram type") || strings.Contains(syntaxErr.Message, "Unexpected")) {
		missingDiagramTypeLikely = !isDiagramTypeKeyword(firstLine)
	}

	return syntaxInputSignals{
		trimmedCode:               trimmed,
		firstLine:                 firstLine,
		diagramType:               defaultDiagramTypeForSyntaxError(code),
		hasGraphviz:               strings.Contains(code, "digraph") || strings.Contains(code, "rankdir"),
		hasYAMLFrontmatter:        strings.HasPrefix(trimmed, "---"),
		hasTabs:                   strings.Contains(code, "\t"),
		hasFlowchartSingleArrow:   (strings.HasPrefix(firstLine, "flowchart") || strings.HasPrefix(firstLine, "graph")) && strings.Contains(strings.ReplaceAll(code, "-->", ""), "->"),
		missingDiagramTypeLikely:  missingDiagramTypeLikely,
		hasMarkdownFence:          strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "```mermaid"),
		hasStateDiagramKeyword:    strings.Contains(lowerCode, "statediagram"),
		stateSyntaxErrorMentioned: strings.Contains(lowerErr, "state") && (strings.Contains(lowerErr, "syntax") || strings.Contains(lowerErr, "parse") || strings.Contains(lowerErr, "expect")),
		hasSmartQuotes:            strings.ContainsAny(code, "“”‘’"),
		hasUnicodeArrowDash:       strings.Contains(code, "—>") || strings.Contains(code, "–>") || strings.Contains(code, "—->") || strings.Contains(code, "–->"),
		hasFlowchartLowercaseEnd:  (strings.HasPrefix(firstLine, "flowchart") || strings.HasPrefix(firstLine, "graph")) && strings.Contains(code, "\nend\n"),
		hasMalformedBracketClose:  openBrackets != closeBrackets || strings.Contains(code, "[[") && strings.Count(code, "]]") < strings.Count(code, "[["),
	}
}

// hintsForSyntaxError analyzes a syntax error and code to provide smart, actionable hints.
func hintsForSyntaxError(syntaxErr *parser.SyntaxError, code string) []responseHint {
	hints := make([]responseHint, 0, 9)
	signals := analyzeInputSignals(code, syntaxErr)
	diagramType := signals.diagramType

	// Detect Graphviz syntax.
	if signals.hasGraphviz {
		hints = append(hints, responseHint{
			Code:       "graphviz_syntax_detected",
			Message:    "This looks like Graphviz syntax. Use Mermaid syntax instead: 'flowchart TD' for directed graphs.",
			Severity:   "warning",
			Confidence: 0.99,
			AppliesTo:  &responseHintAppliesTo{DiagramType: diagramType},
			FixExample: "flowchart TD\n  A --> B",
		})
	}

	// Detect YAML frontmatter.
	if signals.hasYAMLFrontmatter {
		hints = append(hints, responseHint{
			Code:       "yaml_frontmatter_detected",
			Message:    "Remove YAML frontmatter (---); Mermaid code should start directly with the diagram type.",
			Severity:   "warning",
			Confidence: 0.95,
			AppliesTo:  &responseHintAppliesTo{Line: 1, DiagramType: diagramType},
			FixExample: "flowchart TD\n  A --> B",
		})
	}

	// Detect tabs instead of spaces.
	if signals.hasTabs {
		hints = append(hints, responseHint{
			Code:       "tab_indentation_detected",
			Message:    "Replace tabs with spaces (Mermaid uses space indentation).",
			Severity:   "info",
			Confidence: 0.98,
			AppliesTo:  &responseHintAppliesTo{Line: syntaxErr.Line, Column: syntaxErr.Column, DiagramType: diagramType},
			FixExample: "    A --> B",
		})
	}

	// Detect arrow syntax issues based on error message and code content.
	if signals.hasFlowchartSingleArrow {
		hints = append(hints, responseHint{
			Code:       "flowchart_arrow_operator_detected",
			Message:    "Use '-->' for flowchart connections, not '->'.",
			Severity:   "warning",
			Confidence: 0.97,
			AppliesTo:  &responseHintAppliesTo{Line: syntaxErr.Line, Column: syntaxErr.Column, DiagramType: diagramType},
			FixExample: "A --> B",
		})
	}

	if signals.hasMarkdownFence {
		hints = append(hints, responseHint{
			Code:       "markdown_fence_detected",
			Message:    "Remove Markdown code fences (``` or ```mermaid); send raw Mermaid text only.",
			Severity:   "warning",
			Confidence: 0.99,
			AppliesTo:  &responseHintAppliesTo{Line: 1, DiagramType: diagramType},
			FixExample: "flowchart TD\n  A --> B",
		})
	}

	if signals.hasStateDiagramKeyword && signals.stateSyntaxErrorMentioned {
		hints = append(hints, responseHint{
			Code:       "state_diagram_variant_migration",
			Message:    "State diagram syntax mismatch detected. Try `stateDiagram-v2` and Mermaid state transitions like `[*] --> Idle`.",
			Severity:   "warning",
			Confidence: 0.96,
			AppliesTo:  &responseHintAppliesTo{DiagramType: model.DiagramTypeState},
			FixExample: "stateDiagram-v2\n  [*] --> Idle",
		})
	}

	if signals.hasSmartQuotes || signals.hasUnicodeArrowDash {
		hints = append(hints, responseHint{
			Code:       "smart_punctuation_detected",
			Message:    "Replace smart quotes/dashes with plain ASCII characters (" + `"` + `'` + `, -, -->` + ").",
			Severity:   "warning",
			Confidence: 0.98,
			AppliesTo:  &responseHintAppliesTo{Line: syntaxErr.Line, Column: syntaxErr.Column, DiagramType: diagramType},
			FixExample: "A --> B\nC[\"Quoted label\"]",
		})
	}

	if signals.hasFlowchartLowercaseEnd {
		hints = append(hints, responseHint{
			Code:       "flowchart_reserved_end_keyword",
			Message:    "`end` is reserved in flowcharts. If used as a node label/id, rename it (for example `EndNode`).",
			Severity:   "warning",
			Confidence: 0.92,
			AppliesTo:  &responseHintAppliesTo{DiagramType: model.DiagramTypeFlowchart},
			FixExample: "flowchart TD\n  Start --> EndNode",
		})
	}

	if signals.hasMalformedBracketClose {
		hints = append(hints, responseHint{
			Code:       "malformed_label_brackets",
			Message:    "Unbalanced label delimiters detected. Check that every '[' has a matching ']'.",
			Severity:   "warning",
			Confidence: 0.91,
			AppliesTo:  &responseHintAppliesTo{Line: syntaxErr.Line, Column: syntaxErr.Column, DiagramType: diagramType},
			FixExample: "A[Start] --> B[End]",
		})
	}

	// Detect missing diagram type keyword.
	if signals.missingDiagramTypeLikely {
		hints = append(hints, responseHint{
			Code:       "missing_diagram_type_keyword",
			Message:    "Start your diagram with a type keyword: 'flowchart TD', 'sequenceDiagram', 'classDiagram', 'erDiagram', or 'stateDiagram-v2'.",
			Severity:   "warning",
			Confidence: 0.90,
			AppliesTo:  &responseHintAppliesTo{Line: 1},
			FixExample: "flowchart TD\n  A --> B",
		})
	}

	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].Confidence == hints[j].Confidence {
			return hints[i].Code < hints[j].Code
		}
		return hints[i].Confidence > hints[j].Confidence
	})

	deduped := make([]responseHint, 0, len(hints))
	seenCodes := make(map[string]struct{}, len(hints))
	for _, hint := range hints {
		if _, exists := seenCodes[hint.Code]; exists {
			continue
		}
		seenCodes[hint.Code] = struct{}{}
		deduped = append(deduped, hint)
	}

	return deduped
}

func suggestionsFromHints(hints []responseHint) []string {
	if len(hints) == 0 {
		return nil
	}
	suggestions := make([]string, 0, len(hints))
	for _, hint := range hints {
		suggestions = append(suggestions, hint.Message)
	}
	return suggestions
}

// helpForSyntaxError generates structured remediation guidance for syntax errors.
// It returns the primary help suggestion based on the error type and code context.
func helpForSyntaxError(syntaxErr *parser.SyntaxError, code string) *helpSuggestion {
	lines := strings.Split(code, "\n")
	signals := analyzeInputSignals(code, syntaxErr)

	// Detect Graphviz syntax (high confidence error)
	if signals.hasGraphviz {
		return &helpSuggestion{
			Title:          "Graphviz syntax detected",
			Explanation:    "This looks like Graphviz (GraphQL visualization) syntax. merm8 uses Mermaid diagrams, which have different syntax.",
			WrongExample:   "digraph G {\n  A -> B -> C\n}",
			CorrectExample: "flowchart TD\n  A --> B --> C",
			DocLink:        "#common-mistakes",
			FixAction:      "Replace Graphviz syntax (digraph, rankdir, etc.) with Mermaid diagram type keywords (flowchart, sequenceDiagram, etc.)",
		}
	}

	// Detect YAML frontmatter
	if signals.hasYAMLFrontmatter {
		return &helpSuggestion{
			Title:          "YAML frontmatter not supported",
			Explanation:    "Mermaid code should not include YAML frontmatter. Remove the --- lines and start directly with the diagram type.",
			WrongExample:   "---\ntitle: My Diagram\n---\nflowchart TD\n  A --> B",
			CorrectExample: "flowchart TD\n  A --> B",
			DocLink:        "#yaml-frontmatter",
			FixAction:      "Remove the YAML frontmatter (--- lines) from the start of your diagram",
		}
	}

	if signals.hasMarkdownFence {
		return &helpSuggestion{
			Title:          "Markdown code fence detected",
			Explanation:    "The analyzer expects Mermaid source only, not Markdown fences. Remove surrounding ``` or ```mermaid lines.",
			WrongExample:   "```mermaid\nflowchart TD\n  A --> B\n```",
			CorrectExample: "flowchart TD\n  A --> B",
			DocLink:        "#common-mistakes",
			FixAction:      "Remove Markdown fences before sending Mermaid to /v1/analyze or /v1/analyze/raw",
		}
	}

	if signals.hasSmartQuotes || signals.hasUnicodeArrowDash {
		return &helpSuggestion{
			Title:          "Smart punctuation detected",
			Explanation:    "Copied text can replace ASCII quotes/dashes with typographic characters that Mermaid cannot parse in operators or labels.",
			WrongExample:   "flowchart TD\n  A —> B\n  B[“Quoted”]",
			CorrectExample: "flowchart TD\n  A --> B\n  B[\"Quoted\"]",
			DocLink:        "#arrow-syntax",
			FixAction:      "Replace typographic quotes/dashes with plain ASCII characters",
		}
	}

	if signals.hasStateDiagramKeyword && signals.stateSyntaxErrorMentioned {
		return &helpSuggestion{
			Title:          "State diagram variant mismatch",
			Explanation:    "Your parser error points to state syntax. Migrate to `stateDiagram-v2` and standard Mermaid state transitions.",
			WrongExample:   "stateDiagram\n  state idle",
			CorrectExample: "stateDiagram-v2\n  [*] --> Idle",
			DocLink:        "#diagram-types",
			FixAction:      "Switch to `stateDiagram-v2` style syntax for state diagrams",
		}
	}

	// Detect tabs instead of spaces (get the problematic line if possible)
	if signals.hasTabs {
		probableLine := "    A --> B"
		if syntaxErr.Line > 0 && syntaxErr.Line <= len(lines) {
			probableLine = lines[syntaxErr.Line-1]
		}
		// Show what it looks like with tabs (represented visually)
		wrongExample := probableLine // This line contains tabs
		correctExample := strings.ReplaceAll(probableLine, "\t", "    ")

		return &helpSuggestion{
			Title:          "Indent with spaces, not tabs",
			Explanation:    "Mermaid requires space indentation. Tabs in diagram syntax cause parse errors.",
			WrongExample:   wrongExample,
			CorrectExample: correctExample,
			DocLink:        "#indentation",
			FixAction:      "Replace all tab characters with 4 spaces for indentation",
		}
	}

	// Detect arrow syntax issues (looking at the specific error line)
	if syntaxErr.Line > 0 && syntaxErr.Line <= len(lines) {
		probLine := lines[syntaxErr.Line-1]
		if strings.Contains(probLine, "->") && !strings.Contains(probLine, "-->") {
			return &helpSuggestion{
				Title:          "Arrow operator syntax",
				Explanation:    "Mermaid requires '-->' (double dash) for flowchart connections. Single '->' is not valid.",
				WrongExample:   "Process" + " -> Decision",
				CorrectExample: "Process" + " --> Decision",
				DocLink:        "#arrow-syntax",
				FixAction:      "Replace '->' with '-->' on line " + fmt.Sprintf("%d", syntaxErr.Line),
			}
		}
	}

	// Detect missing diagram type keyword
	if signals.missingDiagramTypeLikely && signals.firstLine != "" {
		return &helpSuggestion{
			Title:          "Missing diagram type keyword",
			Explanation:    "Every Mermaid diagram must start with a type keyword (flowchart, sequenceDiagram, etc.) on the first line.",
			WrongExample:   "A --> B\nB --> C",
			CorrectExample: "flowchart TD\n  A --> B\n  B --> C",
			DocLink:        "#diagram-types",
			FixAction:      "Add a diagram type keyword to the first line: 'flowchart TD', 'sequenceDiagram', 'classDiagram', 'erDiagram', or 'stateDiagram-v2'",
		}
	}

	return nil
}

// isDiagramTypeKeyword checks if a line starts with a valid diagram type keyword.
func isDiagramTypeKeyword(line string) bool {
	keywords := []string{"flowchart", "graph", "sequenceDiagram", "classDiagram", "erDiagram", "stateDiagram"}
	for _, kw := range keywords {
		if strings.HasPrefix(strings.TrimSpace(line), kw) {
			return true
		}
	}
	return false
}

// helpForConfigError generates structured remediation guidance for config errors.
func helpForConfigError(validErr *validationError) *helpSuggestion {
	switch validErr.Code {
	case "unknown_rule":
		return &helpSuggestion{
			Title:          "Unknown rule in config",
			Explanation:    "The rule name you specified is not recognized. Each rule requires a valid rule ID.",
			WrongExample:   `"rules": {"max-fanout": {}}`,
			CorrectExample: `"rules": {"core/max-fanout": {"limit": 3}}`,
			FixAction:      "Use GET /v1/rules to discover available rules and their namespaces",
		}
	case "invalid_option":
		if strings.Contains(validErr.Message, "config must be object") {
			return &helpSuggestion{
				Title:          "Config must be an object",
				Explanation:    "The config parameter should be a JSON object, not a string, array, or primitive value.",
				WrongExample:   `"config": "invalid"`,
				CorrectExample: `"config": {"schema-version": "v1", "rules": {"core/max-fanout": {"limit": 3}}}`,
				FixAction:      "Ensure config is a JSON object (wrapped in {}) and contains schema-version and rules properties",
			}
		}
	}
	return nil
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
		HelpSuggestion: helpForConfigError(configValidationErr),
		LintSupported:  false,
		SyntaxError:    nil,
		Issues:         []model.Issue{},
		Metrics:        defaultMetrics(model.DiagramTypeUnknown),
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

// ServeBenchmark serves a benchmark HTML asset from disk.
func (h *Handler) ServeBenchmark(w http.ResponseWriter, r *http.Request) {
	benchmarkPath := strings.TrimSpace(os.Getenv(benchmarkHTMLPathEnvVar))
	if benchmarkPath == "" {
		benchmarkPath = defaultBenchmarkHTMLPath
	}

	content, err := os.ReadFile(benchmarkPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": fmt.Sprintf("benchmark file not found at %q", benchmarkPath),
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to read benchmark file",
		})
		return
	}

	status := benchmarkStatusGenerated
	if strings.Contains(string(content), benchmarkPlaceholderSignature) {
		status = benchmarkStatusPlaceholder
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set(benchmarkStatusHeader, status)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
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

// writeErrorWithContext includes request ID and timestamp in error responses
func writeErrorWithContext(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeErrorWithDetailsAndContext(w, r, status, code, message, nil)
}

func writeErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		RequestID:     "",
		Timestamp:     0,
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

// writeErrorWithDetailsAndContext includes request ID and timestamp in error responses
func writeErrorWithDetailsAndContext(w http.ResponseWriter, r *http.Request, status int, code, message string, details map[string]any) {
	writeJSON(w, status, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		RequestID:     RequestIDFromContext(r.Context()),
		Timestamp:     time.Now().UnixMilli(),
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
	hasOverride := req.Parser.TimeoutSeconds != nil || req.Parser.MaxOldSpaceMB != nil
	parserWithConfig, supportsConfig := h.parser.(ParserWithConfig)
	if hasOverride && !supportsConfig {
		return nil, nil, fmt.Errorf("%w: per-request parser settings are unsupported by the configured parser", errInvalidRequest)
	}
	minTimeout, maxTimeout, minMem, maxMem := parser.LimitBounds()
	if req.Parser.TimeoutSeconds != nil {
		// Validate timeout is in allowed range
		if *req.Parser.TimeoutSeconds < int(minTimeout.Seconds()) || *req.Parser.TimeoutSeconds > int(maxTimeout.Seconds()) {
			return nil, nil, fmt.Errorf("%w: parser.timeout_seconds must be an integer between %d and %d seconds; got %d", errInvalidRequest, int(minTimeout.Seconds()), int(maxTimeout.Seconds()), *req.Parser.TimeoutSeconds)
		}
		timeout := time.Duration(*req.Parser.TimeoutSeconds) * time.Second
		cfg.Timeout = timeout
	}
	if req.Parser.MaxOldSpaceMB != nil {
		// Validate memory limit is in allowed range
		if *req.Parser.MaxOldSpaceMB < minMem || *req.Parser.MaxOldSpaceMB > maxMem {
			return nil, nil, fmt.Errorf("%w: parser.max_old_space_mb must be an integer between %d and %d MiB; got %d", errInvalidRequest, minMem, maxMem, *req.Parser.MaxOldSpaceMB)
		}
		cfg.NodeMaxOldSpaceMB = *req.Parser.MaxOldSpaceMB
	}
	if supportsConfig {
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
	writeJSON(w, details.statusCode, analyzeResponse{
		Valid:         false,
		LintSupported: false,
		RequestID:     requestID,
		Timestamp:     time.Now().UnixMilli(),
		SyntaxError:   nil,
		Issues:        []model.Issue{},
		Metrics:       defaultMetrics(model.DiagramTypeUnknown),
		Error: &apiErrorDetails{
			Code:    details.code,
			Message: details.message,
			Details: details.details,
		},
	})
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
