# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Configurable parser timeout via `PARSER_TIMEOUT_SECONDS` environment variable (1–60 seconds, default 5s). Exposed in `GET /info` response as `parser_timeout_seconds` field.
- `POST /analyze/sarif` returns SARIF 2.1.0 format for all error responses (previously returned JSON with 200 OK). **Breaking change**: Clients must expect proper HTTP status codes (400, 413, 504, 503, 500) with SARIF error format.
- Comprehensive test coverage for timeout configurability, SARIF error response format, and concurrent error handling.

### Changed

- Parser source enhancement is now gated to flowchart-family diagrams and can be toggled with `PARSER_SOURCE_ENHANCEMENT` (`true` by default) for controlled rollout.
- Source-level node analysis was optimized to avoid repeated full-source passes and reduce regex-heavy scanning overhead on large diagrams.

- Parser bridge now caches successful parse results and syntax-error results using a short-lived LRU keyed by request code, parser limits, and parser version, with cache hit/miss/eviction telemetry exposed in Prometheus metrics.
- Parser bridge now supports a long-lived Node worker pool mode (`PARSER_MODE=pool`) with newline-delimited JSON request/response envelopes and per-request timeout recovery that recycles only the stuck worker process.
- Syntax-error remediation now recognizes unsupported first-line Mermaid types (`gantt`, `pie`) before generic fallback, returning dedicated `hints` and matching `help-suggestion` guidance with line-1 targeting.
- Syntax-error remediation now normalizes malformed first-line diagram type headers (e.g. `sequence`, `class`, `stateDiagramv2`, and casing/punctuation variants) into canonical Mermaid keywords, emits `diagram_type_header_typo` hints, and prioritizes line-specific fix guidance in `help-suggestion`.
- Generic syntax-error fallback messaging now includes bounded line/column context and compact source excerpts when available, while safely handling zero or out-of-range parser positions without changing response schema.
- Documented extensibility contract for rule IDs, `/v1/rules` compatibility guarantees, and deterministic plugin/rule loading strategy in API and namespace docs.
- Config normalization now accepts namespaced built-in rule IDs (`core/<id>`) as aliases, normalizes them to canonical built-in IDs, and deterministically merges mixed alias entries.

- `GET /rules` and `GET /rules/schema` now advertise only rules implemented by the active runtime registry; configs that reference non-implemented/planned rules return `400 unknown_rule` without implying enforceability.
- Rule severity overrides are now normalized with case-insensitive, trimmed parsing, and only canonical values `error`, `warning`, and `info` are accepted; the legacy `warn` alias is now rejected.
- `POST /analyze/sarif` error responses now return proper HTTP status codes instead of 200 OK with embedded errors.
  - Invalid JSON: `400 Bad Request`
  - Request too large: `413 Payload Too Large`
  - Parser timeout: `504 Gateway Timeout`
  - Server busy: `503 Service Unavailable`
  - Internal errors: `500 Internal Server Error`

## [0.1.0] - 2026-03-03

### Added

- Deterministic Mermaid lint API with `POST /analyze` and structured syntax/lint responses.
- Rule metadata discovery endpoint (`GET /rules`) for live rule/config documentation.
- Config JSON Schema endpoint (`GET /rules/schema`) and versioned schema artifact at `schemas/config.v1.json`.
- Per-rule suppression directives via `suppression-selectors` in canonical config.
- Health/readiness/metrics endpoints (`GET /healthz`, `GET /health`, `GET /ready`, `GET /metrics`).

### Changed

- Parser bridge now supports a long-lived Node worker pool mode (`PARSER_MODE=pool`) with newline-delimited JSON request/response envelopes and per-request timeout recovery that recycles only the stuck worker process.
- Canonical request/response/config JSON naming to kebab-case.
- Config handling to support versioned schema format (`{"schema-version":"v1","rules":{...}}`) with deprecation signaling for legacy key styles/shapes.
