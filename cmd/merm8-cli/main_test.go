package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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
	t.Parallel()

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
			t.Parallel()

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
		t.Parallel()

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
	raw := json.RawMessage(`{"schema-version":"v1","rules":{"max-fanout":{"limit":2}}}`)
	cfg, err := parseConfigFile(raw)
	if err != nil {
		t.Fatalf("parseConfigFile error: %v", err)
	}
	if _, ok := cfg["max-fanout"]; !ok {
		t.Fatalf("expected max-fanout rule in parsed config")
	}
}

func TestParseConfigFileRejectsUnsupportedSchemaVersion(t *testing.T) {
	raw := json.RawMessage(`{"schema-version":"v2","rules":{}}`)
	if _, err := parseConfigFile(raw); err == nil {
		t.Fatalf("expected schema-version error")
	}
}
