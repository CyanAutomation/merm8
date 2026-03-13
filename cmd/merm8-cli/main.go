package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

const (
	exitOK        = 0
	exitFindings  = 1
	exitInternal  = 2
	exitTransport = 3
)

type cliOptions struct {
	UseStdin     bool
	Format       string
	URL          string
	ConfigPath   string
	FailOnLint   bool
	FailOnSyntax bool
	Timeout      time.Duration
	Files        []string
}

type analyzeRequest struct {
	Code   string          `json:"code"`
	Config json.RawMessage `json:"config,omitempty"`
}

type analyzeError struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Path    string   `json:"path,omitempty"`
	Meta    any      `json:"meta,omitempty"`
	Support []string `json:"supported,omitempty"`
}

type apiErrorEnvelope struct {
	Error *analyzeError `json:"error"`
}

type outputResult struct {
	Input       string              `json:"input"`
	Valid       bool                `json:"valid"`
	DiagramType model.DiagramType   `json:"diagram-type,omitempty"`
	Supported   bool                `json:"lint-supported"`
	SyntaxError *parser.SyntaxError `json:"syntax-error"`
	Issues      []model.Issue       `json:"issues"`
	Error       *analyzeError       `json:"error,omitempty"`
}

type runSummary struct {
	Results            []outputResult `json:"results"`
	HasFindings        bool           `json:"-"`
	HasInternalFailure bool           `json:"-"`
	HasTransport       bool           `json:"-"`
}

type localDiagramParser interface {
	Parse(code string) (*model.Diagram, *parser.SyntaxError, error)
	Close() error
}

var parserNew = func(scriptPath string) (localDiagramParser, error) {
	return parser.New(scriptPath)
}

func parseArgs(args []string) (cliOptions, error) {
	fs := flag.NewFlagSet("merm8-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := cliOptions{}
	fs.BoolVar(&opts.UseStdin, "stdin", false, "Read Mermaid code from stdin.")
	fs.StringVar(&opts.Format, "format", "text", "Output format: text|json")
	fs.StringVar(&opts.URL, "url", "", "Server base URL. When set, analyze via HTTP API.")
	fs.StringVar(&opts.ConfigPath, "config", "", "Path to lint config JSON file.")
	fs.BoolVar(&opts.FailOnLint, "fail-on-lint", false, "Exit non-zero when lint issues are found.")
	fs.BoolVar(&opts.FailOnSyntax, "fail-on-syntax", true, "Exit non-zero on syntax errors.")
	fs.DurationVar(&opts.Timeout, "timeout", 10*time.Second, "HTTP timeout when --url is set.")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	opts.Files = fs.Args()
	opts.Format = strings.ToLower(strings.TrimSpace(opts.Format))
	if opts.Format != "text" && opts.Format != "json" {
		return cliOptions{}, fmt.Errorf("unsupported --format %q (expected text or json)", opts.Format)
	}
	if !opts.UseStdin && len(opts.Files) == 0 {
		opts.UseStdin = true
	}
	return opts, nil
}

func main() {
	exitCode := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(exitCode)
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "argument error: %v\n", err)
		return exitInternal
	}

	cfgRaw, cfg, err := readConfig(opts.ConfigPath)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return exitInternal
	}

	inputs, err := collectInputs(opts)
	if err != nil {
		fmt.Fprintf(stderr, "input error: %v\n", err)
		return exitInternal
	}

	summary := runSummary{Results: make([]outputResult, 0, len(inputs))}

	var localParser localDiagramParser
	var eng *engine.Engine
	if opts.URL == "" {
		scriptPath := os.Getenv("PARSER_SCRIPT")
		if strings.TrimSpace(scriptPath) == "" {
			scriptPath = filepath.Join(".", "parser-node", "parse.mjs")
		}
		localParser, err = parserNew(scriptPath)
		if err != nil {
			fmt.Fprintf(stderr, "parser init error: %v\n", err)
			return exitInternal
		}
		defer func() {
			if closeErr := localParser.Close(); closeErr != nil {
				fmt.Fprintf(stderr, "parser close error: %v\n", closeErr)
			}
		}()
		eng = engine.New()
		if _, err := eng.NormalizeConfig(cfg); err != nil {
			fmt.Fprintf(stderr, "config validation error: %v\n", err)
			return exitInternal
		}
	}

	for _, in := range inputs {
		var result outputResult
		if opts.URL != "" {
			result, err = analyzeRemote(in.name, in.code, cfgRaw, opts)
			if err != nil {
				summary.HasTransport = true
				result = outputResult{Input: in.name, Valid: false, Supported: false, SyntaxError: nil, Issues: []model.Issue{}, Error: &analyzeError{Code: "transport_error", Message: err.Error()}}
			}
		} else {
			result = analyzeLocal(in.name, in.code, cfg, localParser, eng)
		}

		summary.Results = append(summary.Results, result)
		if isSyntaxFailure(result) && opts.FailOnSyntax {
			summary.HasFindings = true
		}
		if len(result.Issues) > 0 && opts.FailOnLint {
			summary.HasFindings = true
		}
		if result.Error != nil && result.Error.Code != "unsupported_diagram_type" && result.Error.Code != "transport_error" {
			summary.HasInternalFailure = true
		}
	}

	if err := emitOutput(summary.Results, opts.Format, stdout); err != nil {
		fmt.Fprintf(stderr, "output error: %v\n", err)
		return exitInternal
	}
	return chooseExitCode(summary)
}

type inputCode struct {
	name string
	code string
}

func collectInputs(opts cliOptions) ([]inputCode, error) {
	inputs := make([]inputCode, 0, len(opts.Files)+1)
	if opts.UseStdin {
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, inputCode{name: "stdin", code: string(buf)})
	}
	for _, path := range opts.Files {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		inputs = append(inputs, inputCode{name: path, code: string(content)})
	}
	return inputs, nil
}

func analyzeLocal(name, code string, cfg rules.Config, p localDiagramParser, eng *engine.Engine) outputResult {
	diagram, syntaxErr, err := p.Parse(code)
	if err != nil {
		return outputResult{Input: name, Valid: false, Supported: false, SyntaxError: nil, Issues: []model.Issue{}, Error: &analyzeError{Code: "internal_error", Message: err.Error()}}
	}
	if syntaxErr != nil {
		return outputResult{Input: name, Valid: false, Supported: false, SyntaxError: syntaxErr, Issues: []model.Issue{}}
	}

	issues := eng.Run(diagram, cfg)
	if len(issues) == 1 && issues[0].RuleID == "unsupported-diagram-type" {
		return outputResult{
			Input:       name,
			Valid:       false,
			DiagramType: diagram.Type,
			Supported:   false,
			SyntaxError: nil,
			Issues:      []model.Issue{},
			Error:       &analyzeError{Code: "unsupported_diagram_type", Message: issues[0].Message},
		}
	}

	return outputResult{Input: name, Valid: true, DiagramType: diagram.Type, Supported: true, SyntaxError: nil, Issues: issues}
}

func analyzeRemote(name, code string, cfgRaw json.RawMessage, opts cliOptions) (outputResult, error) {
	payload := analyzeRequest{Code: code, Config: cfgRaw}
	body, err := json.Marshal(payload)
	if err != nil {
		return outputResult{}, err
	}
	endpoint := strings.TrimRight(opts.URL, "/")
	if !strings.HasSuffix(endpoint, "/analyze") {
		endpoint += "/analyze"
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return outputResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return outputResult{}, err
	}
	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return outputResult{}, err
	}

	var result outputResult
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return outputResult{
				Input:       name,
				Valid:       false,
				Supported:   false,
				SyntaxError: nil,
				Issues:      []model.Issue{},
				Error: &analyzeError{
					Code:    "invalid_response",
					Message: fmt.Sprintf("remote server returned HTTP %d with invalid JSON body: %v", resp.StatusCode, err),
				},
			}, nil
		}
		result.Input = name
		if result.Issues == nil {
			result.Issues = []model.Issue{}
		}
		return result, nil
	}

	apiErr := analyzeError{
		Code:    "http_error",
		Message: strings.TrimSpace(string(bodyBytes)),
	}
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(bodyBytes, &envelope); err == nil && envelope.Error != nil {
		if strings.TrimSpace(envelope.Error.Code) != "" {
			apiErr.Code = envelope.Error.Code
		}
		if strings.TrimSpace(envelope.Error.Message) != "" {
			apiErr.Message = envelope.Error.Message
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}

	return outputResult{
		Input:       name,
		Valid:       false,
		Supported:   false,
		SyntaxError: nil,
		Issues:      []model.Issue{},
		Error: &analyzeError{
			Code:    apiErr.Code,
			Message: fmt.Sprintf("remote server returned HTTP %d: %s", resp.StatusCode, apiErr.Message),
		},
	}, nil
}

func emitOutput(results []outputResult, format string, w io.Writer) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"results": results})
	}
	for _, result := range results {
		fmt.Fprintf(w, "%s: ", result.Input)
		switch {
		case result.Error != nil:
			fmt.Fprintf(w, "error (%s): %s\n", result.Error.Code, result.Error.Message)
		case result.SyntaxError != nil:
			fmt.Fprintf(w, "syntax error at %d:%d: %s\n", result.SyntaxError.Line, result.SyntaxError.Column, result.SyntaxError.Message)
		case len(result.Issues) == 0:
			fmt.Fprintln(w, "ok")
		default:
			fmt.Fprintf(w, "%d lint issue(s)\n", len(result.Issues))
			for _, issue := range result.Issues {
				fmt.Fprintf(w, "  - [%s] %s: %s\n", issue.Severity, issue.RuleID, issue.Message)
			}
		}
	}
	return nil
}

func chooseExitCode(summary runSummary) int {
	if summary.HasTransport {
		return exitTransport
	}
	if summary.HasInternalFailure {
		return exitInternal
	}
	if summary.HasFindings {
		return exitFindings
	}
	return exitOK
}

func isSyntaxFailure(result outputResult) bool {
	if result.SyntaxError != nil {
		return true
	}
	return result.Valid == false && result.Error == nil
}

func readConfig(path string) (json.RawMessage, rules.Config, error) {
	if strings.TrimSpace(path) == "" {
		return nil, rules.Config{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	raw := json.RawMessage(b)
	cfg, err := parseConfigFile(raw)
	if err != nil {
		return nil, nil, err
	}
	return raw, cfg, nil
}

func parseConfigFile(raw json.RawMessage) (rules.Config, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if version, ok := payload["schema-version"]; ok {
		if version != rules.CurrentConfigSchemaVersion {
			return nil, fmt.Errorf("unsupported schema-version %v", version)
		}
		rulesValue, ok := payload["rules"]
		if !ok {
			return nil, errors.New("config.rules is required when schema-version is set")
		}
		return decodeRulesObject(rulesValue)
	}
	if rulesValue, ok := payload["rules"]; ok {
		return decodeRulesObject(rulesValue)
	}

	if err := rejectNestedConfigStructure(payload); err != nil {
		return nil, err
	}

	var cfg rules.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	return canonicalizeConfigRuleKeys(cfg), nil
}

func decodeRulesObject(rulesValue any) (rules.Config, error) {
	rulesMap, ok := rulesValue.(map[string]any)
	if !ok {
		return nil, errors.New("config.rules must be an object")
	}

	if err := rejectNestedConfigStructure(rulesMap); err != nil {
		return nil, err
	}

	rawRules, err := json.Marshal(rulesMap)
	if err != nil {
		return nil, err
	}
	var cfg rules.Config
	if err := json.Unmarshal(rawRules, &cfg); err != nil {
		return nil, err
	}

	return canonicalizeConfigRuleKeys(cfg), nil
}

func rejectNestedConfigStructure(data map[string]any) error {
	for ruleID, rawRuleConfig := range data {
		ruleConfig, ok := rawRuleConfig.(map[string]any)
		if !ok {
			continue
		}
		for optionKey, optionValue := range ruleConfig {
			if hasNestedStructure(optionValue) {
				return fmt.Errorf("config rule %q option %q must not contain nested objects", ruleID, optionKey)
			}
		}
	}
	return nil
}

func hasNestedStructure(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return true
	case []any:
		for _, item := range typed {
			if hasNestedStructure(item) {
				return true
			}
		}
	}
	return false
}

func canonicalizeConfigRuleKeys(cfg rules.Config) rules.Config {
	if cfg == nil {
		return nil
	}

	canonical := make(rules.Config, len(cfg))

	// First pass: apply canonical keys and clone options.
	for _, ruleID := range sortedRuleIDs(cfg) {
		if strings.HasPrefix(ruleID, "core/") {
			continue
		}
		canonical[ruleID] = cloneRuleConfig(cfg[ruleID])
	}

	// Second pass: apply prefixed aliases only when canonical key is absent,
	// otherwise only backfill missing option keys.
	for _, ruleID := range sortedRuleIDs(cfg) {
		if !strings.HasPrefix(ruleID, "core/") {
			continue
		}

		canonicalRuleID := strings.TrimPrefix(ruleID, "core/")
		ruleCfg := cfg[ruleID]
		existing, exists := canonical[canonicalRuleID]
		if !exists {
			canonical[canonicalRuleID] = cloneRuleConfig(ruleCfg)
			continue
		}

		for optionKey, optionValue := range ruleCfg {
			if _, alreadySet := existing[optionKey]; alreadySet {
				continue
			}
			existing[optionKey] = optionValue
		}
	}

	return canonical
}

func sortedRuleIDs(cfg rules.Config) []string {
	keys := make([]string, 0, len(cfg))
	for key := range cfg {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneRuleConfig(ruleCfg map[string]any) map[string]any {
	if ruleCfg == nil {
		return nil
	}
	clone := make(map[string]any, len(ruleCfg))
	for optionKey, optionValue := range ruleCfg {
		clone[optionKey] = optionValue
	}
	return clone
}
