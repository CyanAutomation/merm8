package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

const trustedProxyCIDRsEnv = "ANALYZE_TRUSTED_PROXY_CIDRS"

const requestIDHeader = "X-Request-Id"
const apiVersionHeader = "Accept-Version"
const contentVersionHeader = "Content-Version"

// CurrentAPIVersion is the current version of the API
const CurrentAPIVersion = "1.0"

// SupportedAPIVersions lists all versions this server supports
var SupportedAPIVersions = []string{"1.0"}

type contextKey string

const (
	requestIDContextKey        contextKey = "request-id"
	analyzeLogFieldsContextKey contextKey = "analyze-log-fields"
	apiVersionContextKey       contextKey = "api-version"
)

type analyzeLogFields struct {
	parserOutcome string
	diagramType   string
}

// RequestIDMiddleware propagates or generates request IDs for correlation.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = generateUUID()
		}

		w.Header().Set(requestIDHeader, requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// VersionNegotiationMiddleware handles API version negotiation.
// Supports Accept-Version header for client-requested versions.
// Responds with Content-Version header indicating the API version being used.
func VersionNegotiationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedVersion := strings.TrimSpace(r.Header.Get(apiVersionHeader))

		// Default to current version if not specified
		version := CurrentAPIVersion
		if requestedVersion != "" {
			// Use requested version if it's supported, otherwise use current
			for _, v := range SupportedAPIVersions {
				if v == requestedVersion {
					version = requestedVersion
					break
				}
			}
		}

		// Set response header indicating which version is being used
		w.Header().Set(contentVersionHeader, version)

		// Add version to context for use in handlers
		ctx := context.WithValue(r.Context(), apiVersionContextKey, version)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIVersionFromContext returns the negotiated API version from the request context.
func APIVersionFromContext(ctx context.Context) string {
	if ctx == nil {
		return CurrentAPIVersion
	}
	version, _ := ctx.Value(apiVersionContextKey).(string)
	if version == "" {
		return CurrentAPIVersion
	}
	return version
}

// AnalyzeLoggingMiddleware emits per-request structured logs for analyze endpoints.
func AnalyzeLoggingMiddleware(next http.Handler, logger Logger) http.Handler {
	logger = normalizeLogger(logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAnalyzePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		fields := &analyzeLogFields{}
		ctx := context.WithValue(r.Context(), analyzeLogFieldsContextKey, fields)
		r = r.WithContext(ctx)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)

		requestID := RequestIDFromContext(ctx)
		parserOutcome := fields.parserOutcome
		if parserOutcome == "" {
			parserOutcome = "unknown"
		}
		diagramType := fields.diagramType
		if diagramType == "" {
			diagramType = "unknown"
		}

		logger.Info("analyze request completed",
			"request_id", requestID,
			"route", r.URL.Path,
			"method", r.Method,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"parser_outcome", parserOutcome,
			"diagram_type", diagramType,
		)
	})
}

// RequestIDFromContext returns the request correlation identifier if present.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDContextKey).(string)
	return requestID
}

func setAnalyzeLogFields(ctx context.Context, parserOutcome string, diagramType string) {
	fields, _ := ctx.Value(analyzeLogFieldsContextKey).(*analyzeLogFields)
	if fields == nil {
		return
	}
	if parserOutcome != "" {
		fields.parserOutcome = parserOutcome
	}
	if diagramType != "" {
		fields.diagramType = diagramType
	}
}

func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15],
	)
}

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

// Remaining returns the number of requests remaining for the client in current window.
func (rl *RateLimiter) Remaining(clientID string) int {
	if rl == nil || rl.limit <= 0 || rl.window <= 0 {
		return rl.limit
	}

	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry := rl.clients[clientID]
	if entry == nil {
		return rl.limit
	}

	if now.Sub(entry.windowStart) >= rl.window {
		return rl.limit
	}

	remaining := rl.limit - entry.count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// AllowWithMetrics checks if request is allowed and updates metrics without setting headers.
func (rl *RateLimiter) AllowWithMetrics(clientID string) bool {
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

// Allow checks if a request is allowed (deprecated: use AllowWithMetrics).
func (rl *RateLimiter) Allow(clientID string) bool {
	return rl.AllowWithMetrics(clientID)
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

// AnalyzeRateLimitMiddleware protects POST analyze endpoints with the provided limiter.
func AnalyzeRateLimitMiddleware(limiter *RateLimiter, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && isProtectedAnalyzePath(r.URL.Path) {
			clientID := clientIdentifier(r)

			// Calculate current window reset time
			now := time.Now()
			reset := now.Add(limiter.window).Unix()

			if !limiter.AllowWithMetrics(clientID) {
				// Rate limit exceeded
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limiter.limit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
				writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
				return
			}

			// Request allowed, set remaining headers
			remaining := limiter.Remaining(clientID)
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limiter.limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))
		}
		next.ServeHTTP(w, r)
	})
}

// AnalyzeBearerAuthMiddleware requires Bearer auth on POST analyze endpoints.
func AnalyzeBearerAuthMiddleware(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && isProtectedAnalyzePath(r.URL.Path) {
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

func isAnalyzePath(path string) bool {
	return isAnalyzeJSONPath(path) || path == "/analyze/sarif" || path == "/v1/analyze/sarif"
}

func isAnalyzeJSONPath(path string) bool {
	return path == "/analyze" || path == "/v1/analyze"
}

func isProtectedAnalyzePath(path string) bool {
	return isAnalyzeJSONPath(path) ||
		path == "/analyze/raw" || path == "/v1/analyze/raw" ||
		path == "/analyze/sarif" || path == "/v1/analyze/sarif"
}

func clientIdentifier(r *http.Request) string {
	if remoteIP, ok := remoteIPFromAddr(r.RemoteAddr); ok {
		if isTrustedProxy(remoteIP) {
			if forwardedIP, ok := leftmostForwardedForIP(r.Header.Get("X-Forwarded-For")); ok {
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

func leftmostForwardedForIP(header string) (netip.Addr, bool) {
	for _, part := range strings.Split(header, ",") {
		candidate := strings.TrimSpace(part)
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
