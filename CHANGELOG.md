# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Rule severity overrides are now normalized with case-insensitive, trimmed parsing, and only canonical values `error`, `warning`, and `info` are accepted; the legacy `warn` alias is now rejected.


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
