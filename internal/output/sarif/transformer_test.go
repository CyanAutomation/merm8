package sarif

import (
	"encoding/json"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
)

func intptr(v int) *int { return &v }

func TestTransform_MapsSeverityRuleAndLocation(t *testing.T) {
	issues := []model.Issue{
		{RuleID: "no-cycles", Severity: "error", Message: "cycle", Line: intptr(7), Column: intptr(3), Fingerprint: "fp-no-cycles"},
		{RuleID: "max-fanout", Severity: "warn", Message: "fanout", Line: intptr(2)},
		{RuleID: "info-rule", Severity: "info", Message: "note"},
	}
	report := Transform(issues, RequestMetadata{RequestURI: "/analyze/sarif", ArtifactURI: "request://analyze"})
	if report.Version != "2.1.0" {
		t.Fatalf("unexpected version: %s", report.Version)
	}
	results := report.Runs[0].Results
	if results[0].RuleID != "no-cycles" || results[0].Level != "error" {
		t.Fatalf("unexpected first result: %#v", results[0])
	}
	if len(results[0].Locations) != 1 || results[0].Locations[0].PhysicalLocation.Region.StartLine != 7 || results[0].Locations[0].PhysicalLocation.Region.StartColumn != 3 {
		t.Fatalf("unexpected location mapping: %#v", results[0].Locations)
	}
	if got := results[0].PartialFingerprints["issueFingerprint"]; got != "fp-no-cycles" {
		t.Fatalf("expected fingerprint propagation, got %q", got)
	}
	if results[1].Level != "warning" {
		t.Fatalf("warn should map to warning, got %q", results[1].Level)
	}
	if results[2].Level != "note" {
		t.Fatalf("info should map to note, got %q", results[2].Level)
	}
	if results[0].Message.Text != "cycle" {
		t.Fatalf("expected message to remain unchanged, got %q", results[0].Message.Text)
	}
	if report.Runs[0].Tool.Driver.Rules[0].ID != "no-cycles" {
		t.Fatalf("expected rule ID propagation")
	}
}

func TestTransform_OmitsArtifactsWhenNoResults(t *testing.T) {
	report := Transform(nil, RequestMetadata{RequestURI: "/analyze/sarif"})
	if got := len(report.Runs); got != 1 {
		t.Fatalf("expected one run, got %d", got)
	}
	run := report.Runs[0]
	if run.Tool.Driver.Name == "" {
		t.Fatalf("expected SARIF run to include tool metadata")
	}
	if run.Tool.Driver.InformationURI == "" {
		t.Fatalf("expected SARIF run to include tool information URI")
	}
	if report.Runs[0].Artifacts != nil {
		t.Fatalf("expected artifacts to be omitted when no results, got %#v", report.Runs[0].Artifacts)
	}
	if got := len(run.Results); got != 0 {
		t.Fatalf("expected no results, got %d", got)
	}
	if got := len(run.Invocations); got != 1 {
		t.Fatalf("expected invocation metadata even when there are no findings, got %d", got)
	}
	if !run.Invocations[0].ExecutionSuccessful {
		t.Fatalf("expected invocation to be marked executionSuccessful for successful analysis")
	}
	if got := run.Invocations[0].Properties["request-uri"]; got != "/analyze/sarif" {
		t.Fatalf("expected request-uri property to be preserved, got %q", got)
	}
	if _, ok := run.Invocations[0].Properties["error-code"]; ok {
		t.Fatalf("did not expect error-code property for no-findings success case")
	}

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	runs, ok := decoded["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("expected one serialized run, got %#v", decoded["runs"])
	}
	runJSON, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("expected run object in serialized SARIF, got %#v", runs[0])
	}
	if _, ok := runJSON["artifacts"]; ok {
		t.Fatalf("expected artifacts key to be omitted when no findings, got %#v", runJSON["artifacts"])
	}
	resultsJSON, ok := runJSON["results"].([]any)
	if !ok {
		t.Fatalf("expected results key to serialize as array, got %#v", runJSON["results"])
	}
	if len(resultsJSON) != 0 {
		t.Fatalf("expected serialized results to be an empty array, got %#v", resultsJSON)
	}
}
