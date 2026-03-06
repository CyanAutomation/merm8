package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/model"
	parserpkg "github.com/CyanAutomation/merm8/internal/parser"
)

// suppressionFixtureParser is a controlled parser fixture used to validate
// suppression selector precedence without spawning the real parser runtime.
// Keeping this test fixture-based ensures default `go test ./cmd/...` remains fast.
type suppressionFixtureParser struct{}

func (suppressionFixtureParser) Parse(code string) (*model.Diagram, *parserpkg.SyntaxError, error) {
	return &model.Diagram{
		Type:  model.DiagramTypeFlowchart,
		Nodes: []model.Node{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}},
		Edges: []model.Edge{{From: "A", To: "B"}, {From: "A", To: "C"}, {From: "A", To: "D"}},
	}, nil, nil
}

func TestServerContractIntegration_SuppressionSelectorNegationPrecedence(t *testing.T) {
	h := api.NewHandler(suppressionFixtureParser{}, engine.New())

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
