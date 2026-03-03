package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/telemetry"
)

const (
	defaultParserConcurrencyLimit = 8
	defaultRateLimitPerMinute     = 120
)

var appVersion = ""

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
		panic(fmt.Sprintf("failed to initialize handler: %v", err))
	}

	logger := api.NewLogger("server")
	handler.SetLogger(api.NewLogger("api"))

	parserConcurrencyLimit := envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit)
	handler.SetParserConcurrencyLimit(parserConcurrencyLimit)

	metrics := telemetry.NewMetrics()
	handler.SetMetricsHandler(metrics.Handler())
	handler.SetTelemetryMetrics(metrics)
	handler.SetServiceVersion(strings.TrimSpace(appVersion))

	handler.RegisterRoutes(mux)

	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)

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
		"GET /health":   "/health",
		"GET /healthz":  "/healthz",
		"GET /ready":    "/ready",
		"GET /info":     "/info",
		"GET /metrics":  "/metrics",
		"POST /analyze": "/analyze",
	}
	rootHandler = api.MetricsMiddleware(rootHandler, routePatterns, metrics)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, logger)

	addr := fmt.Sprintf(":%s", port)
	parserTimeoutSecs := int(getParserTimeout().Seconds())
	logger.Info("server starting", "addr", addr, "parser", scriptPath, "mode", deploymentMode, "parser_concurrency_limit", parserConcurrencyLimit, "parser_timeout_seconds", parserTimeoutSecs, "analyze_rate_limit_per_minute", rateLimitPerMinute, "analyze_auth_enabled", authToken != "")
	if err := http.ListenAndServe(addr, rootHandler); err != nil {
		logger.Error("server error", "error", err.Error())
		panic(fmt.Sprintf("server error: %v", err))
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

func getParserTimeout() time.Duration {
	const (
		defaultTimeout = 5 * time.Second
		minTimeout     = 1 * time.Second
		maxTimeout     = 60 * time.Second
	)

	raw := strings.TrimSpace(os.Getenv("PARSER_TIMEOUT_SECONDS"))
	if raw == "" {
		return defaultTimeout
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultTimeout
	}

	timeout := time.Duration(value) * time.Second
	if timeout < minTimeout || timeout > maxTimeout {
		return defaultTimeout
	}

	return timeout
}
