package parser

import (
	"fmt"
	"strconv"
	"sync"
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

func TestLRUTTLCacheExpirationOrderDiffersFromLRUOrder(t *testing.T) {
	now := time.Unix(5_000, 0)
	cache := newLRUTTLCache[string](3, time.Second)
	cache.now = func() time.Time { return now }

	cache.Set("expires-first", "first")
	now = now.Add(500 * time.Millisecond)
	cache.Set("expires-last", "last")
	if _, ok, _ := cache.Get("expires-first"); !ok {
		t.Fatal("expected access to make earliest-expiring entry most recent")
	}

	now = now.Add(600 * time.Millisecond)
	if _, ok, removed := cache.Get("missing"); ok || removed != 1 {
		t.Fatalf("expected exactly the earliest expiration to be removed, got ok=%v removed=%d", ok, removed)
	}
	if value, ok, removed := cache.Get("expires-last"); !ok || value != "last" || removed != 0 {
		t.Fatalf("expected later expiration to remain, got value=%q ok=%v removed=%d", value, ok, removed)
	}
}

func TestLRUTTLCacheUpdateDiscardsStaleExpirationRecord(t *testing.T) {
	now := time.Unix(6_000, 0)
	cache := newLRUTTLCache[string](2, time.Second)
	cache.now = func() time.Time { return now }

	cache.Set("updated", "old")
	now = now.Add(500 * time.Millisecond)
	cache.Set("updated", "new")
	now = now.Add(600 * time.Millisecond)

	if value, ok, removed := cache.Get("updated"); !ok || value != "new" || removed != 0 {
		t.Fatalf("stale expiration removed updated entry: value=%q ok=%v removed=%d", value, ok, removed)
	}
	if got := len(cache.entries); got != 1 {
		t.Fatalf("cache contains %d live entries, want 1", got)
	}
}

func TestLRUTTLCacheExpirationAndCapacityPressure(t *testing.T) {
	now := time.Unix(7_000, 0)
	cache := newLRUTTLCache[string](2, time.Second)
	cache.now = func() time.Time { return now }

	cache.Set("expired", "expired")
	now = now.Add(500 * time.Millisecond)
	cache.Set("live", "live")
	now = now.Add(600 * time.Millisecond)
	if removed := cache.Set("new", "new"); removed != 1 {
		t.Fatalf("Set removed %d entries, want exactly one expired entry", removed)
	}
	if cache.order.Len() != 2 || len(cache.entries) != 2 {
		t.Fatalf("cache occupancy = list:%d map:%d, want 2", cache.order.Len(), len(cache.entries))
	}
	for _, key := range []string{"live", "new"} {
		if _, ok, removed := cache.Get(key); !ok || removed != 0 {
			t.Fatalf("expected %q to survive capacity pressure, got ok=%v removed=%d", key, ok, removed)
		}
	}
}

// @spec: CACHE-003: Concurrent cache operations preserve non-evicted values.
func TestLRUTTLCacheConcurrentAccessPreservesValues(t *testing.T) {
	const (
		goroutines = 16
		perWorker  = 64
	)
	cache := newLRUTTLCache[int](goroutines*perWorker, time.Hour)
	start := make(chan struct{})
	failures := make(chan string, 2*goroutines*perWorker)
	var workers sync.WaitGroup
	workers.Add(goroutines)
	for worker := 0; worker < goroutines; worker++ {
		go func(worker int) {
			defer workers.Done()
			<-start
			for operation := 0; operation < perWorker; operation++ {
				key := fmt.Sprintf("%d:%d", worker, operation)
				want := worker*perWorker + operation
				if removed := cache.Set(key, want); removed != 0 {
					failures <- fmt.Sprintf("Set(%q) removed %d entries, want none", key, removed)
				}
				if got, ok, removed := cache.Get(key); !ok || got != want || removed != 0 {
					failures <- fmt.Sprintf("Get(%q) = (%d, %t, %d), want (%d, true, 0)", key, got, ok, removed, want)
				}
			}
		}(worker)
	}
	close(start)
	workers.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}

	for worker := 0; worker < goroutines; worker++ {
		for operation := 0; operation < perWorker; operation++ {
			key := fmt.Sprintf("%d:%d", worker, operation)
			want := worker*perWorker + operation
			if got, ok, removed := cache.Get(key); !ok || got != want || removed != 0 {
				t.Fatalf("Get(%q) after concurrent writes = (%d, %t, %d), want (%d, true, 0)", key, got, ok, removed, want)
			}
		}
	}
}

func BenchmarkLRUTTLCacheHit(b *testing.B) {
	for _, occupancy := range []int{1, maxParseCacheCapacity} {
		b.Run(fmt.Sprintf("occupancy-%d", occupancy), func(b *testing.B) {
			cache := newLRUTTLCache[int](occupancy, time.Hour)
			for i := 0; i < occupancy; i++ {
				cache.Set(strconv.Itoa(i), i)
			}
			key := strconv.Itoa(occupancy / 2)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, ok, _ := cache.Get(key); !ok {
					b.Fatal("unexpected cache miss")
				}
			}
		})
	}
}

// @spec: CACHE-001: Cache reads return deep copies that callers can mutate safely
func TestParseCache_GetReturnedDiagramMutationDoesNotAffectCachedDiagram(t *testing.T) {
	cache := newParseCache(DefaultCacheConfig())
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

	firstRead, syntaxErr, ok := cache.get(key)
	if !ok || syntaxErr != nil || firstRead == nil {
		t.Fatalf("expected successful cache read, got diagram=%v syntaxErr=%v ok=%v", firstRead, syntaxErr, ok)
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

	secondRead, syntaxErr, ok := cache.get(key)
	if !ok || syntaxErr != nil || secondRead == nil {
		t.Fatalf("expected successful second cache read, got diagram=%v syntaxErr=%v ok=%v", secondRead, syntaxErr, ok)
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

func TestParseCache_GetReturnsSyntaxAfterSuccessOverwrite(t *testing.T) {
	cache := newParseCache(DefaultCacheConfig())
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
	cache := newParseCache(DefaultCacheConfig())
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
	cache := newParseCache(DefaultCacheConfig())
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

func TestCacheConfigFromEnv(t *testing.T) {
	for _, name := range []string{
		"PARSER_CACHE_SUCCESS_CAPACITY", "PARSER_CACHE_SUCCESS_TTL_SECONDS",
		"PARSER_CACHE_SYNTAX_CAPACITY", "PARSER_CACHE_SYNTAX_TTL_SECONDS",
	} {
		t.Setenv(name, "")
	}

	if got, want := CacheConfigFromEnv(), DefaultCacheConfig(); got != want {
		t.Fatalf("default cache config = %+v, want %+v", got, want)
	}

	t.Setenv("PARSER_CACHE_SUCCESS_CAPACITY", "100")
	t.Setenv("PARSER_CACHE_SUCCESS_TTL_SECONDS", "45")
	t.Setenv("PARSER_CACHE_SYNTAX_CAPACITY", "50")
	t.Setenv("PARSER_CACHE_SYNTAX_TTL_SECONDS", "10")
	want := CacheConfig{SuccessCapacity: 100, SuccessTTL: 45 * time.Second, SyntaxCapacity: 50, SyntaxTTL: 10 * time.Second}
	if got := CacheConfigFromEnv(); got != want {
		t.Fatalf("overridden cache config = %+v, want %+v", got, want)
	}
}

func TestCacheConfigFromEnvInvalidValuesUseDefaults(t *testing.T) {
	t.Setenv("PARSER_CACHE_SUCCESS_CAPACITY", "-1")
	t.Setenv("PARSER_CACHE_SUCCESS_TTL_SECONDS", "not-a-number")
	t.Setenv("PARSER_CACHE_SYNTAX_CAPACITY", "10001")
	t.Setenv("PARSER_CACHE_SYNTAX_TTL_SECONDS", "86401")
	if got, want := CacheConfigFromEnv(), DefaultCacheConfig(); got != want {
		t.Fatalf("invalid cache config = %+v, want defaults %+v", got, want)
	}
}

func TestParseCacheDisabledByZeroCapacityOrTTL(t *testing.T) {
	cache := newParseCache(CacheConfig{
		SuccessCapacity: 0,
		SuccessTTL:      time.Hour,
		SyntaxCapacity:  10,
		SyntaxTTL:       0,
	})
	cache.putSuccess("success", &model.Diagram{})
	cache.putSyntax("syntax", &SyntaxError{Message: "bad"})
	if _, _, ok := cache.get("success"); ok {
		t.Fatal("zero capacity must disable the success cache")
	}
	if _, _, ok := cache.get("syntax"); ok {
		t.Fatal("zero TTL must disable the syntax cache")
	}
	if len(cache.success.entries) != 0 || len(cache.syntax.entries) != 0 {
		t.Fatal("disabled caches must not retain entries")
	}
}

func TestCacheConfigEffectiveConfigNormalizesUnsafeValues(t *testing.T) {
	got := (CacheConfig{
		SuccessCapacity: -1,
		SuccessTTL:      -time.Second,
		SyntaxCapacity:  maxParseCacheCapacity + 1,
		SyntaxTTL:       maxParseCacheTTL + time.Second,
	}).EffectiveConfig()
	defaults := DefaultCacheConfig()
	if got.SuccessCapacity != defaults.SuccessCapacity || got.SuccessTTL != defaults.SuccessTTL {
		t.Fatalf("negative values were not safely defaulted: %+v", got)
	}
	if got.SyntaxCapacity != maxParseCacheCapacity || got.SyntaxTTL != maxParseCacheTTL {
		t.Fatalf("excessive values were not capped: %+v", got)
	}
}

func TestNewUsesCacheConfigFromEnv(t *testing.T) {
	t.Setenv("PARSER_CACHE_SUCCESS_CAPACITY", "7")
	t.Setenv("PARSER_CACHE_SUCCESS_TTL_SECONDS", "8")
	t.Setenv("PARSER_CACHE_SYNTAX_CAPACITY", "9")
	t.Setenv("PARSER_CACHE_SYNTAX_TTL_SECONDS", "10")

	p, err := newWithConfigAndCacheAndRepoRootResolver("parse.mjs", Config{}, CacheConfigFromEnv(), func() (string, error) {
		return t.TempDir(), nil
	})
	if err != nil {
		t.Fatalf("construct parser: %v", err)
	}
	if p.cache.success.maxSize != 7 || p.cache.success.ttl != 8*time.Second ||
		p.cache.syntax.maxSize != 9 || p.cache.syntax.ttl != 10*time.Second {
		t.Fatalf("parser did not receive environment cache policy: success=%d/%s syntax=%d/%s",
			p.cache.success.maxSize, p.cache.success.ttl, p.cache.syntax.maxSize, p.cache.syntax.ttl)
	}
}
