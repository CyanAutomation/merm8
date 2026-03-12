package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

// testLogger is a minimal logger implementation for testing.
type testLogger struct{}

func (t *testLogger) Info(msg string, fields ...any)  {}
func (t *testLogger) Warn(msg string, fields ...any)  {}
func (t *testLogger) Error(msg string, fields ...any) {}

func TestAnalyzeRateLimitMiddleware_NilLimiterPassesThrough(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusAccepted)
	})

	handler := AnalyzeRateLimitMiddleware(nil, next)
	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("expected downstream handler to be called when limiter is nil")
	}
	if got := rec.Code; got != http.StatusAccepted {
		t.Fatalf("expected downstream status to pass through, got %d", got)
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "" {
		t.Fatalf("expected no rate-limit headers when limiter is nil, got limit=%q", got)
	}
}

func TestRateLimiterCheck_AtCapacityRejectsUnknownClient(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	limiter := NewRateLimiter(2, time.Hour)
	limiter.maxClients = 3
	limiter.cleanupBatchSize = 64
	limiter.now = func() time.Time { return base }

	for i := 0; i < limiter.maxClients; i++ {
		clientID := fmt.Sprintf("known-%d", i)
		allowed, remaining, reset, limit := limiter.Check(clientID)
		if !allowed {
			t.Fatalf("expected initial known client %q to be allowed", clientID)
		}
		if remaining != limiter.limit-1 {
			t.Fatalf("expected remaining=%d for first request, got %d", limiter.limit-1, remaining)
		}
		if limit != limiter.limit {
			t.Fatalf("expected reported limit %d, got %d", limiter.limit, limit)
		}
		if reset != base.Add(limiter.window).Unix() {
			t.Fatalf("expected reset %d, got %d", base.Add(limiter.window).Unix(), reset)
		}
	}

	allowed, remaining, _, _ := limiter.Check("known-1")
	if !allowed {
		t.Fatal("expected existing client to continue being allowed at capacity")
	}
	if remaining != 0 {
		t.Fatalf("expected remaining=0 after second allowed request, got %d", remaining)
	}

	allowed, remaining, reset, limit := limiter.Check("unknown-client")
	if allowed {
		t.Fatal("expected unknown client to be rejected at capacity")
	}
	if remaining != 0 {
		t.Fatalf("expected remaining=0 for rejected client, got %d", remaining)
	}
	if limit != limiter.limit {
		t.Fatalf("expected reported limit %d, got %d", limiter.limit, limit)
	}
	if reset != base.Add(limiter.window).Unix() {
		t.Fatalf("expected reset %d for rejection, got %d", base.Add(limiter.window).Unix(), reset)
	}
	if got := len(limiter.clients); got != limiter.maxClients {
		t.Fatalf("expected clients map to remain bounded at %d, got %d", limiter.maxClients, got)
	}
}

func TestRateLimiterCheck_CleanupThenAdmitDeterministically(t *testing.T) {
	base := time.Unix(1_700_000_100, 0)
	window := time.Hour
	limiter := NewRateLimiter(1, window)
	limiter.maxClients = 3
	limiter.cleanupBatchSize = 4
	limiter.now = func() time.Time { return base }

	limiter.clients["expired-1"] = &clientWindow{windowStart: base.Add(-3 * window), count: 1}
	limiter.clients["expired-2"] = &clientWindow{windowStart: base.Add(-4 * window), count: 1}
	limiter.clients["active"] = &clientWindow{windowStart: base, count: 0}

	allowed, remaining, _, _ := limiter.Check("new-client")
	if !allowed {
		t.Fatal("expected new client to be admitted after expired cleanup")
	}
	if remaining != 0 {
		t.Fatalf("expected remaining=0 for limit=1 first request, got %d", remaining)
	}
	if got := len(limiter.clients); got != 2 {
		t.Fatalf("expected expired entries removed and bounded size, got %d", got)
	}
	if _, ok := limiter.clients["new-client"]; !ok {
		t.Fatal("expected new client entry to exist after admission")
	}
}

func TestRequestIDMiddleware_PropagatesOrGeneratesRequestID(t *testing.T) {
	tests := []struct {
		name       string
		incomingID string
		wantID     string
	}{
		{name: "propagates incoming request id", incomingID: "incoming-id", wantID: "incoming-id"},
		{name: "generates request id when missing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := RequestIDFromContext(r.Context())
				if tc.wantID != "" && got != tc.wantID {
					t.Fatalf("expected request id in context %q, got %q", tc.wantID, got)
				}
				if tc.wantID == "" && got == "" {
					t.Fatal("expected generated request id in context")
				}
				w.WriteHeader(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if tc.incomingID != "" {
				req.Header.Set(requestIDHeader, tc.incomingID)
			}
			rec := httptest.NewRecorder()

			RequestIDMiddleware(next).ServeHTTP(rec, req)

			got := rec.Header().Get(requestIDHeader)
			if tc.wantID != "" && got != tc.wantID {
				t.Fatalf("expected response request id %q, got %q", tc.wantID, got)
			}
			if tc.wantID == "" && got == "" {
				t.Fatal("expected generated request id in response header")
			}
		})
	}
}

func TestClientIdentifier_UsesXFFForTrustedProxySingleHop(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")

	if got := clientIdentifier(req); got != "203.0.113.10" {
		t.Fatalf("expected leftmost client IP from XFF, got %q", got)
	}
}

func TestClientIdentifier_UsesFirstPublicUntrustedIPForTrustedProxyMultiHop(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8,192.168.0.0/16")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.99, 192.168.1.9, 198.51.100.20, 10.1.2.3")

	if got := clientIdentifier(req); got != "198.51.100.20" {
		t.Fatalf("expected first public untrusted IP from right, got %q", got)
	}
}

func TestClientIdentifier_IgnoresSpoofedLeftmostEntriesForTrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.200, 198.51.100.31, 10.1.2.3")

	if got := clientIdentifier(req); got != "198.51.100.31" {
		t.Fatalf("expected spoofed leftmost entry ignored, got %q", got)
	}
}

func TestClientIdentifier_SkipsMalformedXFFEntriesForTrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	t.Run("uses first valid public token from right", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
		req.RemoteAddr = "10.1.2.3:443"
		req.Header.Set("X-Forwarded-For", "198.51.100.30, not-an-ip, 10.1.2.3")

		if got := clientIdentifier(req); got != "198.51.100.30" {
			t.Fatalf("expected first valid untrusted token, got %q", got)
		}
	})

	t.Run("uses next valid token when right-most untrusted malformed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
		req.RemoteAddr = "10.1.2.3:443"
		req.Header.Set("X-Forwarded-For", "198.51.100.31, bad-token, 10.1.2.3")

		if got := clientIdentifier(req); got != "198.51.100.31" {
			t.Fatalf("expected next valid XFF token, got %q", got)
		}
	})
}

func TestClientIdentifier_FallsBackToRemoteAddrWhenForwardedChainHasNoUntrustedHop(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "10.3.4.5, 10.1.2.3")

	if got := clientIdentifier(req); got != "10.1.2.3" {
		t.Fatalf("expected remote address fallback when forwarded chain has no untrusted hop, got %q", got)
	}
}

func TestClientIdentifier_IgnoresXFFForUntrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "192.0.2.50:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	if got := clientIdentifier(req); got != "192.0.2.50" {
		t.Fatalf("expected remote address for untrusted proxy, got %q", got)
	}
}

func TestAnalyzeResponseCompressionMiddleware_FlushCommitsBufferedBodyOnce(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"part":"one"}`))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write([]byte(`{"part":"two"}`))
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	rec := httptest.NewRecorder()

	AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

	if got := rec.Body.String(); got != `{"part":"one"}{"part":"two"}` {
		t.Fatalf("body mismatch: got %q", got)
	}
	if rec.Flushed != true {
		t.Fatal("expected downstream flush to reach underlying writer")
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no compression for early flush path, got %q", got)
	}
}

func TestAnalyzeResponseCompressionMiddleware_NoFlushUsesFinalCompressionPath(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64)))
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content-encoding = %q, want gzip", got)
	}
}

func TestAnalyzeResponseCompressionMiddleware_CompressesLargeJSONWhenAccepted(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64)))
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()

	AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content-encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(strings.ToLower(got), "accept-encoding") {
		t.Fatalf("vary = %q, want to include Accept-Encoding", got)
	}

	gz, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader error: %v", err)
	}
	defer gz.Close()

	decoded, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("io.ReadAll error: %v", err)
	}
	if got := string(decoded); got != strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64) {
		t.Fatalf("decoded body mismatch: got len=%d", len(decoded))
	}
}

func TestAnalyzeResponseCompressionMiddleware_SkipsCompressionWhenHeaderMissingOrBodySmall(t *testing.T) {
	t.Run("missing accept-encoding", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64)))
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
		rec := httptest.NewRecorder()

		AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("unexpected content-encoding %q", got)
		}
		if got := rec.Body.String(); got != strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64) {
			t.Fatalf("body mismatch for uncompressed response")
		}
	})

	t.Run("body under threshold", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/sarif+json")
			_, _ = w.Write([]byte(`{"small":true}`))
		})

		req := httptest.NewRequest(http.MethodPost, "/analyze/sarif", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()

		AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("unexpected content-encoding %q", got)
		}
		if got := rec.Header().Get("Vary"); !strings.Contains(strings.ToLower(got), "accept-encoding") {
			t.Fatalf("vary = %q, want to include Accept-Encoding", got)
		}
	})
}

func TestAnalyzeResponseCompressionMiddleware_SkipsCompressionWhenAlreadyEncoded(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64)))
	})

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content-encoding = %q, want gzip", got)
	}
	if got := rec.Body.String(); got != strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64) {
		t.Fatalf("body mismatch for pre-encoded response")
	}
}

func TestAnalyzeResponseCompressionMiddleware_IgnoresNonAnalyzePaths(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", defaultAnalyzeCompressionThresholdBytes+64)))
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	AnalyzeResponseCompressionMiddleware(next, defaultAnalyzeCompressionThresholdBytes).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("unexpected content-encoding %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "" {
		t.Fatalf("unexpected vary header %q", got)
	}
}

func TestCORSMiddleware_AllowsMatchingOrigin(t *testing.T) {
	allowedOrigins := "https://example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header with allowed origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods header")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Access-Control-Allow-Headers header")
	}
	if !headerContainsToken(rec.Header(), "Vary", "Origin") {
		t.Fatalf("expected Vary header to include Origin, got %q", rec.Header().Values("Vary"))
	}
	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("expected status 200, got %d", got)
	}
}

func TestCORSMiddleware_RejectsNonMatchingOrigin(t *testing.T) {
	allowedOrigins := "https://example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://other.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header for rejected origin, got %q", got)
	}
	if !headerContainsToken(rec.Header(), "Vary", "Origin") {
		t.Fatalf("expected Vary header to include Origin for rejected origin path, got %q", rec.Header().Values("Vary"))
	}
}

func TestCORSMiddleware_SupportsMultipleAllowedOrigins(t *testing.T) {
	allowedOrigins := "https://example.com, https://app.example.com, https://test.example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	testCases := []struct {
		origin      string
		shouldAllow bool
	}{
		{"https://example.com", true},
		{"https://app.example.com", true},
		{"https://test.example.com", true},
		{"https://other.com", false},
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Origin", tc.origin)
			rec := httptest.NewRecorder()

			middleware(next).ServeHTTP(rec, req)

			got := rec.Header().Get("Access-Control-Allow-Origin")
			if tc.shouldAllow && got != tc.origin {
				t.Fatalf("expected CORS header with origin %q, got %q", tc.origin, got)
			}
			if !tc.shouldAllow && got != "" {
				t.Fatalf("expected no CORS header, got %q", got)
			}
		})
	}
}

func TestCORSMiddleware_HandlesPreflight(t *testing.T) {
	allowedOrigins := "https://example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header in preflight, got %q", got)
	}
	if got := rec.Code; got != http.StatusNoContent {
		t.Fatalf("expected status 204 for preflight, got %d", got)
	}
	// Verify body is empty for preflight
	if got := rec.Body.String(); got != "" {
		t.Fatalf("expected empty body for preflight, got %q", got)
	}
	for _, token := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !headerContainsToken(rec.Header(), "Vary", token) {
			t.Fatalf("expected Vary header to include %q, got %q", token, rec.Header().Values("Vary"))
		}
	}
}

func headerContainsToken(headers http.Header, headerName, token string) bool {
	for _, entry := range headers.Values(headerName) {
		for _, part := range strings.Split(entry, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}

	return false
}

func TestCORSMiddleware_AllowsConfiguredWildcardPattern(t *testing.T) {
	allowedOrigins := "https://merm8-splash-*.vercel.app"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://merm8-splash-preview-123.vercel.app")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://merm8-splash-preview-123.vercel.app" {
		t.Fatalf("expected CORS header with wildcard-matched origin, got %q", got)
	}
}

func TestCORSMiddleware_RejectsWildcardNearMissDomains(t *testing.T) {
	allowedOrigins := "https://merm8-splash-*.vercel.app"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	testCases := []string{
		"https://merm8-splash-preview-123.vercel.app.evil.com",
		"https://merm8-splash-.vercel.app",
		"https://other-merm8-splash-preview.vercel.app",
	}

	for _, origin := range testCases {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()

			middleware(next).ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("expected no CORS header for near-miss domain, got %q", got)
			}
		})
	}
}

func TestCORSMiddleware_ExposesHeaders(t *testing.T) {
	allowedOrigins := "https://example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	exposeHeaders := rec.Header().Get("Access-Control-Expose-Headers")
	if exposeHeaders == "" {
		t.Fatal("expected Access-Control-Expose-Headers header")
	}
	// Verify that rate limit and content version headers are exposed
	if !strings.Contains(exposeHeaders, "X-RateLimit") {
		t.Fatalf("expected X-RateLimit headers to be exposed, got %q", exposeHeaders)
	}
	if !strings.Contains(exposeHeaders, "Content-Version") {
		t.Fatalf("expected Content-Version header to be exposed, got %q", exposeHeaders)
	}
}

func TestCORSMiddleware_EmptyAllowedOrigins(t *testing.T) {
	allowedOrigins := ""
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS header when no origins are configured, got %q", got)
	}
}

func TestCORSMiddleware_AllowsErrorResponsesWithCORS(t *testing.T) {
	allowedOrigins := "https://example.com"
	middleware := CORSMiddleware(allowedOrigins, &testLogger{}, nil)

	// Test that CORS headers are set even for error responses (e.g., 503)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service Unavailable"))
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("expected CORS header on 503 response, got %q", got)
	}
	if got := rec.Code; got != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", got)
	}
}

func TestCORSMiddleware_RejectedOriginIncrementsMetric(t *testing.T) {
	allowedOrigins := "https://example.com"
	metrics := telemetry.NewMetrics()
	middleware := CORSMiddleware(allowedOrigins, nil, metrics)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/analyze", nil)
	req.Header.Set("Origin", "https://other.com")
	rec := httptest.NewRecorder()

	middleware(next).ServeHTTP(rec, req)

	metricsRec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(metricsRec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := metricsRec.Body.String(); !strings.Contains(got, "cors_rejected_total 1") {
		t.Fatalf("expected cors_rejected_total to be incremented, got metrics: %s", got)
	}
}

func TestCORSMiddleware_RejectedOriginLoggingRateLimited(t *testing.T) {
	allowedOrigins := "https://example.com"
	buf := &bytes.Buffer{}
	logger := newJSONLogger(buf, "test")
	middleware := CORSMiddleware(allowedOrigins, logger, nil)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/analyze", nil)
		req.Header.Set("Origin", "https://other.com")
		rec := httptest.NewRecorder()
		middleware(next).ServeHTTP(rec, req)
	}

	logs := strings.TrimSpace(buf.String())
	if logs == "" {
		t.Fatal("expected at least one rejected CORS log line")
	}
	lines := strings.Split(logs, "\n")
	if len(lines) != 1 {
		t.Fatalf("expected exactly one log line due to rate limiting, got %d lines: %q", len(lines), logs)
	}
	line := lines[0]
	if !strings.Contains(line, "cors origin rejected") {
		t.Fatalf("expected rejected CORS log message, got %q", line)
	}
	if !strings.Contains(line, "\"origin\":\"https://other.com\"") {
		t.Fatalf("expected origin in log, got %q", line)
	}
	if !strings.Contains(line, "\"path\":\"/v1/analyze\"") {
		t.Fatalf("expected path in log, got %q", line)
	}
	if !strings.Contains(line, "\"allowlist_size\":1") {
		t.Fatalf("expected allowlist size in log, got %q", line)
	}
}

func TestAnalyzeLoggingMiddleware_LogsRequestIDRegardlessOfMiddlewareOrder(t *testing.T) {
	tests := []struct {
		name  string
		chain func(base http.Handler, logger Logger) http.Handler
	}{
		{
			name: "analyze logging outer, request id inner",
			chain: func(base http.Handler, logger Logger) http.Handler {
				return AnalyzeLoggingMiddleware(RequestIDMiddleware(base), logger)
			},
		},
		{
			name: "request id outer, analyze logging inner",
			chain: func(base http.Handler, logger Logger) http.Handler {
				return RequestIDMiddleware(AnalyzeLoggingMiddleware(base, logger))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			logger := newJSONLogger(buf, "test")
			base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				setAnalyzeLogFields(r.Context(), "", telemetry.OutcomeLintSuccess, "flowchart")
				w.WriteHeader(http.StatusOK)
			})
			handler := tc.chain(base, logger)

			req := httptest.NewRequest(http.MethodPost, "/v1/analyze", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			line := strings.TrimSpace(buf.String())
			if line == "" {
				t.Fatal("expected analyze completion log")
			}
			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("expected JSON log line, got error: %v; line=%q", err, line)
			}

			requestID, _ := entry["request_id"].(string)
			if strings.TrimSpace(requestID) == "" {
				t.Fatalf("expected request_id in analyze completion log, got %q", requestID)
			}
		})
	}
}
