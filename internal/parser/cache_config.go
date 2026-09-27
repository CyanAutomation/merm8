package parser

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultParseSuccessCacheTTL  = 30 * time.Second
	defaultParseSyntaxCacheTTL   = 15 * time.Second
	defaultParseSuccessCacheSize = 256
	defaultParseSyntaxCacheSize  = 256

	maxParseCacheCapacity = 10_000
	maxParseCacheTTL      = 24 * time.Hour
)

// CacheConfig controls the independent successful-parse and syntax-error
// caches. A zero capacity or TTL disables the corresponding cache.
type CacheConfig struct {
	SuccessCapacity int
	SuccessTTL      time.Duration
	SyntaxCapacity  int
	SyntaxTTL       time.Duration
}

// DefaultCacheConfig returns the cache policy used when no overrides are set.
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		SuccessCapacity: defaultParseSuccessCacheSize,
		SuccessTTL:      defaultParseSuccessCacheTTL,
		SyntaxCapacity:  defaultParseSyntaxCacheSize,
		SyntaxTTL:       defaultParseSyntaxCacheTTL,
	}
}

// EffectiveConfig safely normalizes values that cannot represent a bounded
// cache while preserving zero as the explicit disabled setting.
func (c CacheConfig) EffectiveConfig() CacheConfig {
	defaults := DefaultCacheConfig()
	if c.SuccessCapacity < 0 {
		c.SuccessCapacity = defaults.SuccessCapacity
	} else if c.SuccessCapacity > maxParseCacheCapacity {
		c.SuccessCapacity = maxParseCacheCapacity
	}
	if c.SyntaxCapacity < 0 {
		c.SyntaxCapacity = defaults.SyntaxCapacity
	} else if c.SyntaxCapacity > maxParseCacheCapacity {
		c.SyntaxCapacity = maxParseCacheCapacity
	}
	if c.SuccessTTL < 0 {
		c.SuccessTTL = defaults.SuccessTTL
	} else if c.SuccessTTL > maxParseCacheTTL {
		c.SuccessTTL = maxParseCacheTTL
	}
	if c.SyntaxTTL < 0 {
		c.SyntaxTTL = defaults.SyntaxTTL
	} else if c.SyntaxTTL > maxParseCacheTTL {
		c.SyntaxTTL = maxParseCacheTTL
	}
	return c
}

// CacheConfigFromEnv returns the cache policy after applying validated
// PARSER_CACHE_* environment overrides. Malformed, negative, and out-of-range
// values safely retain their defaults; zero explicitly disables a cache.
func CacheConfigFromEnv() CacheConfig {
	cfg := DefaultCacheConfig()
	cfg.SuccessCapacity = cacheCapacityFromEnv("PARSER_CACHE_SUCCESS_CAPACITY", cfg.SuccessCapacity)
	cfg.SuccessTTL = cacheTTLFromEnv("PARSER_CACHE_SUCCESS_TTL_SECONDS", cfg.SuccessTTL)
	cfg.SyntaxCapacity = cacheCapacityFromEnv("PARSER_CACHE_SYNTAX_CAPACITY", cfg.SyntaxCapacity)
	cfg.SyntaxTTL = cacheTTLFromEnv("PARSER_CACHE_SYNTAX_TTL_SECONDS", cfg.SyntaxTTL)
	return cfg
}

func cacheCapacityFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.Atoi(raw)
	if raw == "" || err != nil || value < 0 || value > maxParseCacheCapacity {
		return fallback
	}
	return value
}

func cacheTTLFromEnv(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	value, err := strconv.ParseInt(raw, 10, 64)
	if raw == "" || err != nil || value < 0 || value > int64(maxParseCacheTTL/time.Second) {
		return fallback
	}
	return time.Duration(value) * time.Second
}
