package api

import "testing"

func TestTryAcquireParserSlot_RuntimeLimitTransition_UnlimitedToLimited(t *testing.T) {
	h := NewHandler(noopParser{}, nil)

	releaseA, ok := h.tryAcquireParserSlot()
	if !ok {
		t.Fatal("expected first unlimited acquire to succeed")
	}
	releaseB, ok := h.tryAcquireParserSlot()
	if !ok {
		t.Fatal("expected second unlimited acquire to succeed")
	}

	h.SetParserConcurrencyLimit(1)

	if _, ok := h.tryAcquireParserSlot(); ok {
		t.Fatal("expected acquire to be rejected after lowering limit below current in-flight")
	}

	releaseA()
	if _, ok := h.tryAcquireParserSlot(); ok {
		t.Fatal("expected acquire to still be rejected while in-flight equals limit")
	}

	releaseB()
	if _, ok := h.tryAcquireParserSlot(); !ok {
		t.Fatal("expected acquire to succeed after in-flight drops below lowered limit")
	}
}

func TestTryAcquireParserSlot_RuntimeLimitTransition_LimitedToUnlimited(t *testing.T) {
	h := NewHandler(noopParser{}, nil)
	h.SetParserConcurrencyLimit(1)

	release, ok := h.tryAcquireParserSlot()
	if !ok {
		t.Fatal("expected initial limited acquire to succeed")
	}
	if _, ok := h.tryAcquireParserSlot(); ok {
		t.Fatal("expected second acquire to be rejected at limit")
	}

	h.SetParserConcurrencyLimit(0)
	if _, ok := h.tryAcquireParserSlot(); !ok {
		t.Fatal("expected acquire to succeed after switching to unlimited")
	}

	release()
}

func TestParserConcurrencyLimiter_Release_DoesNotGoNegative(t *testing.T) {
	limiter := newParserConcurrencyLimiter()

	limiter.Release()
	if limiter.inFlight != 0 {
		t.Fatalf("expected inFlight to remain at zero after release without acquire, got %d", limiter.inFlight)
	}

	if !limiter.TryAcquire() {
		t.Fatal("expected unlimited acquire to succeed")
	}
	limiter.Release()
	limiter.Release()
	if limiter.inFlight != 0 {
		t.Fatalf("expected inFlight to stay non-negative after extra release, got %d", limiter.inFlight)
	}
}
