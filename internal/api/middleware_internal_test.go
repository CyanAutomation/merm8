package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientIdentifier_IgnoresForwardedForWithoutTrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "")

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.77")

	got := clientIdentifier(req)
	if got != "203.0.113.10" {
		t.Fatalf("expected remote addr when proxy is untrusted, got %q", got)
	}
}

func TestClientIdentifier_UsesForwardedForFromTrustedProxy(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "203.0.113.0/24")

	req := httptest.NewRequest(http.MethodPost, "/analyze", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.12, 198.51.100.99")

	got := clientIdentifier(req)
	if got != "198.51.100.99" {
		t.Fatalf("expected rightmost forwarded client ip from trusted proxy, got %q", got)
	}
}

func TestRateLimiterDeleteExpiredEntries_BoundedByBatchSize(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rl := &RateLimiter{
		window:           time.Minute,
		limit:            10,
		clients:          map[string]*clientWindow{},
		cleanupBatchSize: 2,
		maxClients:       0,
		now:              func() time.Time { return now },
	}

	rl.clients["active"] = &clientWindow{windowStart: now, count: 1}
	rl.clients["expired-1"] = &clientWindow{windowStart: now.Add(-3 * time.Minute), count: 1}
	rl.clients["expired-2"] = &clientWindow{windowStart: now.Add(-4 * time.Minute), count: 1}
	rl.clients["expired-3"] = &clientWindow{windowStart: now.Add(-5 * time.Minute), count: 1}

	rl.Allow("new-client")

	if _, ok := rl.clients["active"]; !ok {
		t.Fatalf("expected active client to remain")
	}
	if _, ok := rl.clients["new-client"]; !ok {
		t.Fatalf("expected new client to be added")
	}

	removed := 0
	for _, id := range []string{"expired-1", "expired-2", "expired-3"} {
		if _, ok := rl.clients[id]; !ok {
			removed++
		}
	}
	if removed > rl.cleanupBatchSize {
		t.Fatalf("expected at most %d removals, got %d", rl.cleanupBatchSize, removed)
	}
}
