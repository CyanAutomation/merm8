package parser

import (
	"container/heap"
	"container/list"
	"encoding/json"
	"sync"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

// CacheMetricsObserver receives parser cache events for telemetry.
type CacheMetricsObserver interface {
	ObserveParserCacheEvent(result, entryType string)
}

type CacheMemoryMetricsObserver interface {
	ObserveParserCacheBytes(entryType string, bytes int)
}

type parseCache struct {
	success *lruTTLCache[*model.Diagram]
	syntax  *lruTTLCache[*SyntaxError]
	entries sync.RWMutex

	metricsMu sync.RWMutex
	metrics   CacheMetricsObserver
}

func newParseCache(cfg CacheConfig) *parseCache {
	cfg = cfg.EffectiveConfig()
	successCapacity, syntaxCapacity := cfg.SuccessCapacity, cfg.SyntaxCapacity
	if cfg.SuccessMaxBytes == 0 {
		successCapacity = 0
	}
	if cfg.SyntaxMaxBytes == 0 {
		syntaxCapacity = 0
	}
	return &parseCache{
		success: newLRUTTLCache[*model.Diagram](successCapacity, cfg.SuccessTTL, cfg.SuccessMaxBytes),
		syntax:  newLRUTTLCache[*SyntaxError](syntaxCapacity, cfg.SyntaxTTL, cfg.SyntaxMaxBytes),
	}
}

func (c *parseCache) setMetrics(metrics CacheMetricsObserver) {
	if c == nil {
		return
	}
	c.metricsMu.Lock()
	c.metrics = metrics
	c.metricsMu.Unlock()
}

func (c *parseCache) get(key string) (*model.Diagram, *SyntaxError, bool) {
	if c == nil {
		return nil, nil, false
	}
	c.entries.RLock()
	v, successOK, successRemoved := c.success.Get(key)
	if successOK {
		c.entries.RUnlock()
		if successRemoved > 0 {
			c.observeMemory()
		}
		c.observeRemovals(successRemoved, "success")
		c.observe("hit", "success")
		return cloneDiagram(v), nil, true
	}
	syntaxErr, syntaxOK, syntaxRemoved := c.syntax.Get(key)
	if syntaxOK {
		c.entries.RUnlock()
		if successRemoved+syntaxRemoved > 0 {
			c.observeMemory()
		}
		c.observeRemovals(successRemoved, "success")
		c.observeRemovals(syntaxRemoved, "syntax")
		c.observe("hit", "syntax")
		return nil, cloneSyntaxError(syntaxErr), true
	}
	c.entries.RUnlock()
	if successRemoved+syntaxRemoved > 0 {
		c.observeMemory()
	}
	c.observeRemovals(successRemoved, "success")
	c.observeRemovals(syntaxRemoved, "syntax")
	c.observe("miss", "any")
	return nil, nil, false
}

func (c *parseCache) putSuccess(key string, diagram *model.Diagram) {
	if c == nil || diagram == nil {
		return
	}
	c.entries.Lock()
	c.syntax.Delete(key)
	removed := c.success.SetWeighted(key, cloneDiagram(diagram), estimatedEntryBytes(diagram))
	c.entries.Unlock()
	c.observeMemory()
	c.observeRemovals(removed, "success")
}

func (c *parseCache) putSyntax(key string, syntaxErr *SyntaxError) {
	if c == nil || syntaxErr == nil {
		return
	}
	c.entries.Lock()
	c.success.Delete(key)
	removed := c.syntax.SetWeighted(key, cloneSyntaxError(syntaxErr), estimatedEntryBytes(syntaxErr))
	c.entries.Unlock()
	c.observeMemory()
	c.observeRemovals(removed, "syntax")
}

func (c *parseCache) observeMemory() {
	c.metricsMu.RLock()
	metrics := c.metrics
	c.metricsMu.RUnlock()
	observer, ok := metrics.(CacheMemoryMetricsObserver)
	if !ok {
		return
	}
	observer.ObserveParserCacheBytes("success", c.success.ByteSize())
	observer.ObserveParserCacheBytes("syntax", c.syntax.ByteSize())
}

func estimatedEntryBytes(value any) int {
	serialized, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	// JSON size is a stable lower bound; multiplying by two roughly accounts
	// for Go string headers, slices, maps, and copied values retained in entries.
	return len(serialized)*2 + 128
}

func (c *parseCache) observeRemovals(removed int, entryType string) {
	for i := 0; i < removed; i++ {
		c.observe("eviction", entryType)
	}
}

func (c *parseCache) observe(result, entryType string) {
	c.metricsMu.RLock()
	metrics := c.metrics
	c.metricsMu.RUnlock()
	if metrics != nil {
		metrics.ObserveParserCacheEvent(result, entryType)
	}
}

type lruTTLCache[T any] struct {
	mu       sync.Mutex
	ttl      time.Duration
	maxSize  int
	maxBytes int
	bytes    int
	entries  map[string]*list.Element
	order    *list.List
	expiry   expirationHeap
	now      func() time.Time
}

type lruTTLCacheEntry[T any] struct {
	key        string
	value      T
	expiresAt  time.Time
	generation uint64
	weight     int
}

// expirationRecord is deliberately separate from the LRU list. Records are
// immutable; when an entry is updated or deleted, its old record is discarded
// lazily after the generation no longer matches the live entry.
type expirationRecord struct {
	key        string
	expiresAt  time.Time
	generation uint64
}

type expirationHeap []expirationRecord

func (h expirationHeap) Len() int           { return len(h) }
func (h expirationHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h expirationHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expirationHeap) Push(value any)    { *h = append(*h, value.(expirationRecord)) }
func (h *expirationHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	old[last] = expirationRecord{}
	*h = old[:last]
	return value
}

func newLRUTTLCache[T any](maxSize int, ttl time.Duration, maxBytes ...int) *lruTTLCache[T] {
	byteLimit := 0
	if len(maxBytes) > 0 {
		byteLimit = maxBytes[0]
	}
	return &lruTTLCache[T]{
		ttl:      ttl,
		maxSize:  maxSize,
		maxBytes: byteLimit,
		entries:  make(map[string]*list.Element),
		order:    list.New(),
		now:      time.Now,
	}
}

func (c *lruTTLCache[T]) Get(key string) (T, bool, int) {
	var zero T
	if c.maxSize <= 0 || c.ttl <= 0 {
		return zero, false, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	evicted := c.evictExpiredLocked(now)

	elem, ok := c.entries[key]
	if !ok {
		return zero, false, evicted
	}

	entry := elem.Value.(*lruTTLCacheEntry[T])
	if !now.Before(entry.expiresAt) {
		c.removeElementLocked(elem)
		return zero, false, evicted + 1
	}

	c.order.MoveToFront(elem)
	return entry.value, true, evicted
}

func (c *lruTTLCache[T]) Set(key string, value T) int {
	return c.SetWeighted(key, value, 1)
}

// SetWeighted bounds retained memory using the caller's estimated entry size.
func (c *lruTTLCache[T]) SetWeighted(key string, value T, weight int) int {
	if c.maxSize <= 0 || c.ttl <= 0 {
		return 0
	}
	if weight < 0 {
		weight = 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	removed := c.evictExpiredLocked(now)

	if elem, ok := c.entries[key]; ok {
		entry := elem.Value.(*lruTTLCacheEntry[T])
		if c.maxBytes > 0 && weight > c.maxBytes {
			c.removeElementLocked(elem)
			return removed + 1
		}
		c.bytes -= entry.weight
		entry.value = value
		entry.weight = weight
		c.bytes += weight
		entry.expiresAt = now.Add(c.ttl)
		entry.generation++
		heap.Push(&c.expiry, expirationRecord{key: key, expiresAt: entry.expiresAt, generation: entry.generation})
		c.order.MoveToFront(elem)
		return removed
	}

	if c.maxBytes > 0 && weight > c.maxBytes {
		return removed
	}
	entry := &lruTTLCacheEntry[T]{key: key, value: value, expiresAt: now.Add(c.ttl), generation: 1, weight: weight}
	elem := c.order.PushFront(entry)
	c.entries[key] = elem
	c.bytes += weight
	heap.Push(&c.expiry, expirationRecord{key: key, expiresAt: entry.expiresAt, generation: entry.generation})

	for c.order.Len() > c.maxSize || (c.maxBytes > 0 && c.bytes > c.maxBytes) {
		oldest := c.order.Back()
		if oldest != nil {
			c.removeElementLocked(oldest)
			removed++
		} else {
			break
		}
	}

	return removed
}

func (c *lruTTLCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[key]; ok {
		c.removeElementLocked(elem)
	}
}

func (c *lruTTLCache[T]) ByteSize() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

func (c *lruTTLCache[T]) evictExpiredLocked(now time.Time) int {
	evicted := 0
	for c.expiry.Len() > 0 && !now.Before(c.expiry[0].expiresAt) {
		record := heap.Pop(&c.expiry).(expirationRecord)
		elem, ok := c.entries[record.key]
		if !ok {
			continue
		}
		entry := elem.Value.(*lruTTLCacheEntry[T])
		if entry.generation != record.generation || !entry.expiresAt.Equal(record.expiresAt) {
			continue
		}
		c.removeElementLocked(elem)
		evicted++
	}
	return evicted
}

func (c *lruTTLCache[T]) removeElementLocked(elem *list.Element) {
	entry := elem.Value.(*lruTTLCacheEntry[T])
	delete(c.entries, entry.key)
	c.bytes -= entry.weight
	c.order.Remove(elem)
}

func cloneDiagram(diagram *model.Diagram) *model.Diagram {
	if diagram == nil {
		return nil
	}
	copied := *diagram
	copied.Nodes = append([]model.Node(nil), diagram.Nodes...)
	for i := range copied.Nodes {
		if copied.Nodes[i].Line != nil {
			line := *copied.Nodes[i].Line
			copied.Nodes[i].Line = &line
		}
		if copied.Nodes[i].Column != nil {
			column := *copied.Nodes[i].Column
			copied.Nodes[i].Column = &column
		}
	}
	copied.Edges = append([]model.Edge(nil), diagram.Edges...)
	for i := range copied.Edges {
		if copied.Edges[i].Line != nil {
			line := *copied.Edges[i].Line
			copied.Edges[i].Line = &line
		}
		if copied.Edges[i].Column != nil {
			column := *copied.Edges[i].Column
			copied.Edges[i].Column = &column
		}
	}
	copied.Subgraphs = append([]model.Subgraph(nil), diagram.Subgraphs...)
	for i := range copied.Subgraphs {
		copied.Subgraphs[i].Nodes = append([]string(nil), diagram.Subgraphs[i].Nodes...)
	}
	copied.Suppressions = append([]model.SuppressionDirective(nil), diagram.Suppressions...)
	copied.SourceNodeIDs = append([]string(nil), diagram.SourceNodeIDs...)
	copied.StartStates = append([]string(nil), diagram.StartStates...)
	copied.DisconnectedNodeIDs = append([]string(nil), diagram.DisconnectedNodeIDs...)
	copied.DuplicateNodeIDs = append([]string(nil), diagram.DuplicateNodeIDs...)
	return &copied
}

func cloneSyntaxError(err *SyntaxError) *SyntaxError {
	if err == nil {
		return nil
	}
	copied := *err
	return &copied
}
