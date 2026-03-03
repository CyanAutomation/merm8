package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

	var localParser *parser.Parser
	var eng *engine.Engine
	if opts.URL == "" {
		scriptPath := os.Getenv("PARSER_SCRIPT")
		if strings.TrimSpace(scriptPath) == "" {
			scriptPath = filepath.Join(".", "parser-node", "parse.mjs")
		}
		localParser, err = parser.New(scriptPath)
		if err != nil {
			fmt.Fprintf(stderr, "parser init error: %v\n", err)
			return exitInternal
		}
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

func analyzeLocal(name, code string, cfg rules.Config, p *parser.Parser, eng *engine.Engine) outputResult {
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
	resp, err := client.Do(req)
	if err != nil {
		return outputResult{}, err
	}
	defer resp.Body.Close()

	var result outputResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return outputResult{}, err
	}
	result.Input = name
	if result.Issues == nil {
		result.Issues = []model.Issue{}
	}
	if resp.StatusCode >= 500 {
		result.Error = &analyzeError{Code: "server_error", Message: fmt.Sprintf("remote server returned HTTP %d", resp.StatusCode)}
	}
	return result, nil
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
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return nil, errors.New("config.rules must be an object")
		}
		rawRules, err := json.Marshal(rulesMap)
		if err != nil {
			return nil, err
		}
		var cfg rules.Config
		if err := json.Unmarshal(rawRules, &cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if rulesValue, ok := payload["rules"]; ok {
		rulesMap, ok := rulesValue.(map[string]any)
		if !ok {
			return nil, errors.New("config.rules must be an object")
		}
		rawRules, err := json.Marshal(rulesMap)
		if err != nil {
			return nil, err
		}
		var cfg rules.Config
		if err := json.Unmarshal(rawRules, &cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	var cfg rules.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
