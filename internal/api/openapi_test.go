package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
)

func loadServedSpec(t *testing.T) map[string]interface{} {
	t.Helper()
	mux := newTestMux(func(code string) (*model.Diagram, *parser.SyntaxError, error) {
		return nil, nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/spec", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected /spec status 200, got %d", w.Code)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to decode served /spec JSON: %v", err)
	}
	return spec
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func loadFileText(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("failed reading %s: %v", rel, err)
	}
	return string(data)
}

func mustMap(t *testing.T, v interface{}, path string) map[string]interface{} {
	t.Helper()
	m, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("%s should be object, got %T", path, v)
	}
	return m
}

func mustSlice(t *testing.T, v interface{}, path string) []interface{} {
	t.Helper()
	s, ok := v.([]interface{})
	if !ok {
		t.Fatalf("%s should be array, got %T", path, v)
	}
	return s
}

func mustString(t *testing.T, v interface{}, path string) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%s should be string, got %T", path, v)
	}
	return s
}

func atPath(t *testing.T, doc map[string]interface{}, path ...string) interface{} {
	t.Helper()
	var cur interface{} = doc
	for _, p := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("path %v: segment %q parent is %T", path, p, cur)
		}
		next, ok := m[p]
		if !ok {
			t.Fatalf("path %v: missing segment %q", path, p)
		}
		cur = next
	}
	return cur
}

func TestOpenAPISpec_ServedSpecRequiredFieldsAndComponentRefs(t *testing.T) {
	spec := loadServedSpec(t)
	_ = mustString(t, atPath(t, spec, "openapi"), "openapi")
	_ = mustString(t, atPath(t, spec, "info", "title"), "info.title")
	_ = mustMap(t, atPath(t, spec, "paths", "/analyze"), "paths./analyze")
	_ = mustMap(t, atPath(t, spec, "components", "schemas"), "components.schemas")

	if got := mustString(t, atPath(t, spec, "paths", "/analyze", "post", "requestBody", "content", "application/json", "schema", "$ref"), "request ref"); got != "#/components/schemas/AnalyzeRequest" {
		t.Fatalf("unexpected request schema ref: %s", got)
	}
	if got := mustString(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "200", "content", "application/json", "schema", "$ref"), "200 ref"); got != "#/components/schemas/AnalyzeResponse" {
		t.Fatalf("unexpected 200 schema ref: %s", got)
	}
	if got := mustString(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"), "400 ref"); got != "#/components/schemas/ErrorResponse" {
		t.Fatalf("unexpected 400 schema ref: %s", got)
	}
	if got := mustString(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "500", "content", "application/json", "schema", "$ref"), "500 ref"); got != "#/components/schemas/ErrorResponse" {
		t.Fatalf("unexpected 500 schema ref: %s", got)
	}
}

func TestOpenAPISpec_AnalyzeExamplesMatchExpectedShape(t *testing.T) {
	spec := loadServedSpec(t)

	examples200 := mustMap(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "200", "content", "application/json", "examples"), "200 examples")
	for name, raw := range examples200 {
		value := mustMap(t, mustMap(t, raw, name)["value"], name+".value")
		if _, ok := value["valid"]; !ok {
			t.Fatalf("200 example %q missing valid", name)
		}
		if _, ok := value["issues"]; !ok {
			t.Fatalf("200 example %q missing issues", name)
		}
	}
	validValue := mustMap(t, mustMap(t, examples200["validDiagram"], "validDiagram")["value"], "validDiagram.value")
	metrics := mustMap(t, validValue["metrics"], "validDiagram.value.metrics")
	_ = metrics["node_count"]
	_ = metrics["edge_count"]
	_ = metrics["max_fanout"]

	examples400 := mustMap(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "examples"), "400 examples")
	for name, raw := range examples400 {
		value := mustMap(t, mustMap(t, raw, name)["value"], name+".value")
		if valid, ok := value["valid"].(bool); !ok || valid {
			t.Fatalf("400 example %q should have valid=false", name)
		}
		_ = mustSlice(t, value["issues"], name+".issues")
		errObj := mustMap(t, value["error"], name+".error")
		_ = mustString(t, errObj["code"], name+".error.code")
		_ = mustString(t, errObj["message"], name+".error.message")
	}

	examples500 := mustMap(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "500", "content", "application/json", "examples"), "500 examples")
	for name, raw := range examples500 {
		value := mustMap(t, mustMap(t, raw, name)["value"], name+".value")
		if valid, ok := value["valid"].(bool); !ok || valid {
			t.Fatalf("500 example %q should have valid=false", name)
		}
		errObj := mustMap(t, value["error"], name+".error")
		_ = mustString(t, errObj["code"], name+".error.code")
		_ = mustString(t, errObj["message"], name+".error.message")
	}
}

func TestOpenAPISpec_SelectedDriftChecks(t *testing.T) {
	served := loadServedSpec(t)
	jsonText := loadFileText(t, "openapi.json")
	yamlText := loadFileText(t, "openapi.yaml")

	if got := mustString(t, atPath(t, served, "paths", "/analyze", "post", "summary"), "served summary"); got == "" {
		t.Fatal("served /spec missing analyze summary")
	}
	if got := mustString(t, atPath(t, served, "paths", "/analyze", "post", "responses", "200", "content", "application/json", "schema", "$ref"), "served 200 ref"); got != "#/components/schemas/AnalyzeResponse" {
		t.Fatalf("unexpected served 200 schema ref: %s", got)
	}
	if got := mustString(t, atPath(t, served, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "schema", "$ref"), "served 400 ref"); got != "#/components/schemas/ErrorResponse" {
		t.Fatalf("unexpected served 400 schema ref: %s", got)
	}

	for _, snippet := range []string{
		"\"summary\": \"Analyze and lint a Mermaid diagram\"",
		"\"$ref\": \"#/components/schemas/AnalyzeResponse\"",
		"\"enum\": [",
		"\"warn\"",
	} {
		if !strings.Contains(jsonText, snippet) {
			t.Fatalf("openapi.json missing expected snippet %q", snippet)
		}
	}

	for _, snippet := range []string{
		"openapi: 3.0.0",
		"summary: Analyze and lint a Mermaid diagram",
		"$ref: '#/components/schemas/AnalyzeResponse'",
		"$ref: '#/components/schemas/ErrorResponse'",
		"severity: warn",
		"- warn",
	} {
		if !strings.Contains(yamlText, snippet) {
			t.Fatalf("openapi.yaml missing expected snippet %q", snippet)
		}
	}
}

func TestOpenAPISpec_Regression_ConfigValidationAndSeverityOverrideExamples(t *testing.T) {
	spec := loadServedSpec(t)

	t.Run("config validation error examples", func(t *testing.T) {
		examples400 := mustMap(t, atPath(t, spec, "paths", "/analyze", "post", "responses", "400", "content", "application/json", "examples"), "400 examples")
		raw, ok := examples400["invalidConfig"]
		if !ok {
			t.Skip("invalidConfig example not present yet")
		}
		value := mustMap(t, mustMap(t, raw, "invalidConfig")["value"], "invalidConfig.value")
		errObj := mustMap(t, value["error"], "invalidConfig.value.error")
		_ = mustString(t, errObj["code"], "invalidConfig.code")
	})

	t.Run("severity override request examples", func(t *testing.T) {
		reqExamples := mustMap(t, atPath(t, spec, "paths", "/analyze", "post", "requestBody", "content", "application/json", "examples"), "request examples")
		raw, ok := reqExamples["severityOverride"]
		if !ok {
			t.Skip("severityOverride example not present yet")
		}
		value := mustMap(t, mustMap(t, raw, "severityOverride")["value"], "severityOverride.value")
		if len(mustMap(t, value["config"], "severityOverride.config")) == 0 {
			t.Fatal("severityOverride config must not be empty")
		}
	})
}
