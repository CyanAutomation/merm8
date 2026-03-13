package parser

import (
	"container/list"
	"sync"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

const (
	defaultParseSuccessCacheTTL  = 30 * time.Second
	defaultParseSyntaxCacheTTL   = 15 * time.Second
	defaultParseSuccessCacheSize = 256
	defaultParseSyntaxCacheSize  = 256
)

// CacheMetricsObserver receives parser cache events for telemetry.
type CacheMetricsObserver interface {
	ObserveParserCacheEvent(result, entryType string)
}

type parseCache struct {
	success *lruTTLCache[*model.Diagram]
	syntax  *lruTTLCache[*SyntaxError]
	entries sync.RWMutex

	metricsMu sync.RWMutex
	metrics   CacheMetricsObserver
}

func newParseCache() *parseCache {
	return &parseCache{
		success: newLRUTTLCache[*model.Diagram](defaultParseSuccessCacheSize, defaultParseSuccessCacheTTL),
		syntax:  newLRUTTLCache[*SyntaxError](defaultParseSyntaxCacheSize, defaultParseSyntaxCacheTTL),
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

func (c *parseCache) getSuccess(key string) (*model.Diagram, bool) {
	if c == nil {
		return nil, false
	}
	c.entries.RLock()
	v, ok, evicted := c.success.Get(key)
	c.entries.RUnlock()
	for i := 0; i < evicted; i++ {
		c.observe("eviction", "success")
	}
	if !ok {
		c.observe("miss", "success")
		return nil, false
	}
	c.observe("hit", "success")
	return cloneDiagram(v), true
}

func (c *parseCache) get(key string) (*model.Diagram, *SyntaxError, bool) {
	if c == nil {
		return nil, nil, false
	}
	c.entries.RLock()
	if v, ok, evicted := c.success.Get(key); ok {
		c.entries.RUnlock()
		for i := 0; i < evicted; i++ {
			c.observe("eviction", "success")
		}
		c.observe("hit", "success")
		return cloneDiagram(v), nil, true
	}
	if v, ok, evicted := c.syntax.Get(key); ok {
		c.entries.RUnlock()
		for i := 0; i < evicted; i++ {
			c.observe("eviction", "syntax")
		}
		c.observe("hit", "syntax")
		return nil, cloneSyntaxError(v), true
	}
	c.entries.RUnlock()
	c.observe("miss", "any")
	return nil, nil, false
}

func (c *parseCache) putSuccess(key string, diagram *model.Diagram) {
	if c == nil || diagram == nil {
		return
	}
	c.entries.Lock()
	c.syntax.Delete(key)
	evicted := c.success.Set(key, cloneDiagram(diagram))
	c.entries.Unlock()
	if evicted {
		c.observe("eviction", "success")
	}
}

func (c *parseCache) putSyntax(key string, syntaxErr *SyntaxError) {
	if c == nil || syntaxErr == nil {
		return
	}
	c.entries.Lock()
	c.success.Delete(key)
	evicted := c.syntax.Set(key, cloneSyntaxError(syntaxErr))
	c.entries.Unlock()
	if evicted {
		c.observe("eviction", "syntax")
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
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	entries map[string]*list.Element
	order   *list.List
}

type lruTTLCacheEntry[T any] struct {
	key       string
	value     T
	expiresAt time.Time
}

func newLRUTTLCache[T any](maxSize int, ttl time.Duration) *lruTTLCache[T] {
	return &lruTTLCache[T]{
		ttl:     ttl,
		maxSize: maxSize,
		entries: make(map[string]*list.Element),
		order:   list.New(),
	}
}

func (c *lruTTLCache[T]) Get(key string) (T, bool, int) {
	var zero T

	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := c.evictExpiredLocked(time.Now())

	elem, ok := c.entries[key]
	if !ok {
		return zero, false, evicted
	}

	entry := elem.Value.(*lruTTLCacheEntry[T])
	if time.Now().After(entry.expiresAt) {
		c.removeElementLocked(elem)
		return zero, false, evicted + 1
	}

	c.order.MoveToFront(elem)
	return entry.value, true, evicted
}

func (c *lruTTLCache[T]) Set(key string, value T) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.evictExpiredLocked(now)

	if elem, ok := c.entries[key]; ok {
		entry := elem.Value.(*lruTTLCacheEntry[T])
		entry.value = value
		entry.expiresAt = now.Add(c.ttl)
		c.order.MoveToFront(elem)
		return false
	}

	entry := &lruTTLCacheEntry[T]{key: key, value: value, expiresAt: now.Add(c.ttl)}
	elem := c.order.PushFront(entry)
	c.entries[key] = elem

	if c.order.Len() > c.maxSize {
		oldest := c.order.Back()
		if oldest != nil {
			c.removeElementLocked(oldest)
			return true
		}
	}

	return false
}

func (c *lruTTLCache[T]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.entries[key]; ok {
		c.removeElementLocked(elem)
	}
}

func (c *lruTTLCache[T]) evictExpiredLocked(now time.Time) int {
	evicted := 0
	for elem := c.order.Back(); elem != nil; {
		prev := elem.Prev()
		entry := elem.Value.(*lruTTLCacheEntry[T])
		if now.After(entry.expiresAt) {
			c.removeElementLocked(elem)
			evicted++
		}
		elem = prev
	}
	return evicted
}

func (c *lruTTLCache[T]) removeElementLocked(elem *list.Element) {
	entry := elem.Value.(*lruTTLCacheEntry[T])
	delete(c.entries, entry.key)
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
