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
- Canonical request/response/config JSON naming to kebab-case.
- Config handling to support versioned schema format (`{"schema-version":"v1","rules":{...}}`) with deprecation signaling for legacy key styles/shapes.
