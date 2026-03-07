package main

import (
	"encoding/json"
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

func TestChooseExitCodePriority(t *testing.T) {
	if code := chooseExitCode(runSummary{HasFindings: true}); code != exitFindings {
		t.Fatalf("expected findings code %d, got %d", exitFindings, code)
	}
	if code := chooseExitCode(runSummary{HasFindings: true, HasInternalFailure: true}); code != exitInternal {
		t.Fatalf("expected internal code %d, got %d", exitInternal, code)
	}
	if code := chooseExitCode(runSummary{HasFindings: true, HasInternalFailure: true, HasTransport: true}); code != exitTransport {
		t.Fatalf("expected transport code %d, got %d", exitTransport, code)
	}
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
