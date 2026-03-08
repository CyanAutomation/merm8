package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
)

func TestCloneOpenAPISpec_ReturnsErrorForUnmarshalableValue(t *testing.T) {
	_, err := cloneOpenAPISpec(map[string]interface{}{
		"bad": make(chan int),
	})
	if err == nil {
		t.Fatal("expected cloneOpenAPISpec to fail for non-JSON-serializable values")
	}
}

func TestOpenAPISpec_FallsBackWhenCloneFails(t *testing.T) {
	orig := openapi
	t.Cleanup(func() {
		openapi = orig
	})

	openapi = map[string]interface{}{
		"bad": make(chan int),
	}

	spec := OpenAPISpec()

	if spec["openapi"] != "3.0.0" {
		t.Fatalf("expected fallback openapi version, got %#v", spec["openapi"])
	}
	info, ok := spec["info"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback info object, got %T", spec["info"])
	}
	if info["title"] == "" || info["version"] == "" {
		t.Fatalf("expected fallback info title/version, got %#v", info)
	}
	if _, ok := spec["servers"].([]map[string]interface{}); !ok {
		t.Fatalf("expected fallback servers list, got %T", spec["servers"])
	}
}

func TestServeSpec_Returns200WithValidJSONWhenFallbackIsUsed(t *testing.T) {
	orig := openapi
	t.Cleanup(func() {
		openapi = orig
	})

	openapi = map[string]interface{}{
		"bad": make(chan int),
	}

	h := NewHandler(nil, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/spec", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected /v1/spec 200, got %d", w.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("expected valid JSON payload, got error: %v", err)
	}
	if payload["openapi"] != "3.0.0" {
		t.Fatalf("expected fallback payload openapi version, got %#v", payload["openapi"])
	}
}
