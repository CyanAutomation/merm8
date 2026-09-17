# Implementation Guide — merm8 Mermaid Lint

This guide covers the internal implementation of the merm8 service, including build process, runtime configuration, and integration details for contributors and maintainers.

## Project Structure

The merm8 service follows a Go modular architecture:

- **`cmd/server/`** — Main entry point for the HTTP API server; wires up handler, parser, engine, middleware, and config from environment variables.
- **`cmd/merm8-cli/`** — CLI binary for local/offline analysis or server-mode execution.
- **`internal/api/`** — HTTP handlers (`handler.go`), middleware (auth, rate limiting, CORS, metrics), and OpenAPI spec generation.
- **`internal/parser/`** — Node.js subprocess bridge; manages worker pool or subprocess mode, timeouts, memory limits, and source enhancement.
- **`internal/engine/`** — Rule execution engine; iterates registered rules against parsed diagram model, collects issues and rule metrics.
- **`internal/rules/`** — Rule interface definition, built-in implementations per diagram family, config registry, and JSON schema generation.
- **`internal/model/`** — Shared types: `Diagram`, `Node`, `Edge`, `Issue`, `DiagramType`, `DiagramFamily`.
- **`internal/output/sarif/`** — SARIF 2.1.0 transformation from lint results.
- **`internal/telemetry/`** — Prometheus metric collection and handler exposure.
- **`parser-node/`** — Node.js Mermaid parser script (`parse.mjs`) with dependencies.

## Build Process

### Prerequisites

- Go 1.24+
- Node.js 20+ and npm

### Building

```bash
# Install Node parser dependencies
cd parser-node && npm install && cd ..

# Build server
go build -o mermaid-lint ./cmd/server

# Build CLI
go build -o merm8-cli ./cmd/merm8-cli
```

### Running Tests

```bash
# All unit tests
go test ./...

# Parser integration tests (requires PARSER_SCRIPT env var)
PARSER_SCRIPT=./parser-node/parse.mjs go test ./internal/parser/...
go test ./internal/api/...

# Coverage
go test -cover ./...

# Race detector on core packages
go test -race ./internal/api ./internal/engine ./internal/parser
```

### Benchmark Suite

```bash
# Run benchmark suite
go run ./cmd/merm8-bench --input benchmarks/fixtures/ --output json,html,markdown

# Compare against baseline
go run ./cmd/merm8-bench --compare-baseline benchmarks/reports/baseline.json
```

See [docs/benchmarking.md](docs/benchmarking.md) for full benchmark capabilities documentation.

## Current Capabilities Summary

| Feature | Status | Details |
| ------- | ------ | ------- |
| Parser worker pool (`PARSER_MODE=pool`) | Implemented | Long-lived Node workers with timeout recovery |
| Parser cache (LRU keyed by code+version) | Implemented | Cache hits/misses/evictions in Prometheus metrics |
| Source-level AST enhancement | Configurable | `PARSER_SOURCE_ENHANCEMENT=true`; gated to flowchart family |
| Syntax-error hint codes | Implemented | 18+ hint codes covering typo detection, format errors, structural hints |
| Help suggestions | Implemented | Structured `help-suggestion` field with before/after examples |
| Parser override in request body | Implemented | Optional `parser.timeout_seconds` and `max_old_space_mb` |
| Trusted proxy IP attribution | Implemented | Right-to-left `X-Forwarded-For` parsing |
| Conditional gzip compression | Implemented | Applied when `Accept-Encoding: gzip` and payload exceeds threshold |
| CORS wildcard support | Implemented | Single `*` wildcard with prefix/suffix matching |
| Config namespace aliases | Implemented | `core/<id>` normalized to bare ID; merged on conflict |
| Sarif error responses | Implemented | Proper HTTP status codes with SARIF format |

## Architecture Notes

### Parser Bridge

The parser bridge executes the Node.js Mermaid parser via either:
- **Subprocess mode** (`PARSER_MODE=subprocess`): one `node parse.mjs` invocation per request
- **Pool mode** (`PARSER_MODE=pool`): long-lived Node workers communicating via newline-delimited JSON envelopes; timed-out workers are recycled individually

Both modes share the same short-lived LRU cache keyed by request code, parser limits, and parser version.

### Engine Execution

Rules execute in deterministic static order: Flowchart → Sequence → Class → ER → State. Issue sorting is deterministic (severity priority → rule-id → line → column → message). Duplicates are removed.

### Plugin Loading

No runtime network fetch occurs during engine startup. Built-in groups append in static order. Duplicate registrations are rejected using canonical IDs.
