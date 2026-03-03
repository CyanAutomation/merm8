package sarif

import (
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
)

func intptr(v int) *int { return &v }

func TestTransform_MapsSeverityRuleAndLocation(t *testing.T) {
	issues := []model.Issue{
		{RuleID: "no-cycles", Severity: "error", Message: "cycle", Line: intptr(7), Column: intptr(3)},
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
	if results[1].Level != "warning" {
		t.Fatalf("warn should map to warning, got %q", results[1].Level)
	}
	if results[2].Level != "note" {
		t.Fatalf("info should map to note, got %q", results[2].Level)
	}
	if report.Runs[0].Tool.Driver.Rules[0].ID != "no-cycles" {
		t.Fatalf("expected rule ID propagation")
	}
}
