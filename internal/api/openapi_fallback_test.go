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

func TestServeSpec_Returns200WithValidJSONWhenFallbackIsUsed(t *testing.T) {
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
		Paths   map[string]json.RawMessage `json:"paths"`
		Servers []json.RawMessage          `json:"servers"`
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

	if len(payload.Paths) == 0 {
		t.Fatalf("expected fallback payload paths to be present")
	}

	if len(payload.Servers) == 0 {
		t.Fatalf("expected fallback payload servers to be present")
	}
}
