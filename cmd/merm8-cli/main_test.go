package main

import (
	"encoding/json"
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
	if _, err := parseArgs([]string{"--format", "xml"}); err == nil {
		t.Fatalf("expected format validation error")
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
