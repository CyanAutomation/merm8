package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
)

func TestServerContractIntegration_ConcurrencyBusyIncludesRetryAfter(t *testing.T) {
	t.Setenv("PARSER_CONCURRENCY_LIMIT", "1")

	marker := filepath.Join(t.TempDir(), "parse-started.log")
	blockLatch := filepath.Join(t.TempDir(), "parse-block.latch")
	releaseLatch := filepath.Join(t.TempDir(), "parse-release.latch")
	if err := os.WriteFile(blockLatch, []byte("block\n"), 0o600); err != nil {
		t.Fatalf("failed to create block latch: %v", err)
	}
	t.Setenv("MERM8_PARSE_MARKER", marker)
	t.Setenv("MERM8_PARSE_BLOCK_LATCH", blockLatch)
	t.Setenv("MERM8_PARSE_RELEASE_LATCH", releaseLatch)
	parserScript := filepath.Join("testdata", "fixtures", "parser", "controlled_parse.mjs")

	h, err := api.NewHandlerWithScript(parserScript)
	if err != nil {
		t.Fatalf("failed to build handler with fixture parser: %v", err)
	}
	h.SetParserConcurrencyLimit(1)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	slowPayload, err := os.ReadFile(filepath.Join("testdata", "fixtures", "diagrams", "slow-diagram.mmd"))
	if err != nil {
		t.Fatalf("failed to load slow diagram fixture: %v", err)
	}

	firstBody, err := json.Marshal(map[string]string{"code": string(slowPayload)})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	firstReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(firstBody))
	if err != nil {
		t.Fatalf("failed to create first request: %v", err)
	}
	firstReq.Header.Set("Content-Type", "application/json")

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		res, reqErr := server.Client().Do(firstReq)
		if reqErr == nil {
			_ = res.Body.Close()
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if b, readErr := os.ReadFile(marker); readErr == nil && strings.Contains(string(b), "blocked") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first parse request did not reach blocked state within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}

	secondReq, err := http.NewRequest(http.MethodPost, server.URL+"/v1/analyze", bytes.NewReader(firstBody))
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
		t.Fatalf("expected 503 when parser concurrency is saturated, got %d", secondRes.StatusCode)
	}
	if got := secondRes.Header.Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After=1 from API contract, got %q", got)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(secondRes.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode busy response: %v", err)
	}
	if body.Error.Code != "server_busy" {
		t.Fatalf("expected error.code=server_busy, got %q", body.Error.Code)
	}

	if err := os.WriteFile(releaseLatch, []byte("release\n"), 0o600); err != nil {
		t.Fatalf("failed to create release latch: %v", err)
	}

	<-firstDone
}

func TestServerContractIntegration_ParserTimeoutFromControlledSlowFixture(t *testing.T) {
	blockLatch := filepath.Join(t.TempDir(), "parse-block.latch")
	releaseLatch := filepath.Join(t.TempDir(), "parse-release.latch")
	if err := os.WriteFile(blockLatch, []byte("block\n"), 0o600); err != nil {
		t.Fatalf("failed to create block latch: %v", err)
	}
	t.Setenv("MERM8_PARSE_BLOCK_LATCH", blockLatch)
	t.Setenv("MERM8_PARSE_RELEASE_LATCH", releaseLatch)

	parserScript := filepath.Join("testdata", "fixtures", "parser", "controlled_parse.mjs")
	h, err := api.NewHandlerWithScript(parserScript)
	if err != nil {
		t.Fatalf("failed to build handler with fixture parser: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	slowPayload, err := os.ReadFile(filepath.Join("testdata", "fixtures", "diagrams", "large-slow-timeout.mmd"))
	if err != nil {
		t.Fatalf("failed to load large timeout fixture: %v", err)
	}

	requestBody, err := json.Marshal(map[string]any{
		"code": string(slowPayload),
		"parser": map[string]any{
			"timeout_seconds": 1,
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	res, err := http.Post(server.URL+"/v1/analyze", "application/json", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatalf("timeout request failed: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 parser timeout, got %d", res.StatusCode)
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode timeout response: %v", err)
	}
	if payload.Error.Code != "parser_timeout" {
		t.Fatalf("expected error.code=parser_timeout, got %q", payload.Error.Code)
	}
}

func TestServerContractIntegration_SuppressionSelectorNegationPrecedence(t *testing.T) {
	parserScript := filepath.Join("..", "..", "parser-node", "parse.mjs")
	h, err := api.NewHandlerWithScript(parserScript)
	if err != nil {
		t.Fatalf("failed to initialize real parser: %v", err)
	}

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	diagram := "graph TD\nA-->B\nA-->C\nA-->D"

	cases := []struct {
		name      string
		selectors []string
		wantCount int
	}{
		{name: "positive suppression hides issue", selectors: []string{"node:A"}, wantCount: 0},
		{name: "negation-only keeps issue", selectors: []string{"!node:A"}, wantCount: 1},
		{name: "negation takes precedence over positive selector", selectors: []string{"node:A", "!node:A"}, wantCount: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"code": diagram,
				"config": map[string]any{
					"schema-version": "v1",
					"rules": map[string]any{
						"max-fanout": map[string]any{
							"limit":                 1,
							"suppression-selectors": tc.selectors,
						},
					},
				},
			})
			if err != nil {
				t.Fatalf("failed to marshal request body: %v", err)
			}
			res, err := http.Post(server.URL+"/v1/analyze", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("status=%d want 200", res.StatusCode)
			}

			var resp struct {
				Issues []map[string]any `json:"issues"`
			}
			if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
				t.Fatalf("decode failed: %v", err)
			}
			if len(resp.Issues) != tc.wantCount {
				t.Fatalf("issues=%d want %d for selectors %v", len(resp.Issues), tc.wantCount, tc.selectors)
			}
		})
	}
}
