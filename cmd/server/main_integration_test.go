package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

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

func (p *blockingParser) Parse(string) (*model.Diagram, *parser.SyntaxError, error) {
	p.once.Do(func() {
		close(p.start)
		<-p.release
	})
	return &model.Diagram{Type: model.DiagramTypeFlowchart}, nil, nil
}

func TestServerStack_ParserConcurrencyBusyResponses_IncludeRetryAfter(t *testing.T) {
	t.Setenv("PARSER_CONCURRENCY_LIMIT", "1")

	start := make(chan struct{})
	release := make(chan struct{})
	mockP := &blockingParser{start: start, release: release}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->B"})

	firstReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(body))
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

	<-start

	secondReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(body))
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

	var apiResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(secondRes.Body).Decode(&apiResp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if apiResp.Error.Code != "server_busy" {
		t.Fatalf("expected error.code=server_busy, got %q", apiResp.Error.Code)
	}

	close(release)
	<-firstDone
}

func TestServerStack_ParserConcurrencyBusySARIF_IncludeRetryAfter(t *testing.T) {
	t.Setenv("PARSER_CONCURRENCY_LIMIT", "1")

	start := make(chan struct{})
	release := make(chan struct{})
	mockP := &blockingParser{start: start, release: release}

	h := api.NewHandler(mockP, engine.New())
	h.SetParserConcurrencyLimit(envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit))

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body, _ := json.Marshal(map[string]string{"code": "graph TD\nA-->B"})

	firstReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze/sarif", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create first SARIF request: %v", err)
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

	<-start

	secondReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze/sarif", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create second SARIF request: %v", err)
	}
	secondReq.Header.Set("Content-Type", "application/json")
	secondRes, err := server.Client().Do(secondReq)
	if err != nil {
		t.Fatalf("second SARIF request failed: %v", err)
	}
	defer secondRes.Body.Close()

	if secondRes.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on second SARIF request, got %d", secondRes.StatusCode)
	}
	retryAfter := secondRes.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatalf("expected Retry-After header to be set")
	}
	if seconds, err := strconv.Atoi(retryAfter); err != nil || seconds <= 0 {
		t.Fatalf("expected Retry-After to be a positive integer, got %q (err=%v)", retryAfter, err)
	}

	var report sarif.Report
	if err := json.NewDecoder(secondRes.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode SARIF response: %v", err)
	}
	if len(report.Runs) == 0 || len(report.Runs[0].Invocations) == 0 {
		t.Fatalf("expected SARIF invocation with error details, got %#v", report)
	}
	if got := report.Runs[0].Invocations[0].Properties["error-code"]; got != "server_busy" {
		t.Fatalf("expected SARIF error-code=server_busy, got %q", got)
	}

	close(release)
	<-firstDone
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
	rootHandler = api.CORSMiddleware(allowedOrigins)(rootHandler)

	server := httptest.NewServer(rootHandler)
	defer server.Close()

	// Test request from allowed origin
	req, err := http.NewRequest(http.MethodGet, server.URL+"/health", nil)
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
	rootHandler = api.CORSMiddleware(allowedOrigins)(rootHandler)

	server := httptest.NewServer(rootHandler)
	defer server.Close()

	// Test request from non-allowed origin
	req, err := http.NewRequest(http.MethodGet, server.URL+"/health", nil)
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
	rootHandler = api.CORSMiddleware(allowedOrigins)(rootHandler)

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
