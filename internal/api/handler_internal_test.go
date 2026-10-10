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

// @spec: FLOW-CONTROL-001: Runtime limit changes preserve in-flight accounting.
func TestTryAcquireParserSlot_ReleaseCallbackIsIdempotent(t *testing.T) {
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
	releaseA()
	releaseA()

	if _, ok := h.tryAcquireParserSlot(); ok {
		t.Fatal("expected the remaining in-flight request to keep the limit saturated")
	}

	releaseB()
	releaseC, ok := h.tryAcquireParserSlot()
	if !ok {
		t.Fatal("expected acquire to succeed after all in-flight requests are released")
	}
	releaseC()
}
