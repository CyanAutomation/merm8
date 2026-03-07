package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/telemetry"
)

const (
	defaultParserConcurrencyLimit = 8
	defaultRateLimitPerMinute     = 120
)

var appVersion = ""
var buildCommit = ""
var buildTime = ""

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

	// Wire up strict config schema enforcement based on environment variable.
	// Default is false for backward compatibility with v1.0; production deployments should enable.
	strictConfigSchema := strings.ToLower(strings.TrimSpace(os.Getenv("STRICT_CONFIG_SCHEMA")))
	if strictConfigSchema == "true" || strictConfigSchema == "1" {
		handler.SetStrictConfigSchema(true)
		logger.Info("Strict config schema enforcement enabled via STRICT_CONFIG_SCHEMA=true", "component", "server")
	}

	metrics := telemetry.NewMetrics()
	handler.SetMetricsHandler(metrics.Handler())
	handler.SetTelemetryMetrics(metrics)
	handler.SetServiceVersion(strings.TrimSpace(appVersion))
	handler.SetBuildMetadata(strings.TrimSpace(buildCommit), strings.TrimSpace(buildTime))

	handler.RegisterRoutes(mux)

	rootHandler := http.Handler(mux)
	rootHandler = api.RequestIDMiddleware(rootHandler)
	rootHandler = api.VersionNegotiationMiddleware(rootHandler)

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
		"GET /":             "/",
		"GET /health":       "/health",
		"GET /healthz":      "/healthz",
		"GET /ready":        "/ready",
		"GET /version":      "/version",
		"GET /info":         "/info",
		"GET /metrics":      "/metrics",
		"POST /analyze":     "/analyze",
		"POST /analyze/raw": "/analyze/raw",
	}
	rootHandler = api.MetricsMiddleware(rootHandler, routePatterns, metrics)
	rootHandler = api.AnalyzeLoggingMiddleware(rootHandler, logger)

	// Configure CORS with allowed origins from environment variable.
	// Apply this middleware last so it executes first on request entry,
	// including short-circuit 401/429 responses from inner middleware.
	allowedOrigins := strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
	if allowedOrigins == "" {
		// Default to Vercel frontend domain for merm8
		allowedOrigins = "https://merm8-splash.vercel.app"
	}
	rootHandler = api.CORSMiddleware(allowedOrigins, logger, metrics)(rootHandler)

	addr := fmt.Sprintf(":%s", port)
	parserCfg := parser.ConfigFromEnv().EffectiveConfig()
	logger.Info("server starting", "addr", addr, "parser", scriptPath, "mode", deploymentMode, "parser_concurrency_limit", parserConcurrencyLimit, "parser_timeout_seconds", int(parserCfg.Timeout.Seconds()), "parser_max_old_space_mb", parserCfg.NodeMaxOldSpaceMB, "analyze_rate_limit_per_minute", rateLimitPerMinute, "analyze_auth_enabled", authToken != "", "cors_allowed_origins", allowedOrigins)
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
