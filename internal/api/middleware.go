package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

const trustedProxyCIDRsEnv = "ANALYZE_TRUSTED_PROXY_CIDRS"

const requestIDHeader = "X-Request-Id"
const apiVersionHeader = "Accept-Version"
const contentVersionHeader = "Content-Version"
const defaultAnalyzeCompressionThresholdBytes = 1024

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
	requestID     string
	parserOutcome string
	diagramType   string
}

type corsRejectLogger struct {
	logger   Logger
	interval time.Duration
	mu       sync.Mutex
	lastLog  time.Time
}

type bufferedResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	status     int
	underlying http.ResponseWriter
	committed  bool
}

func (w *bufferedResponseWriter) Flush() {
	if !w.committed && (w.status != 0 || w.header != nil || w.body.Len() > 0) {
		w.commitBuffered()
	}
	if f, ok := w.underlying.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *bufferedResponseWriter) Header() http.Header {
	if w.committed {
		return w.underlying.Header()
	}
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if w.committed {
		return w.underlying.Write(p)
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *bufferedResponseWriter) commitBuffered() {
	if w.committed {
		return
	}

	if w.status == 0 {
		w.status = http.StatusOK
	}
	for k, values := range w.header {
		for _, value := range values {
			w.underlying.Header().Add(k, value)
		}
	}

	w.underlying.WriteHeader(w.status)
	if w.body.Len() > 0 {
		_, _ = w.underlying.Write(w.body.Bytes())
		w.body.Reset()
	}
	w.committed = true
}

// AnalyzeResponseCompressionMiddleware applies gzip encoding to eligible analyze JSON/SARIF responses.
func AnalyzeResponseCompressionMiddleware(next http.Handler, thresholdBytes int) http.Handler {
	if thresholdBytes <= 0 {
		thresholdBytes = defaultAnalyzeCompressionThresholdBytes
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAnalyzePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		buffered := &bufferedResponseWriter{underlying: w}
		next.ServeHTTP(buffered, r)
		if buffered.committed {
			return
		}

		status := buffered.status
		if status == 0 {
			status = http.StatusOK
		}

		for k, values := range buffered.Header() {
			for _, value := range values {
				w.Header().Add(k, value)
			}
		}
		appendVaryHeader(w.Header(), "Accept-Encoding")
		existingContentEncoding := strings.TrimSpace(w.Header().Get("Content-Encoding"))

		shouldCompress := acceptsGzipEncoding(r.Header.Get("Accept-Encoding")) &&
			existingContentEncoding == "" &&
			isCompressibleResponseType(w.Header().Get("Content-Type")) &&
			buffered.body.Len() >= thresholdBytes

		w.Header().Del("Content-Length")
		if shouldCompress {
			w.Header().Set("Content-Encoding", "gzip")
		}

		w.WriteHeader(status)
		if !shouldCompress {
			_, _ = w.Write(buffered.body.Bytes())
			return
		}

		gzipWriter := gzip.NewWriter(w)
		_, _ = gzipWriter.Write(buffered.body.Bytes())
		_ = gzipWriter.Close()
	})
}

func newCORSRejectLogger(logger Logger) *corsRejectLogger {
	return &corsRejectLogger{logger: normalizeLogger(logger), interval: time.Minute}
}

func (l *corsRejectLogger) Log(origin, path string, allowlistSize int) {
	if l == nil {
		return
	}
	now := time.Now()
	l.mu.Lock()
	if !l.lastLog.IsZero() && now.Sub(l.lastLog) < l.interval {
		l.mu.Unlock()
		return
	}
	l.lastLog = now
	l.mu.Unlock()

	l.logger.Warn("cors origin rejected",
		"origin", origin,
		"path", path,
		"allowlist_size", allowlistSize,
	)
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
		setAnalyzeLogFields(ctx, requestID, "", "")
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

		requestID := fields.requestID
		if requestID == "" {
			requestID = RequestIDFromContext(r.Context())
		}
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

func setAnalyzeLogFields(ctx context.Context, requestID string, parserOutcome string, diagramType string) {
	fields, _ := ctx.Value(analyzeLogFieldsContextKey).(*analyzeLogFields)
	if fields == nil {
		return
	}
	if requestID != "" {
		fields.requestID = requestID
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

// Check evaluates a request and returns the decision metadata in one locked operation.
// remaining reflects requests left after this decision.
func (rl *RateLimiter) Check(clientID string) (allowed bool, remaining int, resetUnix int64, limit int) {
	if rl == nil {
		return true, 0, 0, 0
	}

	limit = rl.limit
	if rl.limit <= 0 || rl.window <= 0 {
		return true, rl.limit, rl.now().Unix(), rl.limit
	}

	now := rl.now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry := rl.clients[clientID]
	if entry == nil && len(rl.clients) >= rl.maxClients {
		rl.deleteExpiredEntries(now, rl.cleanupBatchSize)
		if len(rl.clients) >= rl.maxClients {
			return false, 0, now.Add(rl.window).Unix(), limit
		}
	}

	if entry == nil {
		entry = &clientWindow{windowStart: now, count: 0}
		rl.clients[clientID] = entry
	} else if now.Sub(entry.windowStart) >= rl.window {
		entry.windowStart = now
		entry.count = 0
	}

	if entry.count < rl.limit {
		entry.count++
		allowed = true
	}

	remaining = rl.limit - entry.count
	if remaining < 0 {
		remaining = 0
	}
	resetUnix = entry.windowStart.Add(rl.window).Unix()

	return allowed, remaining, resetUnix, limit
}

// Remaining returns the number of requests remaining for the client in current window.
func (rl *RateLimiter) Remaining(clientID string) int {
	// A nil limiter is treated as an explicitly safe default with no remaining quota.
	if rl == nil {
		return 0
	}

	if rl.limit <= 0 || rl.window <= 0 {
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
	allowed, _, _, _ := rl.Check(clientID)
	return allowed
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
			allowed, remaining, reset, limit := limiter.Check(clientID)

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", reset))

			if !allowed {
				writeError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
				return
			}
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

// CORSMiddleware handles CORS (Cross-Origin Resource Sharing) headers.
// allowedOrigins is a comma-separated list of allowed origins (e.g., "https://example.com,https://app.example.com").
// If an origin matches one in the allowed list, Access-Control-Allow-Origin is set to that origin.
// Otherwise, no CORS headers are sent (treating the request as disallowed by CORS).
// Preflight OPTIONS requests are handled with an empty response (204 No Content).
func CORSMiddleware(allowedOrigins string, logger Logger, metrics *telemetry.Metrics) func(http.Handler) http.Handler {
	matcher := newCORSOriginMatcher(allowedOrigins)

	rejectLogger := newCORSRejectLogger(logger)
	allowlistSize := matcher.allowlistSize()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				appendVaryHeader(w.Header(), "Origin")
				if r.Method == http.MethodOptions {
					appendVaryHeader(w.Header(), "Access-Control-Request-Method")
					appendVaryHeader(w.Header(), "Access-Control-Request-Headers")
				}
			}

			// Check if origin is allowed
			if origin != "" && matcher.isAllowed(origin) {
				// Set CORS headers for allowed origins
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id")
				w.Header().Set("Access-Control-Expose-Headers", "X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Content-Version, X-Request-Id, Retry-After")
				w.Header().Set("Access-Control-Max-Age", "300") // 5 minutes

				// Handle preflight OPTIONS requests
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else if origin != "" {
				rejectLogger.Log(origin, r.URL.Path, allowlistSize)
				if metrics != nil {
					metrics.ObserveCORSRejectedOrigin()
				}
			}

			// For actual requests, continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

type corsOriginMatcher struct {
	exact     map[string]bool
	wildcards []wildcardOriginPattern
}

type wildcardOriginPattern struct {
	prefix string
	suffix string
}

func newCORSOriginMatcher(allowedOrigins string) corsOriginMatcher {
	matcher := corsOriginMatcher{exact: make(map[string]bool)}

	if strings.TrimSpace(allowedOrigins) == "" {
		return matcher
	}

	for _, origin := range strings.Split(allowedOrigins, ",") {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}

		if pattern, ok := parseWildcardOrigin(origin); ok {
			matcher.wildcards = append(matcher.wildcards, pattern)
			continue
		}

		matcher.exact[origin] = true
	}

	return matcher
}

func parseWildcardOrigin(origin string) (wildcardOriginPattern, bool) {
	if strings.Count(origin, "*") != 1 {
		return wildcardOriginPattern{}, false
	}

	parts := strings.Split(origin, "*")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return wildcardOriginPattern{}, false
	}

	return wildcardOriginPattern{prefix: parts[0], suffix: parts[1]}, true
}

func (m corsOriginMatcher) isAllowed(origin string) bool {
	if m.exact[origin] {
		return true
	}

	for _, pattern := range m.wildcards {
		if !strings.HasPrefix(origin, pattern.prefix) || !strings.HasSuffix(origin, pattern.suffix) {
			continue
		}

		middle := origin[len(pattern.prefix) : len(origin)-len(pattern.suffix)]
		if middle != "" {
			return true
		}
	}

	return false
}

func (m corsOriginMatcher) allowlistSize() int {
	return len(m.exact) + len(m.wildcards)
}

func isAnalyzePath(path string) bool {
	return isAnalyzeJSONPath(path) || path == "/analyze/sarif" || path == "/v1/analyze/sarif"
}

func acceptsGzipEncoding(acceptEncoding string) bool {
	for _, token := range strings.Split(acceptEncoding, ",") {
		encoding := strings.ToLower(strings.TrimSpace(token))
		if encoding == "" {
			continue
		}
		name := encoding
		quality := ""
		if idx := strings.Index(encoding, ";"); idx >= 0 {
			name = strings.TrimSpace(encoding[:idx])
			quality = strings.TrimSpace(encoding[idx+1:])
		}
		if name != "gzip" && name != "*" {
			continue
		}
		if strings.EqualFold(quality, "q=0") {
			continue
		}
		return true
	}

	return false
}

func isCompressibleResponseType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.ToLower(contentType))
	}
	mediaType = strings.ToLower(mediaType)

	return mediaType == "application/json" || mediaType == "application/sarif+json"
}

func appendVaryHeader(headers http.Header, value string) {
	if headers == nil || value == "" {
		return
	}

	existing := headers.Values("Vary")
	for _, entry := range existing {
		for _, token := range strings.Split(entry, ",") {
			if strings.EqualFold(strings.TrimSpace(token), value) {
				return
			}
		}
	}
	headers.Add("Vary", value)
}

func isAnalyzeJSONPath(path string) bool {
	return path == "/analyze" || path == "/v1/analyze" || path == "/v1/analyse"
}

func isProtectedAnalyzePath(path string) bool {
	return isAnalyzeJSONPath(path) ||
		path == "/analyze/raw" || path == "/v1/analyze/raw" || path == "/v1/analyse/raw" ||
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
