# ─────────────────────────────────────────────────────────────────
# Stage 1 – Build the Go binary
# ─────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS go-builder

# Build arguments for version injection
ARG VERSION=v1.0.0
ARG BUILD_DATE
ARG VCS_REF
ARG REQUIRE_BENCHMARK_HTML=false

WORKDIR /src

# Copy dependency files first for better layer caching
# Only go.mod is needed (go.sum doesn't exist in this repo)
COPY go.mod ./

# Copy third_party replacement module before go mod download
COPY third_party ./third_party

# Download dependencies with cached layers
RUN go mod download

# Copy all source code
COPY . .

# Build the binary with version information
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-X main.appVersion=${VERSION}" \
    -o /app/mermaid-lint ./cmd/server

# Ensure benchmark artifact exists for downstream image stages.
# Strict mode is intended for CI/deploy, while local builds keep a placeholder fallback.
RUN if [ ! -f /src/benchmark.html ]; then \
      if [ "${REQUIRE_BENCHMARK_HTML}" = "true" ]; then \
        echo "Error: benchmark.html is required but missing. Generate it with: go run ./benchmarks/main.go" >&2; \
        exit 1; \
      fi; \
      printf '%s\n' '<!doctype html><html><body><p>benchmark.html was not pre-generated. Run `go run ./benchmarks/main.go` for a full report.</p></body></html>' > /src/benchmark.html; \
    fi

# ─────────────────────────────────────────────────────────────────
# Stage 2 – Lightweight runtime with Node + Mermaid
# ─────────────────────────────────────────────────────────────────
FROM node:20-alpine

# Build arguments for labels
ARG VERSION=v1.0.0
ARG BUILD_DATE
ARG VCS_REF

# Set metadata labels following OCI Image Spec conventions
LABEL org.opencontainers.image.title="merm8" \
      org.opencontainers.image.description="Mermaid diagram linter with syntax validation" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.vendor="CyanAutomation"

WORKDIR /app

# Copy Node package files for caching
COPY parser-node/package.json ./parser-node/

# Install Mermaid parser dependencies
# Using --omit=dev to exclude development dependencies
RUN cd parser-node && npm install --omit=dev && npm cache clean --force

# Copy parser entry point script
COPY parser-node/parse.mjs ./parser-node/

# Copy compiled Go binary from builder stage
COPY --from=go-builder /app/mermaid-lint .

# Copy benchmark report (CI-prebuilt if present, fallback placeholder otherwise)
COPY --from=go-builder /src/benchmark.html ./benchmark.html

# Copy go.mod so parser can locate repository root
COPY go.mod .

# Create non-root user for security (UID/GID 10001 avoids conflicts with existing users in alpine)
RUN addgroup -g 10001 appuser && \
    adduser -D -u 10001 -G appuser appuser && \
    chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Set environment variables with defaults
ENV PARSER_SCRIPT=/app/parser-node/parse.mjs \
    PORT=8080 \
    MERM8_API_URL=https://api.example.com

# Expose the server port
EXPOSE 8080

# Health check to enable orchestration systems to detect readiness
# Verifies the server is running on port 8080 using basic connectivity test
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Use exec form of ENTRYPOINT to properly receive signals (especially SIGTERM for graceful shutdown)
ENTRYPOINT ["/app/mermaid-lint"]
