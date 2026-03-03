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

	req := httptest.NewRequest(http.MethodGet, "/v1/spec", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected /v1/spec 200, got %d", w.Code)
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
	if got := lookup(t, spec, "paths", "/v1/analyze", "post", "operationId"); got != "analyzeCode" {
		t.Fatalf("expected analyze operationId analyzeCode, got %#v", got)
	}

	requiredRefs := []struct {
		name string
		path []string
		ref  string
	}{
		{
			name: "Analyze request schema",
			path: []string{"paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeRequest",
		},
		{
			name: "Analyze 200 response schema",
			path: []string{"paths", "/v1/analyze", "post", "responses", "200", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeResponse",
		},
		{
			name: "Analyze SARIF 200 response schema",
			path: []string{"paths", "/v1/analyze/sarif", "post", "responses", "200", "content", "application/sarif+json", "schema", "$ref"},
			ref:  "#/components/schemas/SARIFReport",
		},
		{
			name: "Analyze 400 response schema",
			path: []string{"paths", "/v1/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeResponse",
		},
		{
			name: "Analyze 500 response schema",
			path: []string{"paths", "/v1/analyze", "post", "responses", "500", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeResponse",
		},
		{
			name: "Analyze 504 response schema",
			path: []string{"paths", "/v1/analyze", "post", "responses", "504", "content", "application/json", "schema", "$ref"},
			ref:  "#/components/schemas/AnalyzeResponse",
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

	required := lookup(t, spec, "components", "schemas", "AnalyzeResponse", "required").([]interface{})
	requiredSet := map[string]bool{}
	for _, field := range required {
		requiredSet[field.(string)] = true
	}
	for _, field := range []string{"issues", "syntax-error", "metrics"} {
		if !requiredSet[field] {
			t.Fatalf("expected AnalyzeResponse.required to include %q, got %#v", field, required)
		}
	}

	examples200 := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "200", "content", "application/json", "examples").(map[string]interface{})
	validDiagram := lookup(t, examples200, "validDiagram", "value").(map[string]interface{})
	if validDiagram["valid"] != true {
		t.Fatalf("expected validDiagram.valid=true, got %#v", validDiagram["valid"])
	}
	if _, ok := validDiagram["issues"].([]interface{}); !ok {
		t.Fatalf("expected validDiagram.issues array, got %T", validDiagram["issues"])
	}
	if syntaxErr := lookup(t, validDiagram, "syntax-error"); syntaxErr != nil {
		t.Fatalf("expected validDiagram.syntax-error=null, got %#v", syntaxErr)
	}
	if errField := validDiagram["error"]; errField != nil {
		t.Fatalf("expected validDiagram.error to be null/omitted, got %#v", errField)
	}
	metrics := lookup(t, validDiagram, "metrics").(map[string]interface{})
	for _, metric := range []string{"node-count", "edge-count", "disconnected-node-count", "duplicate-node-count", "max-fanin", "max-fanout"} {
		if _, ok := metrics[metric].(float64); !ok {
			t.Fatalf("expected validDiagram.metrics.%s number, got %#v", metric, metrics[metric])
		}
	}
	if metrics["diagram-type"] != "flowchart" {
		t.Fatalf("expected validDiagram.metrics.diagram-type=flowchart, got %#v", metrics["diagram-type"])
	}
	if metrics["direction"] != "TD" {
		t.Fatalf("expected validDiagram.metrics.direction=TD, got %#v", metrics["direction"])
	}
	if _, ok := lookup(t, metrics, "issue-counts", "by-severity").(map[string]interface{}); !ok {
		t.Fatalf("expected issue-counts.by-severity object, got %T", lookup(t, metrics, "issue-counts", "by-severity"))
	}
	if _, ok := lookup(t, metrics, "issue-counts", "by-rule").(map[string]interface{}); !ok {
		t.Fatalf("expected issue-counts.by-rule object, got %T", lookup(t, metrics, "issue-counts", "by-rule"))
	}

	withIssues := lookup(t, examples200, "withIssues", "value").(map[string]interface{})
	if withIssues["valid"] != true {
		t.Fatalf("expected withIssues.valid=true, got %#v", withIssues["valid"])
	}
	issuesWithFindings, ok := withIssues["issues"].([]interface{})
	if !ok || len(issuesWithFindings) == 0 {
		t.Fatalf("expected withIssues.issues non-empty array, got %#v", withIssues["issues"])
	}
	if syntaxErr := lookup(t, withIssues, "syntax-error"); syntaxErr != nil {
		t.Fatalf("expected withIssues.syntax-error=null, got %#v", syntaxErr)
	}
	if errField := withIssues["error"]; errField != nil {
		t.Fatalf("expected withIssues.error to be null/omitted, got %#v", errField)
	}
	if _, ok := withIssues["metrics"].(map[string]interface{}); !ok {
		t.Fatalf("expected withIssues.metrics object, got %#v", withIssues["metrics"])
	}

	syntaxExample := lookup(t, examples200, "syntaxError", "value").(map[string]interface{})
	if syntaxExample["valid"] != false {
		t.Fatalf("expected syntaxError.valid=false, got %#v", syntaxExample["valid"])
	}
	if _, ok := lookup(t, syntaxExample, "syntax-error").(map[string]interface{}); !ok {
		t.Fatalf("expected syntaxError.syntax-error object, got %#v", lookup(t, syntaxExample, "syntax-error"))
	}
	issuesSyntax, ok := syntaxExample["issues"].([]interface{})
	if !ok || len(issuesSyntax) != 0 {
		t.Fatalf("expected syntaxError.issues empty array, got %#v", syntaxExample["issues"])
	}
	if errField := syntaxExample["error"]; errField != nil {
		t.Fatalf("expected syntaxError.error to be null/omitted, got %#v", errField)
	}
	if _, ok := syntaxExample["metrics"].(map[string]interface{}); !ok {
		t.Fatalf("expected syntaxError.metrics object, got %#v", syntaxExample["metrics"])
	}

	examples400 := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "400", "content", "application/json", "examples").(map[string]interface{})
	missingCode := lookup(t, examples400, "missingCode", "value").(map[string]interface{})
	assertErrorShape(t, missingCode, "missing_code")
	unknownRule := lookup(t, examples400, "unknownRule", "value").(map[string]interface{})
	assertErrorShape(t, unknownRule, "unknown_rule")
	if path := lookup(t, unknownRule, "error", "details", "path").(string); path != "config.rules.unknown-rule" {
		t.Fatalf("expected unknownRule details.path, got %q", path)
	}

	examples500 := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "500", "content", "application/json", "examples").(map[string]interface{})
	for name, code := range map[string]string{
		"subprocess": "parser_subprocess_error",
		"decode":     "parser_decode_error",
		"contract":   "parser_contract_violation",
		"internal":   "internal_error",
	} {
		assertErrorShape(t, lookup(t, examples500, name, "value").(map[string]interface{}), code)
	}

	example504 := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "504", "content", "application/json", "example").(map[string]interface{})
	assertErrorShape(t, example504, "parser_timeout")
}

func assertErrorShape(t *testing.T, payload map[string]interface{}, expectedCode string) {
	t.Helper()
	if payload["valid"] != false {
		t.Fatalf("expected valid=false, got %#v", payload["valid"])
	}
	if payload["lint-supported"] != false {
		t.Fatalf("expected lint-supported=false, got %#v", payload["lint-supported"])
	}
	if syntaxErr, ok := payload["syntax-error"]; !ok || syntaxErr != nil {
		t.Fatalf("expected syntax-error=null, got %#v", payload["syntax-error"])
	}
	issues, ok := payload["issues"].([]interface{})
	if !ok || len(issues) != 0 {
		t.Fatalf("expected empty issues array, got %#v", payload["issues"])
	}
	metrics, ok := payload["metrics"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics object, got %#v", payload["metrics"])
	}
	if metrics["diagram-type"] != "unknown" {
		t.Fatalf("expected metrics.diagram-type=unknown, got %#v", metrics["diagram-type"])
	}
	code := lookup(t, payload, "error", "code")
	if code != expectedCode {
		t.Fatalf("expected error.code=%q, got %#v", expectedCode, code)
	}
	if msg := lookup(t, payload, "error", "message"); msg == "" {
		t.Fatal("expected non-empty error.message")
	}
}

func TestServeSpec_Non2xxResponsesUseSharedErrorSchema(t *testing.T) {
	spec := loadServedSpec(t)

	if got := lookup(t, spec, "components", "schemas", "AnalyzeResponse", "properties", "error", "$ref"); got != "#/components/schemas/Error" {
		t.Fatalf("expected AnalyzeResponse.error to reference Error schema, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/v1/ready", "get", "responses", "503", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/ReadyErrorResponse" {
		t.Fatalf("expected /v1/ready 503 schema ReadyErrorResponse, got %#v", got)
	}
	if got := lookup(t, spec, "components", "schemas", "ReadyErrorResponse", "properties", "error", "$ref"); got != "#/components/schemas/Error" {
		t.Fatalf("expected ReadyErrorResponse.error to reference Error schema, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/metrics", "get", "responses", "501", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/AnalyzeResponse" {
		t.Fatalf("expected /v1/metrics 501 schema AnalyzeResponse, got %#v", got)
	}
}

func TestServeSpec_AnalyzeDescriptionDocumentsOperationalEnvVars(t *testing.T) {
	spec := loadServedSpec(t)

	desc := lookup(t, spec, "paths", "/v1/analyze", "post", "description").(string)
	for _, snippet := range []string{"PARSER_CONCURRENCY_LIMIT", "PARSER_MAX_OLD_SPACE_MB", "error.code=server_busy", "--max-old-space-size", "Operational environment variables"} {
		if !strings.Contains(desc, snippet) {
			t.Fatalf("expected /analyze description to contain %q, got %q", snippet, desc)
		}
	}
}

func TestServeSpec_AnalyzeResponseDescriptionsDocumentModeSemantics(t *testing.T) {
	spec := loadServedSpec(t)

	desc200 := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "200", "description").(string)
	for _, snippet := range []string{"error` is always null", "issues` is always present as an array", "syntax-error` is populated only for parser syntax failures"} {
		if !strings.Contains(desc200, snippet) {
			t.Fatalf("expected 200 description to contain %q, got %q", snippet, desc200)
		}
	}

	for _, status := range []string{"400", "413", "429", "500", "503", "504"} {
		desc := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", status, "description").(string)
		for _, snippet := range []string{"error", "syntax-error", "issues"} {
			if !strings.Contains(desc, snippet) {
				t.Fatalf("expected %s description to mention %q, got %q", status, snippet, desc)
			}
		}
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
		{"paths", "/v1/analyze", "post", "operationId"},
		{"paths", "/diagram-types", "get", "operationId"},
		{"paths", "/v1/internal/metrics", "get", "operationId"},
		{"paths", "/v1/internal/metrics", "get", "responses", "200", "content", "application/json", "schema", "$ref"},
		{"paths", "/v1/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"},
		{"components", "schemas", "Issue", "properties", "severity", "enum"},
		{"paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigVersioned", "value", "config", "rules", "max-fanout", "severity"},
		{"paths", "/v1/analyze/sarif", "post", "responses", "200", "content", "application/sarif+json", "schema", "$ref"},
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
		"Code generated by go run ./scripts/generate_openapi.go; DO NOT EDIT.",
		"\"openapi\": \"3.0.0\"",
		"\"/v1/analyze\"",
		"\"operationId\": \"analyzeCode\"",
		"\"$ref\": \"#/components/schemas/AnalyzeResponse\"",
		"- \"error\"",
		"- \"warning\"",
		"- \"info\"",
		"\"severity\": \"error\"",
		"\"/v1/analyze/sarif\"",
		"\"application/sarif+json\"",
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
	if !slices.Contains(required, "fingerprint") {
		t.Fatalf("expected %q to be required in Issue schema, got required=%#v", "fingerprint", required)
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

	if got := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigVersioned", "summary"); got != "Preferred versioned config payload (recommended)" {
		t.Fatalf("expected withConfigVersioned summary to describe preferred payload, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigNestedLegacy", "summary"); got != "Legacy nested rules payload (migration support)" {
		t.Fatalf("expected withConfigNestedLegacy summary to describe legacy migration support, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigFlatLegacy", "summary"); got != "Legacy flat rule payload (migration support)" {
		t.Fatalf("expected withConfigFlatLegacy summary to describe legacy migration support, got %#v", got)
	}

	withConfigVersioned := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigVersioned", "value").(map[string]interface{})
	if got := lookup(t, withConfigVersioned, "config", "schema-version"); got != "v1" {
		t.Fatalf("expected withConfigVersioned schema-version=v1, got %#v", got)
	}
	severity := lookup(t, withConfigVersioned, "config", "rules", "max-fanout", "severity")
	if severity != "error" {
		t.Fatalf("expected severity override example to be error, got %#v", severity)
	}

	selectors, ok := lookup(t, withConfigVersioned, "config", "rules", "max-fanout", "suppression-selectors").([]interface{})
	if !ok || len(selectors) == 0 {
		t.Fatalf("expected suppression-selectors example array, got %#v", lookup(t, withConfigVersioned, "config", "rules", "max-fanout", "suppression-selectors"))
	}
	if _, ok := selectors[0].(string); !ok {
		t.Fatalf("expected suppression-selectors entries to be strings, got %#v", selectors[0])
	}

	withConfigNestedLegacy := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigNestedLegacy", "value").(map[string]interface{})
	if _, hasSchemaVersion := lookup(t, withConfigNestedLegacy, "config").(map[string]interface{})["schema-version"]; hasSchemaVersion {
		t.Fatal("expected withConfigNestedLegacy to omit schema-version")
	}
	if got := lookup(t, withConfigNestedLegacy, "config", "rules", "max-fanout", "limit"); got != float64(2) {
		t.Fatalf("expected withConfigNestedLegacy max-fanout.limit=2, got %#v", got)
	}

	withConfigFlatLegacy := lookup(t, spec, "paths", "/v1/analyze", "post", "requestBody", "content", "application/json", "examples", "withConfigFlatLegacy", "value").(map[string]interface{})
	if _, hasRules := lookup(t, withConfigFlatLegacy, "config").(map[string]interface{})["rules"]; hasRules {
		t.Fatal("expected withConfigFlatLegacy to omit nested rules key")
	}
	if got := lookup(t, withConfigFlatLegacy, "config", "max-fanout", "limit"); got != float64(2) {
		t.Fatalf("expected withConfigFlatLegacy max-fanout.limit=2, got %#v", got)
	}

	unknownOption := lookup(t, spec, "paths", "/v1/analyze", "post", "responses", "400", "content", "application/json", "examples", "unknownOption", "value").(map[string]interface{})
	assertErrorShape(t, unknownOption, "unknown_option")

	severityEnum := lookup(t, spec, "components", "schemas", "Issue", "properties", "severity", "enum").([]interface{})
	got := make([]string, 0, len(severityEnum))
	for _, v := range severityEnum {
		got = append(got, v.(string))
	}
	for _, expected := range []string{"error", "warning", "info"} {
		if !slices.Contains(got, expected) {
			t.Fatalf("expected severity enum to include %q, got %#v", expected, got)
		}
	}
}

func TestServeSpec_ExposesDiagramTypesEndpointAndSchema(t *testing.T) {
	spec := loadServedSpec(t)
	if got := lookup(t, spec, "paths", "/diagram-types", "get", "operationId"); got != "listDiagramTypes" {
		t.Fatalf("expected /diagram-types operationId listDiagramTypes, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/diagram-types", "get", "responses", "200", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/DiagramTypesResponse" {
		t.Fatalf("expected /diagram-types response schema ref, got %#v", got)
	}
	_ = lookup(t, spec, "components", "schemas", "DiagramTypesResponse")
}

// compile-time guard to ensure local mock parser still satisfies parser contract.
var _ interface {
	Parse(string) (*model.Diagram, *parser.SyntaxError, error)
} = (*mockParser)(nil)

func TestServeSpec_ExposesRulesEndpointAndSchemas(t *testing.T) {
	spec := loadServedSpec(t)
	if got := lookup(t, spec, "paths", "/v1/rules", "get", "operationId"); got != "listRules" {
		t.Fatalf("expected /rules operationId listRules, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/v1/rules", "get", "responses", "200", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/RulesResponse" {
		t.Fatalf("expected /rules response schema ref, got %#v", got)
	}
	_ = lookup(t, spec, "components", "schemas", "RulesResponse")
	_ = lookup(t, spec, "components", "schemas", "RuleMetadata")
	_ = lookup(t, spec, "components", "schemas", "RuleOption")
}

func TestServeSpec_ExposesRuleConfigSchemaAndEndpoint(t *testing.T) {
	spec := loadServedSpec(t)

	if got := lookup(t, spec, "paths", "/v1/rules/schema", "get", "operationId"); got != "getRuleConfigSchema" {
		t.Fatalf("expected /rules/schema operationId getRuleConfigSchema, got %#v", got)
	}
	if got := lookup(t, spec, "components", "schemas", "AnalyzeRequest", "properties", "config", "$ref"); got != "#/components/schemas/RuleConfigSchema" {
		t.Fatalf("expected AnalyzeRequest.config to reference RuleConfigSchema, got %#v", got)
	}

	ruleSchema := lookup(t, spec, "components", "schemas", "RuleConfigSchema").(map[string]interface{})
	oneOf := lookup(t, ruleSchema, "oneOf").([]interface{})
	if len(oneOf) != 2 {
		t.Fatalf("expected RuleConfigSchema.oneOf to contain flat and versioned variants, got %#v", oneOf)
	}

	versioned := oneOf[1].(map[string]interface{})
	required := lookup(t, versioned, "required").([]interface{})
	if len(required) != 2 || required[0] != "schema-version" || required[1] != "rules" {
		t.Fatalf("expected versioned required to be [schema-version rules], got %#v", required)
	}

	rulesProps := lookup(t, versioned, "properties", "rules", "properties").(map[string]interface{})
	maxFanout := rulesProps["max-fanout"].(map[string]interface{})
	limit := lookup(t, maxFanout, "properties", "limit").(map[string]interface{})
	if limit["type"] != "integer" || limit["minimum"] != float64(1) {
		t.Fatalf("expected max-fanout.limit integer minimum=1, got %#v", limit)
	}
}

func TestServeSpec_LegacyAliasesAreDeprecated(t *testing.T) {
	spec := loadServedSpec(t)
	for _, path := range []string{"/analyze", "/rules", "/rules/schema", "/spec", "/docs", "/internal/metrics"} {
		methods := lookup(t, spec, "paths", path).(map[string]interface{})
		for _, methodDef := range methods {
			operation := methodDef.(map[string]interface{})
			if deprecated, ok := operation["deprecated"].(bool); !ok || !deprecated {
				t.Fatalf("expected %s to be marked deprecated", path)
			}
		}
	}
}
func TestServeSpec_InternalMetricsEndpointsDocumented(t *testing.T) {
	spec := loadServedSpec(t)

	if got := lookup(t, spec, "paths", "/v1/internal/metrics", "get", "operationId"); got != "getInternalMetrics" {
		t.Fatalf("expected /v1/internal/metrics operationId getInternalMetrics, got %#v", got)
	}
	if got := lookup(t, spec, "paths", "/v1/internal/metrics", "get", "responses", "200", "content", "application/json", "schema", "$ref"); got != "#/components/schemas/InternalMetricsResponse" {
		t.Fatalf("expected /v1/internal/metrics to use InternalMetricsResponse schema, got %#v", got)
	}
	if deprecated, ok := lookup(t, spec, "paths", "/internal/metrics", "get", "deprecated").(bool); !ok || !deprecated {
		t.Fatalf("expected /internal/metrics legacy alias to be deprecated")
	}
}
