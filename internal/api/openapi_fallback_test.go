package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CyanAutomation/merm8/internal/engine"
)

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

	info, ok := payload["info"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback payload info object, got %T", payload["info"])
	}
	if info["title"] == "" || info["version"] == "" {
		t.Fatalf("expected fallback payload info title/version, got %#v", info)
	}

	servers, ok := payload["servers"].([]interface{})
	if !ok || len(servers) == 0 {
		t.Fatalf("expected fallback payload servers list, got %T (%#v)", payload["servers"], payload["servers"])
	}
	firstServer, ok := servers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected fallback payload first server object, got %T", servers[0])
	}
	if firstServer["url"] == "" {
		t.Fatalf("expected fallback payload first server url, got %#v", firstServer)
	}
}
