package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
)

func withOpenAPIMutation(t *testing.T, replacement map[string]interface{}) {
	t.Helper()

	orig := openapi
	openapi = replacement
	t.Cleanup(func() {
		openapi = orig
	})
}

// @spec: DOCS-003: OpenAPI serialization fallback remains a usable specification endpoint.
func TestServeSpec_Returns200WithValidJSONWhenFallbackIsUsed(t *testing.T) {
	t.Setenv("MERM8_API_URL", "")
	withOpenAPIMutation(t, map[string]interface{}{
		"bad": make(chan int),
	})

	h := NewHandler(nil, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/spec", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected /v1/spec 200, got %d", w.Code)
	}

	var payload struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
		Servers []struct {
			URL string `json:"url"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid JSON payload, got error: %v", err)
	}
	if payload.OpenAPI != "3.0.0" {
		t.Fatalf("expected fallback payload openapi version, got %#v", payload.OpenAPI)
	}

	if payload.Info.Title == "" || payload.Info.Version == "" {
		t.Fatalf("expected fallback payload info title/version, got %#v", payload.Info)
	}

	path, ok := payload.Paths["/v1/spec"]
	if !ok {
		t.Fatalf("expected fallback to document /v1/spec, got paths %#v", payload.Paths)
	}
	var getOperation struct {
		Summary   string                     `json:"summary"`
		Responses map[string]json.RawMessage `json:"responses"`
	}
	if err := json.Unmarshal(path["get"], &getOperation); err != nil {
		t.Fatalf("decode fallback /v1/spec GET operation: %v", err)
	}
	if getOperation.Summary != "Get OpenAPI specification" {
		t.Fatalf("fallback GET summary = %q, want %q", getOperation.Summary, "Get OpenAPI specification")
	}
	if _, ok := getOperation.Responses["200"]; !ok {
		t.Fatalf("fallback GET responses = %#v, want 200 response", getOperation.Responses)
	}

	if len(payload.Servers) == 0 {
		t.Fatalf("expected fallback payload servers to be present")
	}
	if payload.Servers[0].URL != "http://localhost:8080" {
		t.Fatalf("fallback server URL = %q, want %q", payload.Servers[0].URL, "http://localhost:8080")
	}
}
