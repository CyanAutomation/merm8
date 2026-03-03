package promhttp

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

type HandlerOpts struct{}

func HandlerFor(reg *prometheus.Registry, _ HandlerOpts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(reg.Gather()))
	})
}
