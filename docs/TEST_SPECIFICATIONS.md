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

- **Requirement**: POST `/v1/analyze` requests without `code` field return 400 with error code `missing_code`
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

### API-008: Versioned Analysis Spelling Aliases

- **Requirement**: Canonical `/v1/analyze` routes remain available, `/v1/analyse` spelling aliases remain available with deprecation headers, and unversioned routes are not advertised or registered
- **Tests**: `TestRegisterRoutes_V1CanonicalAndSpellingAliases`, `TestServeSpec_RegisteredAnalysisAliasesAreDocumented`
- **Files**: `internal/api/handler_test.go`, `internal/api/openapi_test.go`

### DOCS-001: OpenAPI Copies Are Independent

- **Requirement**: Mutating a returned OpenAPI document does not mutate a later document returned by the package
- **Tests**: `TestOpenAPISpec_ReturnsIndependentCopy`
- **File**: `internal/api/openapi_test.go`

### DOCS-002: Registered Service Routes Are Documented

- **Requirement**: OpenAPI documents the registered versioned health, metadata, metrics, and diagram-type endpoints using their actual paths and operation IDs
- **Tests**: `TestServeSpec_RegisteredServiceRoutesAreDocumented`
- **File**: `internal/api/openapi_test.go`

### API-009: Nil Engine Dependency Uses Default Engine

- **Requirement**: A handler created without an engine dependency still analyzes a valid diagram successfully using its default engine
- **Tests**: `TestNewHandler_DefaultsNilEngineDependency`
- **File**: `internal/api/handler_nil_engine_test.go`

### API-010: SARIF Fallback Request URI

- **Requirement**: A direct SARIF handler call without a request URL returns a valid SARIF report with the stable `/analyze/sarif` request URI
- **Tests**: `TestAnalyzeSARIF_NilURLUsesFallbackRequestURI`
- **File**: `internal/api/handler_test.go`

### API-011: Large Topology Metrics and Findings

- **Requirement**: Large linear, high-fan-out, and high-fan-in diagrams return accurate structure metrics and rule findings
- **Tests**: `TestAnalyze_LargeTopologyMetricsAndFindings`
- **File**: `internal/api/handler_test.go`
- **Runtime note**: The test checks semantic results; performance budgets belong in dedicated benchmarks rather than a wall-clock assertion in the unit suite.

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

### FLOW-CONTROL-001: Runtime Parser Limit Updates Preserve In-Flight Accounting

- **Requirement**: Reapplying the configured parser concurrency limit while parses are in flight does not admit work beyond the configured cap
- **Tests**: `TestAnalyze_ParserConcurrencyLimit_RuntimeUpdates_DoNotCreateParallelLimiters`, `TestTryAcquireParserSlot_ReleaseCallbackIsIdempotent`
- **Files**: `internal/api/handler_test.go`, `internal/api/handler_internal_test.go`

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
- **Tests**: `TestParseCache_GetReturnedDiagramMutationDoesNotAffectCachedDiagram`
- **File**: `internal/parser/cache_test.go`
- **Validation**: Covers node positions, edges, subgraphs, suppressions, and derived fields

### CACHE-002: Cached Parse Honors Timeout Overrides

- **Requirement**: A cached successful parse must not be reused for a request whose effective parser timeout differs; the shorter timeout is enforced and the parser executes again
- **Tests**: `TestParserCache_DoesNotReuseResultAcrossTimeoutOverrides`
- **File**: `internal/parser/cache_integration_test.go`

### CACHE-003: Concurrent Cache Operations Preserve Values

- **Requirement**: Concurrent writes and reads preserve every non-evicted cache value
- **Tests**: `TestLRUTTLCacheConcurrentAccessPreservesValues`
- **File**: `internal/parser/cache_test.go`

### PARSER-001: Runtime Version Metadata Contract

- **Requirement**: Parser version metadata is decoded into the supported bridge and Mermaid versions, cached after one lookup, and rejects malformed or incomplete output with the appropriate error
- **Tests**: `TestParser_VersionInfo`, `TestParser_VersionInfoRejectsMalformedOrIncompleteOutput`
- **File**: `internal/parser/parser_test.go`

### PARSER-002: Repository Root Discovery Failure

- **Requirement**: Parser initialization reports a stable actionable error when no repository root can be found, without changing the calling test process working directory
- **Tests**: `TestParser_NewFailsWhenRepoRootMissing`
- **File**: `internal/parser/parser_test.go`

### PARSER-003: Subprocess Timeout Enforcement and Recovery

- **Requirement**: Parser timeouts return the timeout category, bound incomplete worker responses, and replace timed-out pooled workers before subsequent parses
- **Tests**: `TestParser_TimeoutCategory`, `TestParser_WorkerPoolTimeoutReplacesWorker`, `TestParser_WorkerPoolTimeoutReturnsPromptlyWhenWorkerNeverWritesNewline`
- **File**: `internal/parser/parser_test.go`
- **Runtime note**: Tests use the configured minimum parser timeout so the subprocess contract is exercised without the default 10-second wait.

### PARSER-NODE-001: Settled Worker Requests Clear Timeout Timers

- **Requirement**: Successful and failed worker requests clear their timeout timer after settling
- **Tests**: `withWorkerTimeout clears its timer when the wrapped promise settles`
- **File**: `parser-node/parse.test.ts`

### WORKER-002: Unhealthy Worker Release Does Not Wait for Active Operations

- **Requirement**: An unhealthy worker can be discarded while its operation lock is held, and the pool can create a replacement
- **Tests**: `TestWorkerPoolUnhealthyReleaseDoesNotWaitForInFlightOperation`
- **File**: `internal/parser/worker_pool_test.go`

### WORKER-001: Concurrent Worker Request IDs Are Unique

- **Requirement**: Concurrent request ID generation returns nonempty unique identifiers
- **Tests**: `TestNewWorkerRequestIDReturnsUniqueIDsConcurrently`
- **File**: `internal/parser/parser_internal_test.go`

### ENGINE-001: Nil Diagram Produces an Empty Issue Result

- **Requirement**: Both engine entry points safely return a non-nil empty issue slice and zero instrumentation metrics for a nil diagram
- **Tests**: `TestEngine_NilDiagramReturnsNonNilEmptyIssues`
- **File**: `internal/engine/engine_test.go`

### SMOKE-001: Versioned Health Endpoint

- **Requirement**: The service health route returns HTTP 200 with `status: ok`
- **Checks**: `health`
- **File**: `smoke-test.sh`

### SMOKE-002: Analysis Metrics and Configured Rule Behavior

- **Requirement**: Valid flowcharts return correct node/edge counts, and configured `max-fanout` limits produce the corresponding issue
- **Checks**: `valid diagram`, `configured max-fanout rule`
- **File**: `smoke-test.sh`

### SMOKE-003: SARIF and Request Validation Contracts

- **Requirement**: The SARIF route returns a SARIF 2.1.0 document with the correct media type and preserves configured rule IDs and severity; a missing code field returns HTTP 400 with `missing_code`
- **Checks**: `SARIF endpoint`, `SARIF with configured violation`, `missing code`
- **File**: `smoke-test.sh`

### SMOKE-004: Analyze Rate Limit

- **Requirement**: A client exceeding the configured analysis quota receives HTTP 429 with `rate_limited`
- **Checks**: `rate-limit mode` (opt-in; must run against a fresh service/client window)
- **File**: `smoke-test.sh`
- **Classification**: Manual deployment smoke only; run against a disposable service and fresh client quota window. Deterministic automated coverage lives in `TestAnalyzeRateLimitMiddleware_Returns429` and `TestServerStack_AnalyzeRateLimited_IncludesCORSAndRateLimitHeaders`.

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

## Mutation Testing Status

Mutation checks use `gremlins v0.5.1`, pinned by `scripts/mutation-baseline.sh` and exposed as `make test-mutation`. The initial baseline, captured 2026-10-07, covers `internal/engine`: 69.12% test efficacy (47 killed, 21 lived) and 73.91% mutant coverage (68 of 92 mutants). The parser package is not included in this baseline because its coverage pass takes about 50 seconds and Gremlins reruns package tests for each covered mutant.

`benchmarks/BENCHMARK.md` describes rule-efficacy benchmarks and is not a mutation report. Mutation efficacy and mutant coverage are reported separately by Gremlins; compare future runs against the same tool version and package scope.
