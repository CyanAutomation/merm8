package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestParseArgsDefaultsToStdin(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantStdin  bool
		wantFormat string
		wantConfig string
	}{
		{
			name:       "no args defaults to stdin",
			args:       []string{},
			wantStdin:  true,
			wantFormat: "text",
			wantConfig: "",
		},
		{
			name:       "positional file disables stdin",
			args:       []string{"diagram.mmd"},
			wantStdin:  false,
			wantFormat: "text",
			wantConfig: "",
		},
		{
			name:       "explicit stdin flag with file",
			args:       []string{"--stdin", "diagram.mmd"},
			wantStdin:  true,
			wantFormat: "text",
			wantConfig: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs error: %v", err)
			}

			if opts.UseStdin != tt.wantStdin {
				t.Fatalf("expected UseStdin=%t, got %t", tt.wantStdin, opts.UseStdin)
			}
			if opts.Format != tt.wantFormat {
				t.Fatalf("expected Format=%q, got %q", tt.wantFormat, opts.Format)
			}
			if opts.ConfigPath != tt.wantConfig {
				t.Fatalf("expected ConfigPath=%q, got %q", tt.wantConfig, opts.ConfigPath)
			}
			if opts.URL != "" {
				t.Fatalf("expected URL to default to empty, got %q", opts.URL)
			}
			if opts.Timeout != 10*time.Second {
				t.Fatalf("expected Timeout=%s, got %s", 10*time.Second, opts.Timeout)
			}
		})
	}
}

func TestParseArgsFormatValidation(t *testing.T) {
	t.Parallel()

	validTests := []struct {
		name       string
		formatFlag string
		wantFormat string
	}{
		{name: "text preserved", formatFlag: "text", wantFormat: "text"},
		{name: "json preserved", formatFlag: "json", wantFormat: "json"},
		{name: "text normalized to lower", formatFlag: "TEXT", wantFormat: "text"},
		{name: "json normalized and trimmed", formatFlag: " Json ", wantFormat: "json"},
	}

	for _, tt := range validTests {
		tt := tt
		t.Run("valid_"+tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseArgs([]string{"--format", tt.formatFlag})
			if err != nil {
				t.Fatalf("parseArgs error: %v", err)
			}
			if opts.Format != tt.wantFormat {
				t.Fatalf("expected normalized format %q, got %q", tt.wantFormat, opts.Format)
			}
		})
	}

	invalidFormats := []string{"xml", "yaml", "", "  "}
	for _, invalidFormat := range invalidFormats {
		invalidFormat := invalidFormat
		t.Run("invalid_"+invalidFormat, func(t *testing.T) {
			t.Parallel()

			_, err := parseArgs([]string{"--format", invalidFormat})
			if err == nil {
				t.Fatalf("expected format validation error for format %q", invalidFormat)
			}

			errText := err.Error()
			if !strings.Contains(errText, "unsupported --format") {
				t.Fatalf("expected error to mention unsupported format, got %q", errText)
			}
			if !strings.Contains(errText, "text or json") {
				t.Fatalf("expected error to mention allowed formats, got %q", errText)
			}
		})
	}
}

func TestRunExitCodesFromPublicCLIBehavior(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		responseCode int
		responseBody string
		wantExitCode int
	}{
		{
			name:         "ok output exits zero",
			args:         []string{"--stdin"},
			responseCode: http.StatusOK,
			responseBody: `{"valid":true,"lint-supported":true,"issues":[]}`,
			wantExitCode: exitOK,
		},
		{
			name:         "lint findings exit one when fail-on-lint is set",
			args:         []string{"--stdin", "--fail-on-lint"},
			responseCode: http.StatusOK,
			responseBody: `{"valid":true,"lint-supported":true,"issues":[{"rule-id":"max-fanout","message":"too many","severity":"warning"}]}`,
			wantExitCode: exitFindings,
		},
		{
			name:         "internal failures exit two",
			args:         []string{"--stdin"},
			responseCode: http.StatusInternalServerError,
			responseBody: `{"valid":false,"lint-supported":true,"issues":[]}`,
			wantExitCode: exitInternal,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.responseCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			t.Cleanup(testServer.Close)

			args := append([]string{"--url", testServer.URL}, tt.args...)
			exitCode := runWithStdin(t, args, "graph TD; A-->B")
			if exitCode != tt.wantExitCode {
				t.Fatalf("expected exit code %d, got %d", tt.wantExitCode, exitCode)
			}
		})
	}

	t.Run("transport failures exit three", func(t *testing.T) {
		exitCode := runWithStdin(t, []string{"--url", "http://127.0.0.1:1", "--stdin", "--timeout", "100ms"}, "graph TD; A-->B")
		if exitCode != exitTransport {
			t.Fatalf("expected exit code %d, got %d", exitTransport, exitCode)
		}
	})
}

func TestChooseExitCodePriority(t *testing.T) {
	tests := []struct {
		name    string
		summary runSummary
		want    int
	}{
		{name: "findings", summary: runSummary{HasFindings: true}, want: exitFindings},
		{name: "internal overrides findings", summary: runSummary{HasFindings: true, HasInternalFailure: true}, want: exitInternal},
		{name: "transport overrides internal", summary: runSummary{HasFindings: true, HasInternalFailure: true, HasTransport: true}, want: exitTransport},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseExitCode(tt.summary); got != tt.want {
				t.Fatalf("expected exit code %d, got %d", tt.want, got)
			}
		})
	}
}

func runWithStdin(t *testing.T, args []string, stdinContent string) int {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	if _, err := writer.WriteString(stdinContent); err != nil {
		t.Fatalf("write stdin content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}

	originalStdin := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = originalStdin
		_ = reader.Close()
	})

	return run(args, &bytes.Buffer{}, &bytes.Buffer{})
}

func TestParseConfigFileVersionedShape(t *testing.T) {
	tests := []struct {
		name            string
		flat            json.RawMessage
		versioned       json.RawMessage
		wantRuleKey     string
		wantLimit       float64
		wantErrContains []string
	}{
		{
			name:        "flat and versioned parse to equivalent config",
			flat:        json.RawMessage(`{"max-fanout":{"limit":2}}`),
			versioned:   json.RawMessage(`{"schema-version":"v1","rules":{"max-fanout":{"limit":2}}}`),
			wantRuleKey: "max-fanout",
			wantLimit:   2,
		},
		{
			name:        "canonicalizes built-in namespaced rule key",
			flat:        json.RawMessage(`{"core/max-fanout":{"limit":2}}`),
			versioned:   json.RawMessage(`{"schema-version":"v1","rules":{"core/max-fanout":{"limit":2}}}`),
			wantRuleKey: "max-fanout",
			wantLimit:   2,
		},
		{
			name:            "rejects unknown nested structure with stable error",
			flat:            json.RawMessage(`{"max-fanout":{"limit":{"value":2}}}`),
			versioned:       json.RawMessage(`{"schema-version":"v1","rules":{"max-fanout":{"limit":{"value":2}}}}`),
			wantErrContains: []string{"config rule \"max-fanout\" option \"limit\" must not contain nested objects"},
		},
		{
			name:            "rejects unsupported schema version",
			versioned:       json.RawMessage(`{"schema-version":"v2","rules":{}}`),
			wantErrContains: []string{"schema-version", "v2"},
		},
	}

	assertParsed := func(t *testing.T, cfg rules.Config, wantRuleKey string, wantLimit float64) {
		t.Helper()
		ruleCfg, ok := cfg[wantRuleKey]
		if !ok {
			t.Fatalf("expected %q rule in parsed config; got keys: %#v", wantRuleKey, cfg)
		}
		if _, hasLegacyCoreKey := cfg["core/max-fanout"]; hasLegacyCoreKey {
			t.Fatalf("expected canonical built-in key normalization, found unexpected legacy key: %#v", cfg)
		}
		gotLimit, ok := ruleCfg["limit"].(float64)
		if !ok {
			t.Fatalf("expected %q.limit to decode as float64, got %#v", wantRuleKey, ruleCfg["limit"])
		}
		if gotLimit != wantLimit {
			t.Fatalf("expected %q.limit=%v, got %v", wantRuleKey, wantLimit, gotLimit)
		}
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			inputs := []struct {
				name string
				raw  json.RawMessage
			}{
				{name: "flat", raw: tt.flat},
				{name: "versioned", raw: tt.versioned},
			}

			var baseline rules.Config
			for _, input := range inputs {
				if len(input.raw) == 0 {
					continue
				}
				cfg, err := parseConfigFile(input.raw)

				if len(tt.wantErrContains) > 0 {
					if err == nil {
						t.Fatalf("expected parseConfigFile error for %s input", input.name)
					}
					errText := err.Error()
					for _, want := range tt.wantErrContains {
						if !strings.Contains(errText, want) {
							t.Fatalf("expected %s error %q to contain %q", input.name, errText, want)
						}
					}
					continue
				}

				if err != nil {
					t.Fatalf("parseConfigFile(%s) error: %v", input.name, err)
				}
				assertParsed(t, cfg, tt.wantRuleKey, tt.wantLimit)

				if baseline == nil {
					baseline = cfg
					continue
				}
				if !reflect.DeepEqual(baseline, cfg) {
					t.Fatalf("expected equivalent parsed output, baseline=%#v, got=%#v", baseline, cfg)
				}
			}
		})
	}
}
