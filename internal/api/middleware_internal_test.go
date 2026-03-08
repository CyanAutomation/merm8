package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testLogger is a minimal logger implementation for testing.
type testLogger struct{}

func (t *testLogger) Info(msg string, fields ...any)  {}
func (t *testLogger) Warn(msg string, fields ...any)  {}
func (t *testLogger) Error(msg string, fields ...any) {}

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

func TestClientIdentifier_UsesLeftmostIPForTrustedProxyMultiHop(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
	req.RemoteAddr = "10.1.2.3:443"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.2, 10.1.2.3")

	if got := clientIdentifier(req); got != "198.51.100.1" {
		t.Fatalf("expected original client IP from XFF chain, got %q", got)
	}
}

func TestClientIdentifier_SkipsMalformedXFFEntriesForTrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.0.0.0/8")

	t.Run("uses first token when valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
		req.RemoteAddr = "10.1.2.3:443"
		req.Header.Set("X-Forwarded-For", "198.51.100.30, not-an-ip")

		if got := clientIdentifier(req); got != "198.51.100.30" {
			t.Fatalf("expected first valid XFF token, got %q", got)
		}
	})

	t.Run("uses second token when first malformed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/analyze", nil)
		req.RemoteAddr = "10.1.2.3:443"
		req.Header.Set("X-Forwarded-For", "bad-token, 198.51.100.31")

		if got := clientIdentifier(req); got != "198.51.100.31" {
			t.Fatalf("expected next valid XFF token, got %q", got)
		}
	})
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
