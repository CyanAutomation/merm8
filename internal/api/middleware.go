package api

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

const trustedProxyCIDRsEnv = "ANALYZE_TRUSTED_PROXY_CIDRS"

// RateLimiter applies a simple per-client fixed-window request limit.
type RateLimiter struct {
	mu               sync.Mutex
	window           time.Duration
	limit            int
	clients          map[string]*clientWindow
	now              func() time.Time
	maxClients       int
	cleanupBatchSize int
}

type clientWindow struct {
	windowStart time.Time
	count       int
}

// NewRateLimiter returns a fixed-window per-client rate limiter.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		window:           window,
		limit:            limit,
		clients:          make(map[string]*clientWindow),
		now:              time.Now,
		maxClients:       10000,
		cleanupBatchSize: 128,
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
		rl.deleteExpiredEntries(now, rl.cleanupBatchSize)
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

func (rl *RateLimiter) deleteExpiredEntries(now time.Time, maxDeletes int) {
	if maxDeletes <= 0 {
		maxDeletes = 1
	}

	deleted := 0
	for id, entry := range rl.clients {
		if now.Sub(entry.windowStart) > rl.window*2 {
			delete(rl.clients, id)
			deleted++
			if deleted >= maxDeletes {
				return
			}
		}
	}
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
			if !strings.HasPrefix(header, "Bearer ") {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
			providedToken := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if subtle.ConstantTimeCompare([]byte(providedToken), []byte(token)) != 1 {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIdentifier(r *http.Request) string {
	if remoteIP, ok := remoteIPFromAddr(r.RemoteAddr); ok {
		if isTrustedProxy(remoteIP) {
			if forwardedIP, ok := rightmostForwardedForIP(r.Header.Get("X-Forwarded-For")); ok {
				return forwardedIP.String()
			}
		}
		return remoteIP.String()
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

func rightmostForwardedForIP(header string) (netip.Addr, bool) {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		if candidate == "" {
			continue
		}
		if ip, err := netip.ParseAddr(candidate); err == nil {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func remoteIPFromAddr(addr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		host = strings.TrimSpace(addr)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip, true
}

func isTrustedProxy(remoteIP netip.Addr) bool {
	configured := strings.TrimSpace(os.Getenv(trustedProxyCIDRsEnv))
	if configured == "" {
		return false
	}

	for _, token := range strings.Split(configured, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		if strings.Contains(token, "/") {
			prefix, err := netip.ParsePrefix(token)
			if err == nil && prefix.Contains(remoteIP) {
				return true
			}
			continue
		}

		if ip, err := netip.ParseAddr(token); err == nil && ip == remoteIP {
			return true
		}
	}

	return false
}
