package api

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"

	"github.com/CyanAutomation/merm8/internal/model"
)

const (
	defaultAnalysisCacheEntries = 256
	defaultAnalysisCacheTTL     = 30 * time.Second
	defaultAnalysisCacheBytes   = 8 << 20
)

type analysisCoreResult struct {
	Issues  []model.Issue    `json:"issues"`
	Metrics *metricsResponse `json:"metrics"`
}

type analysisCacheEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
}

type analysisResultCache struct {
	mu         sync.Mutex
	maxEntries int
	maxBytes   int
	ttl        time.Duration
	bytes      int
	entries    map[string]*list.Element
	order      *list.List
	now        func() time.Time
}

func newAnalysisResultCache(maxEntries int, ttl time.Duration, maxBytes int, now func() time.Time) *analysisResultCache {
	if now == nil {
		now = time.Now
	}
	return &analysisResultCache{
		maxEntries: maxEntries,
		ttl:        ttl,
		maxBytes:   maxBytes,
		entries:    make(map[string]*list.Element),
		order:      list.New(),
		now:        now,
	}
}

func (c *analysisResultCache) Get(key string) (analysisCoreResult, bool, int) {
	var zero analysisCoreResult
	if c == nil {
		return zero, false, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := c.pruneLocked()
	elem := c.entries[key]
	if elem == nil {
		return zero, false, removed
	}
	c.order.MoveToFront(elem)
	entry := elem.Value.(*analysisCacheEntry)
	var result analysisCoreResult
	if err := json.Unmarshal(entry.value, &result); err != nil {
		c.removeLocked(elem)
		return zero, false, removed + 1
	}
	return result, true, removed
}

func (c *analysisResultCache) Set(key string, result analysisCoreResult) int {
	if c == nil || c.maxEntries <= 0 || c.ttl <= 0 || c.maxBytes <= 0 {
		return 0
	}
	value, err := json.Marshal(result)
	if err != nil {
		return 0
	}
	weight := len(key) + len(value)
	if weight > c.maxBytes {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := c.pruneLocked()
	if elem := c.entries[key]; elem != nil {
		c.removeLocked(elem)
	}
	entry := &analysisCacheEntry{key: key, value: value, expiresAt: c.now().Add(c.ttl)}
	c.entries[key] = c.order.PushFront(entry)
	c.bytes += weight
	for c.order.Len() > c.maxEntries || c.bytes > c.maxBytes {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.removeLocked(oldest)
		removed++
	}
	return removed
}

func (c *analysisResultCache) pruneLocked() int {
	now := c.now()
	removed := 0
	for elem := c.order.Back(); elem != nil; {
		previous := elem.Prev()
		if !now.Before(elem.Value.(*analysisCacheEntry).expiresAt) {
			c.removeLocked(elem)
			removed++
		}
		elem = previous
	}
	return removed
}

func (c *analysisResultCache) removeLocked(elem *list.Element) {
	entry := elem.Value.(*analysisCacheEntry)
	delete(c.entries, entry.key)
	c.bytes -= len(entry.key) + len(entry.value)
	c.order.Remove(elem)
}
