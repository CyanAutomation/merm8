package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
)

const (
	defaultParserConcurrencyLimit = 8
	defaultRateLimitPerMinute     = 120
)

func main() {
	scriptPath := os.Getenv("PARSER_SCRIPT")
	if scriptPath == "" {
		scriptPath = "/app/parser-node/parse.mjs"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	deploymentMode := strings.ToLower(strings.TrimSpace(os.Getenv("DEPLOYMENT_MODE")))
	if deploymentMode == "" {
		deploymentMode = "development"
	}

	mux := http.NewServeMux()
	handler, err := api.NewHandlerWithScript(scriptPath)
	if err != nil {
		log.Fatalf("failed to initialize handler: %v", err)
	}

	parserConcurrencyLimit := envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit)
	handler.SetParserConcurrencyLimit(parserConcurrencyLimit)

	metricsExporter := api.NewPrometheusMetricsExporter()
	handler.SetMetricsHandler(metricsExporter)

	handler.RegisterRoutes(mux)

	rootHandler := http.Handler(mux)

	authToken := strings.TrimSpace(os.Getenv("ANALYZE_AUTH_TOKEN"))
	rateLimitPerMinute := envInt("ANALYZE_RATE_LIMIT_PER_MINUTE", 0)
	if deploymentMode == "production" && rateLimitPerMinute <= 0 {
		rateLimitPerMinute = defaultRateLimitPerMinute
	}
	if rateLimitPerMinute > 0 {
		limiter := api.NewRateLimiter(rateLimitPerMinute, time.Minute)
		rootHandler = api.AnalyzeRateLimitMiddleware(limiter, rootHandler)
	}

	if deploymentMode == "production" {
		rootHandler = api.AnalyzeBearerAuthMiddleware(authToken, rootHandler)
	}

	routePatterns := map[string]string{
		"GET /healthz":  "/healthz",
		"GET /ready":    "/ready",
		"POST /analyze": "/analyze",
	}
	rootHandler = api.MetricsMiddleware(rootHandler, routePatterns, metricsExporter)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("mermaid-lint listening on %s (parser: %s, mode: %s, parser_concurrency_limit: %d, analyze_rate_limit_per_minute: %d, analyze_auth_enabled: %t)", addr, scriptPath, deploymentMode, parserConcurrencyLimit, rateLimitPerMinute, authToken != "")
	if err := http.ListenAndServe(addr, rootHandler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}
