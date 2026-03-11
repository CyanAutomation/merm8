# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Benchmark Suite Enhancements (Phase 1)

- **False Positive Rate Tracking**: Benchmark suite now calculates and reports `false_positive_rate` (actual issues reported / total actual issues) per rule. Exposed in JSON, HTML, and CSV reports.
- **CSV Output Format**: Added `--output csv` flag (default: `json,html`). Benchmark results can now be exported as comma-separated values for spreadsheet analysis and programmatic integration.
- **Improved Version Detection**: Benchmark runner now detects version from:
  1. `MERM8_VERSION` environment variable (CI/build)
  2. `git describe --tags` (local development)
  3. Linker flag `-ldflags` (at build time)
  Makefile updated to propagate version via linker flags and environment variables.
- **Test Case Deduplication Warning**: Benchmark discovery now detects and warns about duplicate fixture content (same `.mmd` file included multiple times with different names). Helps identify wasted benchmark runtime.
- **Enhanced HTML Report**: False positive counts and rates added to rule metrics table for transparency into rule over-reporting behavior.

### Benchmark Suite Enhancements (Phase 2)

- **Performance Regression Detection**: Benchmark comparison (`--compare-baseline`) now detects and alerts on:
  - Detection rate drops (existing behavior, same threshold)
  - Parse time increases >10% per rule
  - Lint time increases >10% per rule
  Alerts printed with rule, baseline→current values, percentage increase, and threshold. Separate alerts for each regression type.
- **Test Coverage Analysis**: New "Coverage Analysis" section in HTML report shows:
  - ✅ Full coverage status when all rules have ≥5 test cases  
  - ⚠️ Limited coverage with list of low-coverage rules (<5 cases) and uncovered diagram types
  Helps identify gaps in test suite systematically.
- **Sample Size Indicators**: Detection rate now displays with sample size (e.g., "100.00% (2/2)") showing actual case count. Rules with <5 test cases display ⚠️ low-confidence badge in rule name column. Improves distinction between high-confidence metrics (many cases) and low-confidence ones (few cases).

### Benchmark Suite Enhancements (Phase 3)

- **Parser Instance Caching**: Benchmark runner now initializes a single parser instance at startup and reuses it across all benchmark cases, eliminating subprocess creation overhead per case. Parser is created once during `Run()` and passed to each `executeCase()` call. Reduces benchmark execution overhead while maintaining identical result accuracy.

### Benchmark Suite Enhancements (Phase 4)

- **Test Suite Expansion for New Diagram Types**: Added comprehensive test fixtures for sequence, class, ER, and state diagrams:
  - **Sequence diagrams**: 17 fixtures (valid interactions, duplicate actors, high message count, deep nesting, parallel messages)
  - **Class diagrams**: 12 fixtures (inheritance, composition, interfaces, circular dependencies, deep hierarchies)
  - **ER diagrams**: 10 fixtures (entity relationships, many-to-many, circular references, single entity edge cases)
  - **State diagrams**: 10 fixtures (state machines, transitions, unreachable states, nested states)
  - Total new fixtures: 49 across 4 diagram types; combined with 18 existing flowchart fixtures = 67 test cases
- **Updated Contributing Guide**: Extended `CONTRIBUTING.md` with diagram-type-specific examples, fixture templates, and best practices for sequence, class, ER, and state diagrams. Helps future contributors author test cases for new diagram types as rules are implemented.

Fixtures are discoverable and ready for rule implementation. As new rules are added for each diagram type, the existing fixtures will automatically be evaluated by the benchmark suite.
### Benchmark Suite Enhancements (Phase 5)

- **Enhanced Metadata Syntax**: Test fixtures now support optional expected issue counts in rule annotations:
  - `%% @rule: no-cycles:1` specifies expected count (currently for documentation)
  - Multiple rules with counts: `%% @rule: max-fanout:3, max-depth:1`
  - Enables future strict validation where exact counts must match
- **Trend Tracking Infrastructure**: Added data structures (`TrendMetric`, `TrendHistory`) for tracking benchmark metrics over time:
  - Timestamp-based metric recording per rule and benchmark run
  - Tracks detection rate, false positive rate, parse/lint times
  - Foundation for longitudinal analysis and regression detection across multiple benchmark runs
  - Current implementation stores structures (JSON serializable); collection for future runs not yet implemented
- **Interactive HTML Report** (Phase 5 Enhancement):
  - **Simple filtering**: Search box to filter rules by name (real-time highlighting)
  - **Click-to-sort table headers**: Sort by any column (rule name, pass count, detection rate, timing)
  - **Clean indicators**: ↑/↓/↕ symbols show current sort state (no animations, simple visual feedback)
  - Vanilla JavaScript with zero dependencies—works offline
  - Follows "normal UI" aesthetic: functional, no decorative elements, minimal transitions
### Added

- Configurable parser timeout via `PARSER_TIMEOUT_SECONDS` environment variable (1–60 seconds, default 5s). Exposed in `GET /info` response as `parser_timeout_seconds` field.
- `POST /analyze/sarif` returns SARIF 2.1.0 format for all error responses (previously returned JSON with 200 OK). **Breaking change**: Clients must expect proper HTTP status codes (400, 413, 504, 503, 500) with SARIF error format.
- Comprehensive test coverage for timeout configurability, SARIF error response format, and concurrent error handling.

### Changed

- Parser default execution mode is now `pool` (instead of `subprocess`); startup logging and parser tuning guidance now emphasize sizing `PARSER_WORKER_POOL_SIZE` to host CPU/memory capacity.
- **API now gracefully handles unknown rule IDs** — Unknown or cross-diagram-type rules are silently skipped during analysis instead of returning `400 invalid_config`. Clients can now send universal rule configurations without diagram-type-specific filtering. Deprecation warnings are included in the response to help clients identify and update outdated configs.
- Parser source enhancement is now gated to flowchart-family diagrams and can be toggled with `PARSER_SOURCE_ENHANCEMENT` (`true` by default) for controlled rollout.
- Source-level node analysis was optimized to avoid repeated full-source passes and reduce regex-heavy scanning overhead on large diagrams.

- Parser bridge now caches successful parse results and syntax-error results using a short-lived LRU keyed by request code, parser limits, and parser version, with cache hit/miss/eviction telemetry exposed in Prometheus metrics.
- Parser bridge now supports a long-lived Node worker pool mode (`PARSER_MODE=pool`) with newline-delimited JSON request/response envelopes and per-request timeout recovery that recycles only the stuck worker process.
- Syntax-error remediation now recognizes unsupported first-line Mermaid types (`gantt`, `pie`) before generic fallback, returning dedicated `hints` and matching `help-suggestion` guidance with line-1 targeting.
- Syntax-error remediation now normalizes malformed first-line diagram type headers (e.g. `sequence`, `class`, `stateDiagramv2`, and casing/punctuation variants) into canonical Mermaid keywords, emits `diagram_type_header_typo` hints, and prioritizes line-specific fix guidance in `help-suggestion`.
- Syntax-error remediation now detects prose/Markdown preambles before the first recognized Mermaid header, emits `non_mermaid_preamble_detected`, and provides guidance/examples to start with a diagram type on line 1.
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
- CORS allowlist origin matching now supports constrained wildcard entries (single `*` with explicit prefix/suffix, e.g. `https://merm8-splash-*.vercel.app`) while preserving exact-match behavior as the default fast path.

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
