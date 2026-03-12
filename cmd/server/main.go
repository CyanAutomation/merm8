package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/CyanAutomation/merm8/internal/api"
	"github.com/CyanAutomation/merm8/internal/engine"
	"github.com/CyanAutomation/merm8/internal/parser"
	"github.com/CyanAutomation/merm8/internal/telemetry"
)

const (
	defaultParserConcurrencyLimit   = 8
	defaultRateLimitPerMinute       = 120
	defaultAllowedOrigins           = "https://merm8-splash.vercel.app"
	dockerAllowedOriginsPlaceholder = "https://merm8.example.app"
	defaultShutdownTimeout          = 10 * time.Second
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
	p, err := parser.New(scriptPath)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize parser: %v", err))
	}
	handler := api.NewHandler(p, engine.New())

	logger := api.NewLogger("server")
	handler.SetLogger(api.NewLogger("api"))

	parserConcurrencyLimit := envInt("PARSER_CONCURRENCY_LIMIT", defaultParserConcurrencyLimit)
	handler.SetParserConcurrencyLimit(parserConcurrencyLimit)

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
	rootHandler = api.AnalyzeResponseCompressionMiddleware(rootHandler, 0)

	authToken := strings.TrimSpace(os.Getenv("ANALYZE_AUTH_TOKEN"))
	if err := validateStartupAuthToken(deploymentMode, authToken); err != nil {
		logger.Error("invalid startup authentication configuration", "mode", deploymentMode, "error", err.Error())
		panic(err.Error())
	}

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

	configuredAllowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	allowedOrigins, warnOnAllowedOrigins := resolveAllowedOrigins(deploymentMode, configuredAllowedOrigins)
	if warnOnAllowedOrigins {
		logger.Warn("ALLOWED_ORIGINS appears misconfigured for production", "mode", deploymentMode, "configured_allowed_origins", strings.TrimSpace(configuredAllowedOrigins), "placeholder_allowed_origins", dockerAllowedOriginsPlaceholder, "recommended_allowed_origins", defaultAllowedOrigins)
	}
	rootHandler = api.CORSMiddleware(allowedOrigins, logger, metrics)(rootHandler)

	addr := fmt.Sprintf(":%s", port)
	parserCfg := parser.ConfigFromEnv().EffectiveConfig()
	parserExecMode := parser.ModeFromEnv()
	parserWorkerPoolSize := parser.WorkerPoolSizeFromEnv()
	logger.Info("server starting", "addr", addr, "parser", scriptPath, "mode", deploymentMode, "parser_mode", parserExecMode, "parser_mode_default", "pool", "parser_worker_pool_size", parserWorkerPoolSize, "parser_worker_pool_size_guidance", "tune with CPU/memory headroom when parser_mode=pool", "parser_concurrency_limit", parserConcurrencyLimit, "parser_timeout_seconds", int(parserCfg.Timeout.Seconds()), "parser_max_old_space_mb", parserCfg.NodeMaxOldSpaceMB, "analyze_rate_limit_per_minute", rateLimitPerMinute, "analyze_auth_enabled", authToken != "", "cors_allowed_origins", allowedOrigins)

	server := &http.Server{Addr: addr, Handler: rootHandler}
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err.Error())
			panic(fmt.Sprintf("server error: %v", err))
		}
	case <-shutdownCtx.Done():
		logger.Info("shutdown signal received")
	}

	gracefulCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	
	var shutdownErrs []error
	if err := server.Shutdown(gracefulCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("graceful shutdown failed", "error", err.Error())
		shutdownErrs = append(shutdownErrs, err)
	}
	if err := p.Close(); err != nil {
		logger.Error("parser shutdown failed", "error", err.Error())
		shutdownErrs = append(shutdownErrs, err)
	}
	
	if len(shutdownErrs) > 0 && gracefulCtx.Err() == context.DeadlineExceeded {
		logger.Warn("shutdown completed with timeout", "shutdown_timeout_seconds", int(defaultShutdownTimeout.Seconds()))
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

func resolveAllowedOrigins(deploymentMode, rawAllowedOrigins string) (string, bool) {
	configured := strings.TrimSpace(rawAllowedOrigins)
	warnOnMisconfiguration := strings.EqualFold(strings.TrimSpace(deploymentMode), "production") &&
		(configured == "" || configured == dockerAllowedOriginsPlaceholder)

	if configured == "" {
		return defaultAllowedOrigins, warnOnMisconfiguration
	}

	return configured, warnOnMisconfiguration
}

func validateStartupAuthToken(deploymentMode, authToken string) error {
	if strings.EqualFold(strings.TrimSpace(deploymentMode), "production") && strings.TrimSpace(authToken) == "" {
		return fmt.Errorf("ANALYZE_AUTH_TOKEN is required when DEPLOYMENT_MODE=production")
	}

	return nil
}
