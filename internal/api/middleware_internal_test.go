package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDMiddleware_PropagatesIncomingHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got != "incoming-id" {
			t.Fatalf("expected request id in context, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(requestIDHeader, "incoming-id")
	rec := httptest.NewRecorder()

	RequestIDMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got != "incoming-id" {
		t.Fatalf("expected response request id %q, got %q", "incoming-id", got)
	}
}

func TestRequestIDMiddleware_GeneratesHeaderWhenMissing(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := RequestIDFromContext(r.Context()); got == "" {
			t.Fatal("expected generated request id in context")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	RequestIDMiddleware(next).ServeHTTP(rec, req)

	if got := rec.Header().Get(requestIDHeader); got == "" {
		t.Fatal("expected generated request id in response header")
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
