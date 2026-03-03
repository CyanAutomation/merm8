package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type routeMetricKey struct {
	route  string
	method string
	status int
}

type durationMetricKey struct {
	route  string
	method string
}

// PrometheusMetricsExporter stores HTTP metrics and exports them in Prometheus text format.
type PrometheusMetricsExporter struct {
	mu            sync.RWMutex
	requestCounts map[routeMetricKey]uint64
	durationSums  map[durationMetricKey]float64
	durationCount map[durationMetricKey]uint64
}

func NewPrometheusMetricsExporter() *PrometheusMetricsExporter {
	return &PrometheusMetricsExporter{
		requestCounts: make(map[routeMetricKey]uint64),
		durationSums:  make(map[durationMetricKey]float64),
		durationCount: make(map[durationMetricKey]uint64),
	}
}

func (e *PrometheusMetricsExporter) Observe(route, method string, status int, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.requestCounts[routeMetricKey{route: route, method: method, status: status}]++
	dk := durationMetricKey{route: route, method: method}
	e.durationSums[dk] += duration.Seconds()
	e.durationCount[dk]++
}

func (e *PrometheusMetricsExporter) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	e.mu.RLock()
	defer e.mu.RUnlock()

	var b strings.Builder
	b.WriteString("# HELP merm8_http_requests_total Total number of HTTP requests by route, method, and status.\n")
	b.WriteString("# TYPE merm8_http_requests_total counter\n")

	requestKeys := make([]routeMetricKey, 0, len(e.requestCounts))
	for key := range e.requestCounts {
		requestKeys = append(requestKeys, key)
	}
	requestCountsCopy := make(map[routeMetricKey]uint64, len(e.requestCounts))
	for _, key := range requestKeys {
		requestCountsCopy[key] = e.requestCounts[key]
	}

	durationKeys := make([]durationMetricKey, 0, len(e.durationCount))
	for key := range e.durationCount {
		durationKeys = append(durationKeys, key)
	}
	durationSumsCopy := make(map[durationMetricKey]float64, len(e.durationSums))
	durationCountCopy := make(map[durationMetricKey]uint64, len(e.durationCount))
	for _, key := range durationKeys {
		durationSumsCopy[key] = e.durationSums[key]
		durationCountCopy[key] = e.durationCount[key]
	}

	e.mu.RUnlock()

	sort.Slice(requestKeys, func(i, j int) bool {
		if requestKeys[i].route != requestKeys[j].route {
			return requestKeys[i].route < requestKeys[j].route
		}
		if requestKeys[i].method != requestKeys[j].method {
			return requestKeys[i].method < requestKeys[j].method
		}
		return requestKeys[i].status < requestKeys[j].status
	})
	for _, key := range requestKeys {
		fmt.Fprintf(&b, "merm8_http_requests_total{route=%q,method=%q,status=%q} %d\n", key.route, key.method, strconv.Itoa(key.status), requestCountsCopy[key])
	}
	}

	_, _ = w.Write([]byte(b.String()))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// MetricsMiddleware observes request status/duration for mapped routes.
func MetricsMiddleware(next http.Handler, routePatterns map[string]string, exporter *PrometheusMetricsExporter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routeKey := r.Method + " " + r.URL.Path
		route := routePatterns[routeKey]
		if route == "" || exporter == nil {
			next.ServeHTTP(w, r)
			return
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)
		exporter.Observe(route, r.Method, recorder.status, time.Since(start))
	})
}
