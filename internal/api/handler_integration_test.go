//go:build integration

package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// newTestMuxWithRealParser creates a test mux that uses the real parser subprocess.
// Used for integration tests. Returns nil mux if parser script doesn't exist.
func newTestMuxWithRealParser(t *testing.T, scriptPath string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	p, err := parser.New(scriptPath)
	if err != nil {
		t.Skipf("skipping integration test; parser init failed: %v", err)
	}
	if err := p.Ready(); err != nil {
		t.Skipf("skipping integration test; parser not ready: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	h.RegisterRoutes(mux)
	return mux
}

func newTestMuxWithRealParserAndEngine(t *testing.T, scriptPath string, e *engine.Engine) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	p, err := parser.New(scriptPath)
	if err != nil {
		t.Skipf("skipping integration test; parser init failed: %v", err)
	}
	if err := p.Ready(); err != nil {
		t.Skipf("skipping integration test; parser not ready: %v", err)
	}
	h := api.NewHandler(p, e)
	h.RegisterRoutes(mux)
	return mux
}

type nextLineProbeRule struct{}

func (nextLineProbeRule) ID() string { return "next-line-probe" }

func (nextLineProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (nextLineProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	directiveLine := 2
	targetLine := 3
	return []model.Issue{
		{RuleID: "next-line-probe", Severity: "warning", Message: "directive-line issue", Line: &directiveLine},
		{RuleID: "next-line-probe", Severity: "warning", Message: "target-line issue", Line: &targetLine},
	}
}

type otherProbeRule struct{}

func (otherProbeRule) ID() string { return "other-probe" }

func (otherProbeRule) Families() []model.DiagramFamily {
	return []model.DiagramFamily{model.DiagramFamilyFlowchart}
}

func (otherProbeRule) Run(_ *model.Diagram, _ rules.Config) []model.Issue {
	line := 3
	return []model.Issue{{RuleID: "other-probe", Severity: "warning", Message: "other rule issue", Line: &line}}
}

func getParserScriptPath(t *testing.T) string {
	t.Helper()

	if script := os.Getenv("PARSER_SCRIPT"); script != "" {
		if _, err := os.Stat(script); err == nil {
			return script
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	for {
		candidate := filepath.Join(cwd, "parser-node", "parse.mjs")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	t.Skip("real parser script not found")
	return ""
}

func TestAnalyze_Integration_SingleRuleSuppression(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable max-fanout\nA-->B\nA-->C\nA-->D",
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Valid  bool          `json:"valid"`
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid=true response, got body=%s", w.Body.String())
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected max-fanout issue to be suppressed, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_GlobalSuppression(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable all\nA-->B\nA-->C\nA-->D\nE",
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) != 0 {
		t.Fatalf("expected all issues to be suppressed, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_SupportedDiagramFamilies(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	tests := []struct {
		name         string
		code         string
		expectedType string
	}{
		{
			name:         "class diagram",
			code:         "classDiagram\nClass01 <|-- AveryLongClass : Cool",
			expectedType: "class",
		},
		{
			name:         "sequence diagram",
			code:         "sequenceDiagram\nAlice->>Bob: Hi",
			expectedType: "sequence",
		},
		{
			name:         "ER diagram",
			code:         "erDiagram\nCUSTOMER ||--o{ ORDER : places",
			expectedType: "er",
		},
		{
			name:         "state diagram",
			code:         "stateDiagram-v2\n[*] --> Ready\nReady --> [*]",
			expectedType: "state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"code": tt.code})
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/v1/analyze", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if valid, ok := resp["valid"].(bool); !ok || !valid {
				t.Fatalf("expected valid=true for parsed diagrams, got %#v", resp["valid"])
			}
			if lintSupported, ok := resp["lint-supported"].(bool); !ok || !lintSupported {
				t.Fatalf("expected lint-supported=true, got %#v", resp["lint-supported"])
			}
			if diagramType, ok := resp["diagram-type"].(string); !ok || diagramType != tt.expectedType {
				t.Fatalf("expected diagram-type=%s, got %#v", tt.expectedType, resp["diagram-type"])
			}

			issues, ok := resp["issues"].([]interface{})
			if !ok {
				t.Fatalf("expected issues array, got %#v", resp["issues"])
			}

			for _, issue := range issues {
				issueMap, ok := issue.(map[string]interface{})
				if !ok {
					continue
				}
				if ruleID, ok := issueMap["rule-id"].(string); ok && ruleID == "unsupported-diagram-type" {
					t.Fatalf("did not expect unsupported-diagram-type for %s, got %#v", tt.expectedType, issues)
				}
			}

			metrics, ok := resp["metrics"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected metrics object, got %#v", resp["metrics"])
			}
			if diagramType, ok := metrics["diagram-type"].(string); !ok || diagramType != tt.expectedType {
				t.Fatalf("expected metrics.diagram-type=%s, got %#v", tt.expectedType, metrics["diagram-type"])
			}
		})
	}
}

func TestAnalyze_Integration_IgnoreNextLineSuppressesOnlyTargetLineForMatchingRule(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParserAndEngine(t, scriptPath, engine.NewWithRules(nextLineProbeRule{}, otherProbeRule{}))

	body := `{
		"code": "graph TD\n%% merm8-ignore-next-line next-line-probe\nA-->B"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Valid  bool          `json:"valid"`
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("expected valid=true response, got body=%s", w.Body.String())
	}
	if len(resp.Issues) != 2 {
		t.Fatalf("expected 2 remaining issues, got %#v", resp.Issues)
	}

	foundDirectiveLine := false
	foundOtherRule := false
	for _, issue := range resp.Issues {
		if issue.RuleID == "next-line-probe" && issue.Line != nil && *issue.Line == 2 {
			foundDirectiveLine = true
		}
		if issue.RuleID == "other-probe" {
			foundOtherRule = true
		}
		if issue.RuleID == "next-line-probe" && issue.Line != nil && *issue.Line == 3 {
			t.Fatalf("expected next-line suppression to hide matching target-line issue, got %#v", resp.Issues)
		}
	}
	if !foundDirectiveLine {
		t.Fatalf("expected directive-line issue to remain; next-line suppression must not apply to directive line")
	}
	if !foundOtherRule {
		t.Fatalf("expected non-matching rule issue to remain, got %#v", resp.Issues)
	}
}

func TestAnalyze_Integration_ParserTimeout_Returns504AndHandlerStaysResponsive(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "api-timeout-parser-")
	if err != nil {
		t.Fatalf("failed to create repo temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tempDir)
	})

	scriptPath := filepath.Join(tempDir, "parse.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {
  process.stdout.write(JSON.stringify({ valid: true, ast: { type: "flowchart", direction: "TD", nodes: [], edges: [], subgraphs: [], suppressions: [] } }) + "\n");
}, 8000);
`
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write timeout parser script: %v", err)
	}

	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{"code":"graph TD\nA-->B"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 when parser times out, got %d", w.Code)
	}
	assertExactErrorResponse(t, w.Body.Bytes(), "parser_timeout", "parser timed out while validating Mermaid code")

	healthReq := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	healthW := httptest.NewRecorder()
	mux.ServeHTTP(healthW, healthReq)

	if healthW.Code != http.StatusOK {
		t.Fatalf("expected handler to remain responsive after timeout; /healthz got %d", healthW.Code)
	}
}

func TestAnalyze_Integration_NonMatchingSuppressionDoesNotHideIssue(t *testing.T) {
	scriptPath := getParserScriptPath(t)
	mux := newTestMuxWithRealParser(t, scriptPath)

	body := `{
		"code": "graph TD\n%% merm8-disable no-duplicate-node-ids\nA-->B\nA-->C\nA-->D",
		"config": {"schema-version":"v1","rules": {"max-fanout": {"limit": 1}}}
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Issues []model.Issue `json:"issues"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Issues) == 0 {
		t.Fatalf("expected non-matching suppression to keep max-fanout issue")
	}
	if resp.Issues[0].RuleID != "max-fanout" {
		t.Fatalf("expected max-fanout issue, got %#v", resp.Issues)
	}
}

func TestV1AnalyseAliases_Integration_EmitDeprecationWarningAndMatchCanonicalResponse(t *testing.T) {
	mux := newTestMuxWithRealParser(t, getParserScriptPath(t))

	tests := []struct {
		name          string
		canonicalPath string
		aliasPath     string
		body          string
		contentType   string
	}{
		{name: "json analyze", canonicalPath: "/v1/analyze", aliasPath: "/v1/analyse", body: `{"code":"graph TD;A-->B"}`, contentType: "application/json"},
		{name: "raw analyze", canonicalPath: "/v1/analyze/raw", aliasPath: "/v1/analyse/raw", body: "graph TD\nA-->B", contentType: "text/plain"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canonicalReq := httptest.NewRequest(http.MethodPost, tc.canonicalPath, strings.NewReader(tc.body))
			canonicalReq.Header.Set("Content-Type", tc.contentType)
			canonicalW := httptest.NewRecorder()
			mux.ServeHTTP(canonicalW, canonicalReq)

			if canonicalW.Code != http.StatusOK {
				t.Fatalf("canonical route expected 200, got %d body=%s", canonicalW.Code, canonicalW.Body.String())
			}

			aliasReq := httptest.NewRequest(http.MethodPost, tc.aliasPath, strings.NewReader(tc.body))
			aliasReq.Header.Set("Content-Type", tc.contentType)
			aliasW := httptest.NewRecorder()
			mux.ServeHTTP(aliasW, aliasReq)

			if aliasW.Code != http.StatusOK {
				t.Fatalf("alias route expected 200, got %d body=%s", aliasW.Code, aliasW.Body.String())
			}
			var canonicalResp map[string]any
			if err := json.Unmarshal(canonicalW.Body.Bytes(), &canonicalResp); err != nil {
				t.Fatalf("failed to decode canonical response: %v", err)
			}
			var aliasResp map[string]any
			if err := json.Unmarshal(aliasW.Body.Bytes(), &aliasResp); err != nil {
				t.Fatalf("failed to decode alias response: %v", err)
			}
			delete(canonicalResp, "timestamp")
			delete(aliasResp, "timestamp")
			if !reflect.DeepEqual(canonicalResp, aliasResp) {
				t.Fatalf("expected alias response body to match canonical body (excluding timestamp)\ncanonical=%s\nalias=%s", canonicalW.Body.String(), aliasW.Body.String())
			}
			if got := canonicalW.Header().Get("Warning"); got != "" {
				t.Fatalf("expected no Warning header on canonical route, got %q", got)
			}
			if got := aliasW.Header().Get("Warning"); got == "" {
				t.Fatal("expected Warning header on /v1/analyse* alias route")
			}
			if got := aliasW.Header().Get("Deprecation"); got != "true" {
				t.Fatalf("expected Deprecation=true on alias route, got %q", got)
			}
		})
	}
}

func TestAnalyze_IntegrationParserTimeoutErrorDetails(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-timeout.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {}, 10000);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 1 * time.Second, NodeMaxOldSpaceMB: 512})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusGatewayTimeout)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "parser_timeout" {
		t.Fatalf("error.code=%v want parser_timeout", errObj["code"])
	}
	details := errObj["details"].(map[string]any)
	if _, ok := details["suggestion"]; !ok {
		t.Fatalf("expected suggestion in error.details")
	}
	if details["limit"] != "1s" {
		t.Fatalf("limit=%v want 1s", details["limit"])
	}
}

func TestAnalyze_IntegrationParserMemoryLimitErrorDetails(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-test-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-memory.mjs")
	scriptBody := `#!/usr/bin/env node
process.stderr.write("FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\\n");
process.stdout.write(JSON.stringify({valid:false,error:{message:"internal parser error: oom",line:0,column:0}}));
process.exit(1);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 5 * time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	code := "graph TD; A-->B"
	resp, err := http.Post(server.URL+"/v1/analyze", "application/json", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusInternalServerError)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	errObj := body["error"].(map[string]any)
	if errObj["code"] != "parser_memory_limit" {
		t.Fatalf("error.code=%v want parser_memory_limit", errObj["code"])
	}
	details := errObj["details"].(map[string]any)
	if details["limit"] != "256 MiB" {
		t.Fatalf("limit=%v want 256 MiB", details["limit"])
	}
	if _, ok := details["observed_size"]; !ok {
		t.Fatalf("expected observed_size in error.details")
	}
}

func TestAnalyze_ParserOverrideTimeoutBehavior(t *testing.T) {
	tempDir, err := os.MkdirTemp(".", "handler-parser-timeout-")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	script := filepath.Join(tempDir, "parse-timeout-override.mjs")
	scriptBody := `#!/usr/bin/env node
setTimeout(() => {
	process.stdout.write(JSON.stringify({valid:true,diagram_type:"flowchart",ast:{type:"flowchart",direction:"TD",nodes:[],edges:[],subgraphs:[],suppressions:[]}}));
}, 2000);
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o700); err != nil {
		t.Fatalf("failed to write parser script: %v", err)
	}

	p, err := parser.NewWithConfig(script, parser.Config{Timeout: 5 * time.Second, NodeMaxOldSpaceMB: 256})
	if err != nil {
		t.Fatalf("failed to initialize parser: %v", err)
	}
	h := api.NewHandler(p, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/analyze", "application/json", strings.NewReader(`{"code":"graph TD; A-->B","parser":{"timeout_seconds":1}}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusGatewayTimeout)
	}
}
