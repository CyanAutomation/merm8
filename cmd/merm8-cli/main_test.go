package main

import (
	"encoding/json"
	"testing"
)

func TestParseArgsDefaultsToStdin(t *testing.T) {
	opts, err := parseArgs([]string{})
	if err != nil {
		t.Fatalf("parseArgs error: %v", err)
	}
	if !opts.UseStdin {
		t.Fatalf("expected UseStdin=true when no files are provided")
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
