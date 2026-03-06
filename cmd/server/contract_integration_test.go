package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
)

func TestServerContractIntegration_ConcurrencyBusyIncludesRetryAfter(t *testing.T) {
	t.Setenv("PARSER_CONCURRENCY_LIMIT", "1")

	tmpDir := t.TempDir()
	marker := filepath.Join(tmpDir, "parse-started.log")
	blockReleaseFile := filepath.Join(tmpDir, "release-parse-block")
	t.Setenv("MERM8_PARSE_MARKER", marker)
	t.Setenv("MERM8_PARSE_BLOCK_RELEASE_FILE", blockReleaseFile)
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

	type latch struct {
		once sync.Once
		ch   chan struct{}
	}

	started := &latch{ch: make(chan struct{})}
	notifyMux := http.NewServeMux()
	notifyMux.HandleFunc("/started", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		started.once.Do(func() { close(started.ch) })
		w.WriteHeader(http.StatusNoContent)
	})
	notifyServer := httptest.NewServer(notifyMux)
	defer notifyServer.Close()
	t.Setenv("MERM8_PARSE_SIGNAL_URL", notifyServer.URL+"/started")

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		res, reqErr := server.Client().Do(firstReq)
		if reqErr == nil {
			_ = res.Body.Close()
		}
	}()

	const maxWait = 10 * time.Second
	waitStart := time.Now()
	select {
	case <-started.ch:
	case <-time.After(maxWait):
		markerState := "unavailable"
		if b, readErr := os.ReadFile(marker); readErr == nil {
			markerState = string(b)
		} else {
			markerState = "read error: " + readErr.Error()
		}
		t.Fatalf("first parse request did not signal parser start within %s (elapsed=%s, marker_state=%q)", maxWait, time.Since(waitStart), markerState)
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
	retryAfter := secondRes.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatalf("expected Retry-After header to be set when parser concurrency is saturated")
	}
	if retryAfterSeconds, convErr := strconv.Atoi(retryAfter); convErr != nil || retryAfterSeconds <= 0 {
		t.Fatalf("expected Retry-After to be a positive integer, got %q (conv_err=%v)", retryAfter, convErr)
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

	if err := os.WriteFile(blockReleaseFile, []byte("release\n"), 0o644); err != nil {
		t.Fatalf("failed to release parser block: %v", err)
	}

	select {
	case <-firstDone:
	case <-time.After(maxWait):
		t.Fatalf("first parse request did not complete within %s after release", maxWait)
	}
}

func TestServerContractIntegration_ParserTimeoutFromControlledSlowFixture(t *testing.T) {
	tmpDir := t.TempDir()
	blockReleaseFile := filepath.Join(tmpDir, "never-release-parse-block")
	t.Setenv("MERM8_PARSE_BLOCK_RELEASE_FILE", blockReleaseFile)

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
