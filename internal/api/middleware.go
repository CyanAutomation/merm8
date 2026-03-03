package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter applies a simple per-client fixed-window request limit.
type RateLimiter struct {
	mu         sync.Mutex
	window     time.Duration
	limit      int
	clients    map[string]*clientWindow
	now        func() time.Time
	maxClients int
}

type clientWindow struct {
	windowStart time.Time
	count       int
}

// NewRateLimiter returns a fixed-window per-client rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:     window,
		limit:      limit,
		clients:    make(map[string]*clientWindow),
		now:        time.Now,
		maxClients: 10000,
	}
}

func (rl *RateLimiter) Allow(clientID string) bool {
	if rl == nil || rl.limit <= 0 || rl.window <= 0 {
		return true
	}

	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if len(rl.clients) > rl.maxClients {
		for id, entry := range rl.clients {
			if now.Sub(entry.windowStart) > rl.window*2 {
				delete(rl.clients, id)
			}
		}
	}

	entry := rl.clients[clientID]
	if entry == nil {
		rl.clients[clientID] = &clientWindow{windowStart: now, count: 1}
		return true
	}

	if now.Sub(entry.windowStart) >= rl.window {
		entry.windowStart = now
		entry.count = 1
		return true
	}

	if entry.count >= rl.limit {
		return false
	}

	entry.count++
	return true
}

// AnalyzeRateLimitMiddleware protects POST /analyze with the provided limiter.
func AnalyzeRateLimitMiddleware(limiter *RateLimiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/analyze" {
			clientID := clientIdentifier(r)
			if !limiter.Allow(clientID) {
				writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// AnalyzeBearerAuthMiddleware requires Bearer auth on POST /analyze.
func AnalyzeBearerAuthMiddleware(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/analyze" {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")) != token {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIdentifier(r *http.Request) string {
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}

	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}

	return "unknown"
}
