package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

func loadServedSpec(t *testing.T) map[string]interface{} {
	t.Helper()

	mux := http.NewServeMux()
	h := api.NewHandler(&mockParser{}, engine.New())
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/spec", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected /spec 200, got %d", w.Code)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to decode /spec JSON: %v", err)
	}
	return spec
}

func repoFilePath(t *testing.T, name string) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	for {
		candidate := filepath.Join(cwd, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}
	t.Fatalf("failed to locate %s from test cwd", name)
	return ""
}

func loadOpenAPIJSON(t *testing.T) map[string]interface{} {
	t.Helper()

	data, err := os.ReadFile(repoFilePath(t, "openapi.json"))
	if err != nil {
		t.Fatalf("failed to read openapi.json: %v", err)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("failed to decode openapi.json: %v", err)
	}
	return spec
}

func lookup(t *testing.T, v interface{}, path ...string) interface{} {
	t.Helper()
	cur := v
	for _, p := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("path %v: expected object at %q, got %T", path, p, cur)
		}
		next, ok := m[p]
		if !ok {
			t.Fatalf("path %v: missing key %q", path, p)
		}
		cur = next
	}
	return cur
}

func TestServeSpec_HasRequiredOpenAPIFieldsAndRefs(t *testing.T) {
	spec := loadServedSpec(t)

	if got := lookup(t, spec, "openapi"); got != "3.0.0" {
		t.Fatalf("expected openapi version 3.0.0, got %#v", got)
	}

	if got := lookup(t, spec, "info", "title"); got == "" {
		t.Fatal("expected non-empty info.title")
	}
	if got := lookup(t, spec, "paths", "/analyze", "post", "operationId"); got != "analyzeCode" {
		t.Fatalf("expected analyze operationId analyzeCode, got %#v", got)
	}

	requiredRefs := []struct {
		name string
		path []string
		ref  string
	}{
		{
			name: "Analyze request schema",
			path: []string{"paths", "/analyze", "post", "requestBody", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeRequest",
		},
		{
			name: "Analyze 200 response schema",
			path: []string{"paths", "/analyze", "post", "responses", "200", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeResponse",
		},
		{
			name: "Analyze 400 response schema",
			path: []string{"paths", "/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/ErrorResponse",
		},
		{
			name: "Analyze 500 response schema",
			path: []string{"paths", "/analyze", "post", "responses", "500", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/ErrorResponse",
		},
	}

	for _, tc := range requiredRefs {
		t.Run(tc.name, func(t *testing.T) {
			if got := lookup(t, spec, tc.path...).(string); got != tc.ref {
				t.Fatalf("expected %s, got %s", tc.ref, got)
			}
		})
	}
}

func TestServeSpec_AnalyzeExamplesMatchExpectedShape(t *testing.T) {
	spec := loadServedSpec(t)

	examples200 := lookup(t, spec, "paths", "/analyze", "post", "responses", "200", "content", "application/json", "examples").(map[string]interface{})
	validDiagram := lookup(t, examples200, "validDiagram", "value").(map[string]interface{})
	if validDiagram["valid"] != true {
		t.Fatalf("expected validDiagram.valid=true, got %#v", validDiagram["valid"])
	}
	if _, ok := validDiagram["issues"].([]interface{}); !ok {
		t.Fatalf("expected validDiagram.issues array, got %T", validDiagram["issues"])
	}
	metrics := lookup(t, validDiagram, "metrics").(map[string]interface{})
	for _, metric := range []string{"node_count", "edge_count", "disconnected_node_count", "duplicate_node_count", "max_fanin", "max_fanout"} {
		if _, ok := metrics[metric].(float64); !ok {
			t.Fatalf("expected validDiagram.metrics.%s number, got %#v", metric, metrics[metric])
		}
	}
	if metrics["diagram_type"] != "flowchart" {
		t.Fatalf("expected validDiagram.metrics.diagram_type=flowchart, got %#v", metrics["diagram_type"])
	}
	if metrics["direction"] != "TD" {
		t.Fatalf("expected validDiagram.metrics.direction=TD, got %#v", metrics["direction"])
	}
	if _, ok := lookup(t, metrics, "issue_counts", "by_severity").(map[string]interface{}); !ok {
		t.Fatalf("expected issue_counts.by_severity object, got %T", lookup(t, metrics, "issue_counts", "by_severity"))
	}
	if _, ok := lookup(t, metrics, "issue_counts", "by_rule").(map[string]interface{}); !ok {
		t.Fatalf("expected issue_counts.by_rule object, got %T", lookup(t, metrics, "issue_counts", "by_rule"))
	}

	examples400 := lookup(t, spec, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "examples").(map[string]interface{})
	missingCode := lookup(t, examples400, "missingCode", "value").(map[string]interface{})
	assertErrorShape(t, missingCode, "missing_code")
	unknownRule := lookup(t, examples400, "unknownRule", "value").(map[string]interface{})
	assertErrorShape(t, unknownRule, "unknown_rule")
	if path := lookup(t, unknownRule, "error", "path").(string); path != "config.rules.unknown-rule" {
		t.Fatalf("expected unknownRule path, got %q", path)
	}

	examples500 := lookup(t, spec, "paths", "/analyze", "post", "responses", "500", "content", "application/json", "examples").(map[string]interface{})
	for name, code := range map[string]string{
		"subprocess": "parser_subprocess_error",
		"decode":     "parser_decode_error",
		"contract":   "parser_contract_violation",
		"internal":   "internal_error",
	} {
		assertErrorShape(t, lookup(t, examples500, name, "value").(map[string]interface{}), code)
	}
}

func assertErrorShape(t *testing.T, payload map[string]interface{}, expectedCode string) {
	t.Helper()
	if payload["valid"] != false {
		t.Fatalf("expected valid=false, got %#v", payload["valid"])
	}
	issues, ok := payload["issues"].([]interface{})
	if !ok || len(issues) != 0 {
		t.Fatalf("expected empty issues array, got %#v", payload["issues"])
	}
	code := lookup(t, payload, "error", "code")
	if code != expectedCode {
		t.Fatalf("expected error.code=%q, got %#v", expectedCode, code)
	}
	if msg := lookup(t, payload, "error", "message"); msg == "" {
		t.Fatal("expected non-empty error.message")
	}
}

func TestOpenAPIDrift_SelectedFieldsStayInSync(t *testing.T) {
	servedSpec := loadServedSpec(t)
	jsonSpec := loadOpenAPIJSON(t)
	yamlBytes, err := os.ReadFile(repoFilePath(t, "openapi.yaml"))
	if err != nil {
		t.Fatalf("failed to read openapi.yaml: %v", err)
	}
	yamlSpec := string(yamlBytes)

	selectedPaths := [][]string{
		{"openapi"},
		{"info", "title"},
		{"paths", "/analyze", "post", "operationId"},
		{"paths", "/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"},
		{"components", "schemas", "Issue", "properties", "severity", "enum"},
		{"paths", "/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfig", "value", "config", "rules", "max-fanout", "severity"},
	}

	for _, p := range selectedPaths {
		name := strings.Join(p, ".")
		t.Run(name, func(t *testing.T) {
			served := lookup(t, servedSpec, p...)
			fromJSON := lookup(t, jsonSpec, p...)
			if fmt.Sprintf("%v", served) != fmt.Sprintf("%v", fromJSON) {
				t.Fatalf("drift between served /spec and openapi.json at %s: served=%#v json=%#v", name, served, fromJSON)
			}
		})
	}

	for _, snippet := range []string{
		"openapi: 3.0.0",
		"/analyze:",
		"operationId: analyzeCode",
		"$ref: '#/components/schemas/ErrorResponse'",
		"- error",
		"- warn",
		"- info",
		"severity: error",
	} {
		if !strings.Contains(yamlSpec, snippet) {
			t.Fatalf("openapi.yaml missing drift-check snippet: %q", snippet)
		}
	}
}

func TestServeSpec_IssueLocationFieldsAreOptionalAndNullable(t *testing.T) {
	spec := loadServedSpec(t)

	requiredRaw := lookup(t, spec, "components", "schemas", "Issue", "required").([]interface{})
	required := make([]string, 0, len(requiredRaw))
	for _, v := range requiredRaw {
		required = append(required, v.(string))
	}
	for _, field := range []string{"line", "column"} {
		if slices.Contains(required, field) {
			t.Fatalf("expected %q to be optional in Issue schema, got required=%#v", field, required)
		}
		nullable, ok := lookup(t, spec, "components", "schemas", "Issue", "properties", field, "nullable").(bool)
		if !ok || !nullable {
			t.Fatalf("expected %q to be nullable=true in Issue schema, got %#v", field, lookup(t, spec, "components", "schemas", "Issue", "properties", field, "nullable"))
		}
	}
}

func TestServeSpec_Regression_ConfigValidationAndSeverityExamples(t *testing.T) {
	spec := loadServedSpec(t)

	withConfig := lookup(t, spec, "paths", "/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfig", "value").(map[string]interface{})
	severity := lookup(t, withConfig, "config", "rules", "max-fanout", "severity")
	if severity != "error" {
		t.Fatalf("expected severity override example to be error, got %#v", severity)
	}

	selectors, ok := lookup(t, withConfig, "config", "rules", "max-fanout", "suppression_selectors").([]interface{})
	if !ok || len(selectors) == 0 {
		t.Fatalf("expected suppression_selectors example array, got %#v", lookup(t, withConfig, "config", "rules", "max-fanout", "suppression_selectors"))
	}
	if _, ok := selectors[0].(string); !ok {
		t.Fatalf("expected suppression_selectors entries to be strings, got %#v", selectors[0])
	}

	unknownOption := lookup(t, spec, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "examples", "unknownOption", "value").(map[string]interface{})
	assertErrorShape(t, unknownOption, "unknown_option")

	severityEnum := lookup(t, spec, "components", "schemas", "Issue", "properties", "severity", "enum").([]interface{})
	got := make([]string, 0, len(severityEnum))
	for _, v := range severityEnum {
		got = append(got, v.(string))
	}
	for _, expected := range []string{"error", "warn", "info"} {
		if !slices.Contains(got, expected) {
			t.Fatalf("expected severity enum to include %q, got %#v", expected, got)
		}
	}
}

// compile-time guard to ensure local mock parser still satisfies parser contract.
var _ interface {
	Parse(string) (*model.Diagram, *parser.SyntaxError, error)
} = (*mockParser)(nil)

func TestServeSpec_ExposesRulesEndpointAndSchemas(t *testing.T) {
	spec := loadServedSpec(t)
	if got := lookup(t, spec, "paths", "/rules", "get", "operationId"); got != "listRules" {
		t.Fatalf("expected /rules operationId listRules, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/rules", "get", "responses", "200", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/RulesResponse" {
		t.Fatalf("expected /rules response schema ref, got %#v", got)
	}
	_ = lookup(t, spec, "components", "schemas", "RulesResponse")
	_ = lookup(t, spec, "components", "schemas", "RuleMetadata")
	_ = lookup(t, spec, "components", "schemas", "RuleOption")
}
