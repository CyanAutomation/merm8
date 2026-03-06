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
