package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
)

type asyncBlockingLogger struct {
	started chan struct{}
	release chan struct{}
}

func (l *asyncBlockingLogger) Info(string, ...any)  {}
func (l *asyncBlockingLogger) Error(string, ...any) {}
func (l *asyncBlockingLogger) Warn(string, ...any) {
	select {
	case <-l.started:
	default:
		close(l.started)
	}
	<-l.release
}

func TestCORSMiddlewareRejectedOriginDoesNotBlockOnLogger(t *testing.T) {
	logger := &asyncBlockingLogger{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	nextReached := make(chan struct{}, 1)
	h := api.CORSMiddleware("https://allowed.example", logger, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextReached <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/analyze", nil)
	req.Header.Set("Origin", "https://blocked.example")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-logger.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected logger to be invoked")
	}

	select {
	case <-nextReached:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("request handling blocked on logger")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("middleware did not return promptly")
	}

	close(logger.release)
}
