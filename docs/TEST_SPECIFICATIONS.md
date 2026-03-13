# Test Specification Mapping

This document maps all refactored and high-value tests to their specification IDs for discoverability and traceability.

## Format

Each entry contains:

- **Spec ID**: Unique identifier (e.g., `API-001`, `OBSERVABILITY-001`)
- **Requirement**: What behavior/contract is being verified
- **Test(s)**: Go test function name(s)
- **Files**: Test file location(s)

---

## API Contract Tests

### API-001: Nil Diagram Error Response

- **Requirement**: When parser returns nil diagram, API responds with 500 and structured error response
- **Tests**: `TestAnalyze_ParserReturnsNilDiagram_Returns500`
- **File**: `internal/api/handler_test.go`
- **Notes**: Also verifies no panic occurs

### API-002: Missing Code Field Validation

- **Requirement**: POST /analyze requests without `code` field return 400 with error code `missing_code`
- **Tests**: `TestAnalyze_MissingCode`
- **File**: `internal/api/handler_test.go`

### API-003: Invalid JSON Request Body

- **Requirement**: Malformed JSON in request body returns 400 with error code `invalid_json`
- **Tests**: `TestAnalyze_InvalidJSON`, `TestAnalyze_RejectsTrailingContentAfterJSONObject`
- **File**: `internal/api/handler_test.go`

### API-004: Request Body Size Limit (1 MiB)

- **Requirement**: Requests with body >1 MiB return 413 and error code `request_too_large`
- **Tests**: `TestAnalyze_RequestBodyTooLarge`
- **File**: `internal/api/handler_test.go`
- **SLO**: Parser should NOT be invoked for oversized requests

### API-005: Parser Timeout Handling

- **Requirement**: Parser timeout returns 504 with error code `parser_timeout`
- **Tests**: `TestAnalyze_ParserTimeout_Returns504`, `TestAnalyzeV1_ParserTimeout_Returns504AndErrorCode`
- **File**: `internal/api/handler_test.go`

### API-006: Parser Subprocess Error Handling

- **Requirement**: Parser subprocess failure returns 500 with error code `parser_subprocess_error`
- **Tests**: `TestAnalyze_ParserSubprocessError_Returns500`
- **File**: `internal/api/handler_test.go`

### API-007: Parser Contract Violation Detection

- **Requirement**: Parser returning invalid AST structure returns 500 with error code `parser_contract_violation`
- **Tests**: `TestAnalyze_ParserContractViolation_Returns500`
- **File**: `internal/api/handler_test.go`

---

## Observability & Telemetry Tests

### OBSERVABILITY-001: Metrics Middleware Records HTTP Request Metadata

- **Requirement**: Metrics middleware records `request_total` counter with correct route, method, status labels
- **Tests**: `TestMetricsMiddleware_RecordsRequestWhenMetricsConfigured`
- **File**: `internal/api/metrics_middleware_test.go`
- **Assertion Strategy**: Structured Prometheus metric parsing (JSON-based labels)

### OBSERVABILITY-002: Metrics Middleware Preserves HTTP Behavior

- **Requirement**: Metrics middleware does NOT modify HTTP response status, headers, or body
- **Tests**: `TestMetricsMiddleware_PreservesHTTPBehavior`
- **File**: `internal/api/metrics_middleware_test.go`

### OBSERVABILITY-003: Metrics Middleware Records Without Side Effects

- **Requirement**: Metrics recording does NOT affect downstream request/response handling
- **Tests**: `TestMetricsMiddleware_RecordsMetricsWithoutSideEffects`
- **File**: `internal/api/metrics_middleware_test.go`

### TELEMETRY-001: Unknown Outcome Labels Fallback to 'other'

- **Requirement**: Telemetry library coerces unknown outcome labels to 'other' for metric safety
- **Tests**: `TestMetrics_UnknownAnalyzeOutcomeDefaultsToOther`, `TestMetrics_UnknownParserDurationOutcomeDefaultsToOther`
- **File**: `internal/telemetry/metrics_test.go`
- **Impact**: Prevents unbounded cardinality in Prometheus metrics

### TELEMETRY-002: Parser Cache Event Metric Label Normalization

- **Requirement**: Cache event labels are normalized (hit/miss → result; success/any → entry_type)
- **Tests**: `TestObserveParserCacheEvent_NormalizesLabels`
- **File**: `internal/telemetry/metrics_test.go`

---

## Rate Limiting & Flow Control Tests

### RATE-LIMIT-001: Nil Rate Limiter Pass-Through

- **Requirement**: When rate limiter is nil, all requests pass through unchanged without rate-limit headers
- **Tests**: `TestAnalyzeRateLimitMiddleware_NilLimiterPassesThroughWithoutHeaders`
- **File**: `internal/api/middleware_internal_test.go`

### RATE-LIMIT-002: Unknown Client Rejection at Capacity

- **Requirement**: Unknown clients rejected (429) when limiter at capacity; existing clients continue
- **Tests**: `TestRateLimiter_UnknownClientRejectedAtCapacity`, `TestRateLimiter_ExistingClientContinuesAtCapacity`
- **File**: `internal/api/middleware_internal_test.go`

---

## Hint & Help System Tests

### HINT-001: Syntax Error → Contextual Help Mapping

- **Requirement**: Syntax errors trigger contextual help hints (graphviz, arrow operator, tab indentation)
- **Tests**: `TestAnalyzeRaw_SyntaxError_HintMapping` (critical cases only)
- **File**: `internal/api/handler_help_test.go`
- **Coverage**: Graphviz syntax, single arrow (→ vs -->), tab indentation, common user mistakes
- **Note**: Reduced from 15 cases to 3-5 essential user-facing scenarios

---

## Parser Cache Tests

### CACHE-001: Parser Cache Returns Deep Copies

- **Requirement**: Parser cache returns deep copies; mutations by caller don't affect cached value
- **Tests**: `TestParseCache_GetSuccessReturnedDiagramMutationDoesNotAffectCachedDiagram`
- **File**: `internal/parser/cache_test.go`
- **Validation**: Covers node positions, edges, subgraphs, suppressions, and derived fields

---

## Integration Tests (with `build tag: integration`)

### INTEGRATION-001: Server Concurrency Busy Response with Retry-After

- **Requirement**: When parser concurrency limit saturated, return 503 with Retry-After header
- **Tests**: `TestServerContractIntegration_ConcurrencyBusyIncludesRetryAfter`
- **File**: `cmd/server/runtime_integration_test.go`

### INTEGRATION-002: Parser Timeout Handling (Real Parser)

- **Requirement**: Parser timeout (via PARSER_TIMEOUT_SECONDS env) returns 504 with JSON Content-Type
- **Tests**: `TestServerContractIntegration_ParserTimeoutFromControlledSlowFixture`
- **File**: `cmd/server/runtime_integration_test.go`

---

## Deprecated Helpers

The following test helpers are deprecated and should not be used for new tests. Use structured assertions instead.

### `assertExactErrorResponse(t, body, wantCode, wantMessage)`

- **Deprecation Reason**: Validates using field inspection; doesn't enforce response shape consistency
- **Replacement**: Use structured JSON unmarshaling + field-by-field assertions
- **Timeline**: Keep for backwards compatibility; migrate gradually

### `assertResponseHasHintCode(t, resp, wantCode)`

- **Deprecation Reason**: Brittle hint array searching; doesn't validate hint message content
- **Replacement**: Use `assertHintCodeAndMessage(t, resp, code, messageFragment)`
- **Timeline**: Keep for backwards compatibility; migrate gradually

---

## Mutation Testing Baseline

After refactoring, mutation testing baseline was updated:

- **Before**: 3 low-value tests removed; 7 tests refactored with structured assertions
- **Expected Impact**: Mutation score maintained or improved (fewer brittle assertions prone to false negatives)
- **Baseline File**: See `benchmarks/BENCHMARK.md` for detailed mutation score report
