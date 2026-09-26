package parser

import (
	"fmt"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

type recordingCacheMetrics struct {
	events map[string]int
}

func (m *recordingCacheMetrics) ObserveParserCacheEvent(result, entryType string) {
	m.events[fmt.Sprintf("%s:%s", result, entryType)]++
}

func newTestParseCache(now *time.Time, successSize, syntaxSize int, ttl time.Duration) (*parseCache, *recordingCacheMetrics) {
	clock := func() time.Time { return *now }
	metrics := &recordingCacheMetrics{events: make(map[string]int)}
	cache := &parseCache{
		success: newLRUTTLCache[*model.Diagram](successSize, ttl),
		syntax:  newLRUTTLCache[*SyntaxError](syntaxSize, ttl),
	}
	cache.success.now = clock
	cache.syntax.now = clock
	cache.setMetrics(metrics)
	return cache, metrics
}

func assertCacheEvents(t *testing.T, metrics *recordingCacheMetrics, expected map[string]int) {
	t.Helper()
	if len(metrics.events) != len(expected) {
		t.Fatalf("unexpected cache events: got %v, want %v", metrics.events, expected)
	}
	for event, count := range expected {
		if got := metrics.events[event]; got != count {
			t.Errorf("unexpected count for %s: got %d, want %d (all events: %v)", event, got, count, metrics.events)
		}
	}
}

func TestParseCache_GetObservesExpirationsDuringMissByEntryType(t *testing.T) {
	now := time.Unix(1_000, 0)
	cache, metrics := newTestParseCache(&now, 4, 4, time.Second)
	cache.putSuccess("expired-success", &model.Diagram{})
	cache.putSyntax("expired-syntax", &SyntaxError{Message: "expired"})

	now = now.Add(2 * time.Second)
	if diagram, syntaxErr, ok := cache.get("missing"); ok || diagram != nil || syntaxErr != nil {
		t.Fatalf("expected miss, got diagram=%v syntaxErr=%v ok=%v", diagram, syntaxErr, ok)
	}

	assertCacheEvents(t, metrics, map[string]int{
		"eviction:success": 1,
		"eviction:syntax":  1,
		"miss:any":         1,
	})
}

func TestParseCache_GetSuccessObservesExpirationDuringMiss(t *testing.T) {
	now := time.Unix(2_000, 0)
	cache, metrics := newTestParseCache(&now, 4, 4, time.Second)
	cache.putSuccess("expired", &model.Diagram{})

	now = now.Add(2 * time.Second)
	if diagram, ok := cache.getSuccess("missing"); ok || diagram != nil {
		t.Fatalf("expected miss, got diagram=%v ok=%v", diagram, ok)
	}

	assertCacheEvents(t, metrics, map[string]int{
		"eviction:success": 1,
		"miss:success":     1,
	})
}

func TestParseCache_PutObservesEveryExpirationDuringSet(t *testing.T) {
	now := time.Unix(3_000, 0)
	cache, metrics := newTestParseCache(&now, 4, 4, time.Second)
	cache.putSyntax("first", &SyntaxError{Message: "first"})
	cache.putSyntax("second", &SyntaxError{Message: "second"})

	now = now.Add(2 * time.Second)
	cache.putSyntax("replacement", &SyntaxError{Message: "replacement"})

	assertCacheEvents(t, metrics, map[string]int{"eviction:syntax": 2})
}

func TestParseCache_PutObservesCapacityEviction(t *testing.T) {
	now := time.Unix(4_000, 0)
	cache, metrics := newTestParseCache(&now, 1, 1, time.Hour)
	cache.putSuccess("first", &model.Diagram{})
	cache.putSuccess("second", &model.Diagram{})

	assertCacheEvents(t, metrics, map[string]int{"eviction:success": 1})
}

func TestParseCache_GetSuccessReturnedDiagramMutationDoesNotAffectCachedDiagram(t *testing.T) {
	cache := newParseCache()
	const key = "flowchart:mutation"

	nodeLine := 10
	nodeColumn := 20
	edgeLine := 30
	edgeColumn := 40

	cache.putSuccess(key, &model.Diagram{
		Type:      model.DiagramTypeFlowchart,
		Direction: "TD",
		Nodes: []model.Node{{
			ID:     "A",
			Label:  "Node A",
			Line:   &nodeLine,
			Column: &nodeColumn,
		}},
		Edges: []model.Edge{{
			From:   "A",
			To:     "B",
			Type:   "-->",
			Line:   &edgeLine,
			Column: &edgeColumn,
		}},
		Subgraphs:           []model.Subgraph{{ID: "S", Label: "Group", Nodes: []string{"A", "B"}}},
		Suppressions:        []model.SuppressionDirective{{RuleID: "rule", Scope: "line", Line: 1, TargetLine: 2, SubgraphID: "S"}},
		SourceNodeIDs:       []string{"A", "B"},
		StartStates:         []string{"A"},
		DisconnectedNodeIDs: []string{"B"},
		DuplicateNodeIDs:    []string{"A"},
	})

	firstRead, ok := cache.getSuccess(key)
	if !ok {
		t.Fatalf("expected cache hit on first read")
	}
	if firstRead.Nodes[0].Line == nil || firstRead.Nodes[0].Column == nil || firstRead.Edges[0].Line == nil || firstRead.Edges[0].Column == nil {
		t.Fatalf("expected first read to include node/edge positions")
	}

	*firstRead.Nodes[0].Line = 101
	*firstRead.Nodes[0].Column = 102
	*firstRead.Edges[0].Line = 103
	*firstRead.Edges[0].Column = 104
	firstRead.Nodes[0].ID = "mutated-node"
	firstRead.Edges[0].Type = "---"
	firstRead.Subgraphs[0].Nodes[0] = "mutated-subgraph-node"
	firstRead.Suppressions[0].RuleID = "mutated-rule"
	firstRead.SourceNodeIDs[0] = "mutated-source"
	firstRead.StartStates[0] = "mutated-start"
	firstRead.DisconnectedNodeIDs[0] = "mutated-disconnected"
	firstRead.DuplicateNodeIDs[0] = "mutated-duplicate"

	secondRead, ok := cache.getSuccess(key)
	if !ok {
		t.Fatalf("expected cache hit on second read")
	}

	if got := *secondRead.Nodes[0].Line; got != 10 {
		t.Fatalf("expected cached node line to remain 10, got %d", got)
	}
	if got := *secondRead.Nodes[0].Column; got != 20 {
		t.Fatalf("expected cached node column to remain 20, got %d", got)
	}
	if got := *secondRead.Edges[0].Line; got != 30 {
		t.Fatalf("expected cached edge line to remain 30, got %d", got)
	}
	if got := *secondRead.Edges[0].Column; got != 40 {
		t.Fatalf("expected cached edge column to remain 40, got %d", got)
	}
	if got := secondRead.Nodes[0].ID; got != "A" {
		t.Fatalf("expected cached node ID to remain A, got %q", got)
	}
	if got := secondRead.Edges[0].Type; got != "-->" {
		t.Fatalf("expected cached edge type to remain -->, got %q", got)
	}
	if got := secondRead.Subgraphs[0].Nodes[0]; got != "A" {
		t.Fatalf("expected cached subgraph nodes to remain unchanged, got %q", got)
	}
	if got := secondRead.Suppressions[0].RuleID; got != "rule" {
		t.Fatalf("expected cached suppression to remain unchanged, got %q", got)
	}
	if got := secondRead.SourceNodeIDs[0]; got != "A" {
		t.Fatalf("expected cached source node IDs to remain unchanged, got %q", got)
	}
	if got := secondRead.StartStates[0]; got != "A" {
		t.Fatalf("expected cached start states to remain unchanged, got %q", got)
	}
	if got := secondRead.DisconnectedNodeIDs[0]; got != "B" {
		t.Fatalf("expected cached disconnected node IDs to remain unchanged, got %q", got)
	}
	if got := secondRead.DuplicateNodeIDs[0]; got != "A" {
		t.Fatalf("expected cached duplicate node IDs to remain unchanged, got %q", got)
	}
}

func TestParseCache_GetReturnedDiagramMutationDoesNotAffectCachedDiagram(t *testing.T) {
	cache := newParseCache()
	const key = "flowchart:get"

	nodeLine := 7
	edgeColumn := 9
	cache.putSuccess(key, &model.Diagram{
		Nodes: []model.Node{{ID: "A", Line: &nodeLine}},
		Edges: []model.Edge{{From: "A", To: "B", Type: "-->", Column: &edgeColumn}},
	})

	firstRead, syntaxErr, ok := cache.get(key)
	if !ok || syntaxErr != nil || firstRead == nil {
		t.Fatalf("expected successful cache read, got diagram=%v syntaxErr=%v ok=%v", firstRead, syntaxErr, ok)
	}

	*firstRead.Nodes[0].Line = 70
	*firstRead.Edges[0].Column = 90

	secondRead, syntaxErr, ok := cache.get(key)
	if !ok || syntaxErr != nil || secondRead == nil {
		t.Fatalf("expected successful second cache read, got diagram=%v syntaxErr=%v ok=%v", secondRead, syntaxErr, ok)
	}
	if got := *secondRead.Nodes[0].Line; got != 7 {
		t.Fatalf("expected node line to remain 7, got %d", got)
	}
	if got := *secondRead.Edges[0].Column; got != 9 {
		t.Fatalf("expected edge column to remain 9, got %d", got)
	}
}

func TestParseCache_GetReturnsSyntaxAfterSuccessOverwrite(t *testing.T) {
	cache := newParseCache()
	const key = "flowchart:success-then-syntax"

	cache.putSuccess(key, &model.Diagram{Nodes: []model.Node{{ID: "A"}}})
	cache.putSyntax(key, &SyntaxError{Message: "bad token", Line: 3, Column: 7})

	diagram, syntaxErr, ok := cache.get(key)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if diagram != nil {
		t.Fatalf("expected success entry to be replaced by syntax entry")
	}
	if syntaxErr == nil {
		t.Fatalf("expected syntax entry")
	}
	if got := syntaxErr.Message; got != "bad token" {
		t.Fatalf("expected syntax message bad token, got %q", got)
	}
}

func TestParseCache_GetReturnsSuccessAfterSyntaxOverwrite(t *testing.T) {
	cache := newParseCache()
	const key = "flowchart:syntax-then-success"

	cache.putSyntax(key, &SyntaxError{Message: "old syntax", Line: 1, Column: 1})
	cache.putSuccess(key, &model.Diagram{Nodes: []model.Node{{ID: "B"}}})

	diagram, syntaxErr, ok := cache.get(key)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if syntaxErr != nil {
		t.Fatalf("expected syntax entry to be replaced by success entry")
	}
	if diagram == nil {
		t.Fatalf("expected success entry")
	}
	if got := diagram.Nodes[0].ID; got != "B" {
		t.Fatalf("expected diagram node ID B, got %q", got)
	}
}

func TestParseCache_ConcurrentOverwritesKeepSingleEntryTypePerKey(t *testing.T) {
	cache := newParseCache()
	const key = "flowchart:concurrent-overwrite"

	for i := 0; i < 500; i++ {
		start := make(chan struct{})
		done := make(chan struct{}, 2)

		go func(iter int) {
			<-start
			cache.putSuccess(key, &model.Diagram{Nodes: []model.Node{{ID: "success"}}})
			done <- struct{}{}
		}(i)

		go func(iter int) {
			<-start
			cache.putSyntax(key, &SyntaxError{Message: "syntax", Line: iter + 1, Column: 1})
			done <- struct{}{}
		}(i)

		close(start)
		<-done
		<-done

		cache.entries.RLock()
		_, successOK, _ := cache.success.Get(key)
		_, syntaxOK, _ := cache.syntax.Get(key)
		cache.entries.RUnlock()

		if successOK == syntaxOK {
			t.Fatalf("expected exactly one cache entry type after concurrent overwrite, got success=%v syntax=%v on iter=%d", successOK, syntaxOK, i)
		}
	}
}
