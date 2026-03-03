package api

import (
	"net/http"
	"time"

	"github.com/CyanAutomation/merm8/internal/telemetry"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// MetricsMiddleware observes request status/duration for mapped routes.
func MetricsMiddleware(next http.Handler, routePatterns map[string]string, metrics *telemetry.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeKey := r.Method + " " + r.URL.Path
		route := routePatterns[routeKey]
		if route == "" {
			route = "unknown"
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		metrics.ObserveHTTPRequest(route, r.Method, recorder.status, time.Since(start))
	})
}
