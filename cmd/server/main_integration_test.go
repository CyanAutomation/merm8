package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/output/sarif"
	"github.com/CyanAutomation/merm8/internal/parser"
)

type blockingParser struct {
	start   chan struct{}
	release chan struct{}
	once    sync.Once
}

const (
	busyResponseStartTimeout   = 2 * time.Second
	busyResponseRequestTimeout = 2 * time.Second
)

func (p *blockingParser) Parse(string) (*model.Diagram, *parser.SyntaxError, error) {
	p.once.Do(func() {
		close(p.start)
		<-p.release
	})
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func runBusyResponseScenario(t *testing.T, path string) []byte {
	t.Helper()

	t.Setenv("PARSER_CONCURRENCY_LIMIT", "1")

	start := make(chan struct{})
	releaseGate := make(chan struct{})
	mockP := &blockingParser{start: start, release: releaseGate}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->B"})

	firstReq, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create first request: %v", err)
	}
	firstReq.Header.Set("Content-Type", "application/json")

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		res, err := server.Client().Do(firstReq)
		if err == nil {
			_ = res.Body.Close()
		}
	}()

	select {
	case <-start:
	case <-time.After(busyResponseStartTimeout):
		t.Fatal("timed out waiting for saturation request to start")
	}

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			close(releaseGate)
		})
	}
	t.Cleanup(func() {
		release()
		select {
		case <-firstDone:
		case <-time.After(busyResponseRequestTimeout):
			t.Fatal("timed out waiting for saturation request cleanup")
		}
	})

	secondReq, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create second request: %v", err)
	}
	secondReq.Header.Set("Content-Type", "application/json")
	secondRes, err := server.Client().Do(secondReq)
	if err != nil {
		t.Fatalf("second request failed: %v", err)
	}
	defer secondRes.Body.Close()

	if secondRes.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on second request, got %d", secondRes.StatusCode)
	}
	retryAfter := secondRes.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatalf("expected Retry-After header to be set")
	}
	if seconds, err := strconv.Atoi(retryAfter); err != nil || seconds <= 0 {
		t.Fatalf("expected Retry-After to be a positive integer, got %q (err=%v)", retryAfter, err)
	}

	defer secondRes.Body.Close()

	responseBody, err := io.ReadAll(secondRes.Body)
	if err != nil {
		t.Fatalf("failed to read busy response body: %v", err)
	}

	release()
	select {
	case <-firstDone:
	case <-time.After(busyResponseRequestTimeout):
		t.Fatal("timed out waiting for saturation request to complete")
	}

	return responseBody
}

func TestServerStack_ParserConcurrencyBusyResponses_IncludeRetryAfter(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		body := runBusyResponseScenario(t, "/v1/analyze")

		var apiResp struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &apiResp); err != nil {
			t.Fatalf("failed to decode JSON response: %v", err)
		}
		if apiResp.Error.Code != "server_busy" {
			t.Fatalf("expected error.code=server_busy, got %q", apiResp.Error.Code)
		}
	})

	t.Run("sarif", func(t *testing.T) {
		body := runBusyResponseScenario(t, "/v1/analyze/sarif")

		var report sarif.Report
		if err := json.Unmarshal(body, &report); err != nil {
			t.Fatalf("failed to decode SARIF response: %v", err)
		}
		if len(report.Runs) == 0 || len(report.Runs[0].Invocations) == 0 {
			t.Fatalf("expected SARIF invocation with error details, got %#v", report)
		}
		if got := report.Runs[0].Invocations[0].Properties["error-code"]; got != "server_busy" {
			t.Fatalf("expected SARIF error-code=server_busy, got %q", got)
		}
	})
}

// TestServerStack_CORSHeaders_AllowedOrigin verifies CORS headers are set for allowed origins.
func TestServerStack_CORSHeaders_AllowedOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com,https://app.example.com")

	h := api.NewHandler(&mockParser{}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Simulate the middleware chain from main.go
	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)
	rootHandler = api.VersionNegotiationMiddleware(rootHandler)
	allowedOrigins := "https://example.com,https://app.example.com"
	rootHandler = api.MetricsMiddleware(rootHandler, map[string]string{"GET /v1/health": "/v1/health"}, nil)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, api.NewLogger("test"))
	rootHandler = api.CORSMiddleware(allowedOrigins, nil, nil)(rootHandler)

	server := httptest.NewServer(rootHandler)
	defer server.Close()

	// Test request from allowed origin
	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "https://example.com")

	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header, got %q", got)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
}

// TestServerStack_CORSHeaders_DisallowedOrigin verifies CORS headers are NOT set for disallowed origins.
func TestServerStack_CORSHeaders_DisallowedOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com")

	h := api.NewHandler(&mockParser{}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Simulate the middleware chain from main.go
	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)
	rootHandler = api.VersionNegotiationMiddleware(rootHandler)
	allowedOrigins := "https://example.com"
	rootHandler = api.MetricsMiddleware(rootHandler, map[string]string{"GET /v1/health": "/v1/health"}, nil)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, api.NewLogger("test"))
	rootHandler = api.CORSMiddleware(allowedOrigins, nil, nil)(rootHandler)

	server := httptest.NewServer(rootHandler)
	defer server.Close()

	// Test request from non-allowed origin
	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/health", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "https://attacker.com")

	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header for disallowed origin, got %q", got)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.StatusCode)
	}
}

// TestServerStack_CORSPreflight_AllowedOrigin verifies preflight OPTIONS requests are handled correctly.
func TestServerStack_CORSPreflight_AllowedOrigin(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "https://example.com")

	h := api.NewHandler(&mockParser{}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Simulate the middleware chain from main.go
	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)
	rootHandler = api.VersionNegotiationMiddleware(rootHandler)
	allowedOrigins := "https://example.com"
	rootHandler = api.MetricsMiddleware(rootHandler, map[string]string{"POST /v1/analyze": "/v1/analyze"}, nil)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, api.NewLogger("test"))
	rootHandler = api.CORSMiddleware(allowedOrigins, nil, nil)(rootHandler)

	server := httptest.NewServer(rootHandler)
	defer server.Close()

	// Test preflight OPTIONS request
	req, err := http.NewRequest(http.MethodOptions, server.URL+"/v1/analyze", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	defer res.Body.Close()

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header in preflight, got %q", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods header in preflight")
	}
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204 for preflight, got %d", res.StatusCode)
	}
}

type mockParser struct{}

func (p *mockParser) Parse(string) (*model.Diagram, *parser.SyntaxError, error) {
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func newMainLikeTestServer(t *testing.T, allowedOrigins string, rateLimitPerMinute int, authToken string) *httptest.Server {
	t.Helper()

	h := api.NewHandler(&mockParser{}, engine.New())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)
	rootHandler = api.VersionNegotiationMiddleware(rootHandler)
	if rateLimitPerMinute > 0 {
		limiter := api.NewRateLimiter(rateLimitPerMinute, time.Minute)
		rootHandler = api.AnalyzeRateLimitMiddleware(limiter, rootHandler)
	}
	rootHandler = api.AnalyzeBearerAuthMiddleware(authToken, rootHandler)
	routePatterns := map[string]string{
		"GET /":             "/",
		"GET /health":       "/health",
		"GET /healthz":      "/healthz",
		"GET /ready":        "/ready",
		"GET /version":      "/version",
		"GET /info":         "/info",
		"GET /metrics":      "/metrics",
		"POST /analyze":     "/analyze",
		"POST /analyze/raw": "/analyze/raw",
	}
	rootHandler = api.MetricsMiddleware(rootHandler, routePatterns, nil)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, api.NewLogger("test"))
	rootHandler = api.CORSMiddleware(allowedOrigins, nil, nil)(rootHandler)

	return httptest.NewServer(rootHandler)
}

func TestServerStack_AnalyzeAuthFailure_IncludesCORSHeader(t *testing.T) {
	allowedOrigins := "https://example.com"
	server := newMainLikeTestServer(t, allowedOrigins, 0, "s3cr3t")

	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->B"})

	unauthorizedReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create unauthorized request: %v", err)
	}
	unauthorizedReq.Header.Set("Content-Type", "application/json")
	unauthorizedReq.Header.Set("Origin", "https://example.com")

	unauthorizedRes, err := server.Client().Do(unauthorizedReq)
	if err != nil {
		t.Fatalf("unauthorized request failed: %v", err)
	}
	defer unauthorizedRes.Body.Close()

	if unauthorizedRes.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing bearer token, got %d", unauthorizedRes.StatusCode)
	}
	if got := unauthorizedRes.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header on 401 response, got %q", got)
	}
	var unauthorizedPayload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(unauthorizedRes.Body).Decode(&unauthorizedPayload); err != nil {
		t.Fatalf("failed to decode unauthorized response JSON: %v", err)
	}
	if unauthorizedPayload.Error.Code != "unauthorized" || unauthorizedPayload.Error.Message != "missing or invalid bearer token" {
		t.Fatalf("unexpected unauthorized payload: %#v", unauthorizedPayload)
	}
}

func TestServerStack_AnalyzeRateLimited_IncludesCORSAndRateLimitHeaders(t *testing.T) {
	allowedOrigins := "https://example.com"
	server := newMainLikeTestServer(t, allowedOrigins, 1, "s3cr3t")
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->B"})

	firstReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create first request: %v", err)
	}
	firstReq.Header.Set("Content-Type", "application/json")
	firstReq.Header.Set("Origin", "https://example.com")
	firstReq.Header.Set("Authorization", "Bearer s3cr3t")
	firstAuthorizedRes, err := server.Client().Do(firstReq)
	if err != nil {
		t.Fatalf("first authorized request failed: %v", err)
	}
	defer firstAuthorizedRes.Body.Close()
	if firstAuthorizedRes.StatusCode != http.StatusOK {
		t.Fatalf("expected first authorized request to pass, got %d", firstAuthorizedRes.StatusCode)
	}

	secondAuthorizedReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create second authorized request: %v", err)
	}
	secondAuthorizedReq.Header.Set("Content-Type", "application/json")
	secondAuthorizedReq.Header.Set("Origin", "https://example.com")
	secondAuthorizedReq.Header.Set("Authorization", "Bearer s3cr3t")
	rateLimitedRes, err := server.Client().Do(secondAuthorizedReq)
	if err != nil {
		t.Fatalf("rate-limited request failed: %v", err)
	}
	defer rateLimitedRes.Body.Close()

	if rateLimitedRes.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for second authorized request, got %d", rateLimitedRes.StatusCode)
	}
	if got := rateLimitedRes.Header.Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header on 429 response, got %q", got)
	}
	if got := rateLimitedRes.Header.Get("X-RateLimit-Limit"); got == "" {
		t.Fatal("expected X-RateLimit-Limit header on 429 response")
	}
	if got := rateLimitedRes.Header.Get("X-RateLimit-Remaining"); got == "" {
		t.Fatal("expected X-RateLimit-Remaining header on 429 response")
	}
	if got := rateLimitedRes.Header.Get("X-RateLimit-Reset"); got == "" {
		t.Fatal("expected X-RateLimit-Reset header on 429 response")
	}

	var rateLimitedPayload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rateLimitedRes.Body).Decode(&rateLimitedPayload); err != nil {
		t.Fatalf("failed to decode rate-limited response JSON: %v", err)
	}
	if rateLimitedPayload.Error.Code != "rate_limited" || rateLimitedPayload.Error.Message != "rate limit exceeded" {
		t.Fatalf("unexpected rate-limited payload: %#v", rateLimitedPayload)
	}
}

func TestValidateStartupAuthToken_ProductionRequiresAuthToken(t *testing.T) {
	err := validateStartupAuthToken("production", "")
	if err == nil {
		t.Fatal("expected error when production mode has empty ANALYZE_AUTH_TOKEN")
	}
	if got, want := err.Error(), "ANALYZE_AUTH_TOKEN is required when DEPLOYMENT_MODE=production"; got != want {
		t.Fatalf("error=%q want %q", got, want)
	}
}

func TestValidateStartupAuthToken_NonProductionAllowsEmptyAuthToken(t *testing.T) {
	if err := validateStartupAuthToken("development", ""); err != nil {
		t.Fatalf("expected no error for non-production mode, got %v", err)
	}
}

func TestResolveAllowedOrigins_ProductionWarnsWhenEmpty(t *testing.T) {
	resolved, shouldWarn := resolveAllowedOrigins("production", "")

	if resolved != defaultAllowedOrigins {
		t.Fatalf("expected default allowed origins %q, got %q", defaultAllowedOrigins, resolved)
	}
	if !shouldWarn {
		t.Fatal("expected warning for empty ALLOWED_ORIGINS in production")
	}
}

func TestResolveAllowedOrigins_ProductionWarnsOnDockerPlaceholder(t *testing.T) {
	resolved, shouldWarn := resolveAllowedOrigins("production", dockerAllowedOriginsPlaceholder)

	if resolved != dockerAllowedOriginsPlaceholder {
		t.Fatalf("expected configured allowed origins %q, got %q", dockerAllowedOriginsPlaceholder, resolved)
	}
	if !shouldWarn {
		t.Fatal("expected warning for Docker placeholder ALLOWED_ORIGINS in production")
	}
}

func TestResolveAllowedOrigins_DevelopmentNoWarningWhenEmpty(t *testing.T) {
	resolved, shouldWarn := resolveAllowedOrigins("development", "")

	if resolved != defaultAllowedOrigins {
		t.Fatalf("expected default allowed origins %q, got %q", defaultAllowedOrigins, resolved)
	}
	if shouldWarn {
		t.Fatal("did not expect warning for empty ALLOWED_ORIGINS in development")
	}
}
