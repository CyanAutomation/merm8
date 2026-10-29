package api

import (
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
	"github.com/CyanAutomation/merm8/internal/rules"
)

func TestAnalysisResultCacheExpiresEvictsAndCopiesValues(t *testing.T) {
	now := time.Unix(100, 0)
	cache := newAnalysisResultCache(1, time.Second, 4096, func() time.Time { return now })
	want := analysisCoreResult{Issues: []model.Issue{{RuleID: "no-cycles"}}}
	cache.Set("a", want)
	got, ok, _ := cache.Get("a")
	if !ok || len(got.Issues) != 1 || got.Issues[0].RuleID != "no-cycles" {
		t.Fatalf("cache Get = %#v, %v", got, ok)
	}
	got.Issues[0].RuleID = "changed"
	got, _, _ = cache.Get("a")
	if got.Issues[0].RuleID != "no-cycles" {
		t.Fatal("caller mutation changed cached result")
	}
	cache.Set("b", want)
	if _, ok, evicted := cache.Get("a"); ok || evicted != 0 {
		t.Fatalf("old LRU entry survived capacity eviction: ok=%v evicted=%d", ok, evicted)
	}
	now = now.Add(2 * time.Second)
	if _, ok, evicted := cache.Get("b"); ok || evicted != 1 {
		t.Fatalf("expired entry result: ok=%v evicted=%d", ok, evicted)
	}
}

func TestAnalysisResultCacheKeyIncludesDiagramAndRuleConfig(t *testing.T) {
	diagram := &model.Diagram{Type: model.DiagramTypeFlowchart, Nodes: []model.Node{{ID: "A"}}}
	base := rules.Config{"max-fanout": {"limit": 2}}
	if analysisResultCacheKey(diagram, base) == analysisResultCacheKey(diagram, rules.Config{"max-fanout": {"limit": 3}}) {
		t.Fatal("different rule configuration shared an analysis cache key")
	}
	changed := *diagram
	changed.Nodes = []model.Node{{ID: "B"}}
	if analysisResultCacheKey(diagram, base) == analysisResultCacheKey(&changed, base) {
		t.Fatal("different parsed diagrams shared an analysis cache key")
	}
}
