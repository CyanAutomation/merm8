# Error Responses Reference

This document provides a comprehensive guide to all error codes returned by the merm8 API, including HTTP status codes, response structures, and recommended client-side handling strategies.

## Error Response Structure

All error responses follow a consistent JSON structure:

```json
{
  "error": {
    "code": "error_code_string",
    "message": "human-readable description",
    "details": {
      "path": "location in config or request",
      "supported": ["list", "of", "valid", "options"],
      "hint": "additional guidance"
    }
  }
}
```

- **code**: Machine-readable error identifier (kebab-case)
- **message**: Human-readable explanation suitable for logging and UI display
- **details**: Context-specific information:
  - `path`: Indicates where in the request the error occurred (e.g., `config.rules.max-fanout.limit`)
  - `supported`: List of valid options when applicable (shown for `unknown_rule`, `unknown_option`, `invalid_option`)
  - `hint`: Additional guidance on how to fix the issue

## Configuration Validation Errors (HTTP 400)

These errors occur when the request body contains invalid configuration.

### `unknown_rule`

**HTTP Status**: 400 Bad Request

**When**: A rule ID in the config is not recognized by the engine.

**Example Request**:

```json
{
  "code": "graph TD\n  A --> B",
  "config": {
    "rules": {
      "custom-undefined-rule": {
        "enabled": true,
        "severity": "warning"
      }
    }
  }
}
```

**Example Response**:

```json
{
  "error": {
    "code": "unknown_rule",
    "message": "unknown rule: custom-undefined-rule",
    "details": {
      "path": "config.rules.custom-undefined-rule",
      "supported": ["max-depth", "max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids"]
    }
  }
}
```

**Client Guidance**:

1. Verify the rule ID is in the `supported` list
2. Check for typos (rule IDs are case-sensitive and use kebab-case: `max-fanout`, not `maxFanOut`)
3. If migrating from another linter, use the rule remapping/renaming feature
4. **Migration aid**: Set `config.allow-unknown-rules: true` to treat unknown rules as warnings instead of errors (Phase 1 deprecation feature)

---

### `unknown_option`

**HTTP Status**: 400 Bad Request

**When**: A configuration option within a rule is not recognized.

**Example Request**:

```json
{
  "code": "graph TD\n  A --> B",
  "config": {
    "rules": {
      "max-fanout": {
        "enabled": true,
        "threshold": 5
      }
    }
  }
}
```

**Example Response**:

```json
{
  "error": {
    "code": "unknown_option",
    "message": "unknown option: threshold",
    "details": {
      "path": "config.rules.max-fanout.threshold",
      "supported": ["enabled", "limit", "severity", "suppression-selectors"]
    }
  }
}
```

**Client Guidance**:

1. Refer to the `supported` list to find the correct option name
2. Common migration issues:
   - `threshold` → `limit` (naming convention change)
   - `suppress_selectors` → `suppression-selectors` (kebab-case)
3. See [migration-guide.md](migration-guide.md) for config schema updates

---

### `invalid_option`

**HTTP Status**: 400 Bad Request

**When**: An option value is of the wrong type or outside valid constraints.

**Example Request**:

```json
{
  "code": "graph TD\n  A --> B\n  B --> C\n  B --> D\n  B --> E\n  B --> F",
  "config": {
    "rules": {
      "max-fanout": {
        "enabled": true,
        "limit": "five"
      }
    }
  }
}
```

**Example Response**:

```json
{
  "error": {
    "code": "invalid_option",
    "message": "invalid option value for limit: expected integer >= 1",
    "details": {
      "path": "config.rules.max-fanout.limit"
    }
  }
}
```

**Client Guidance**:

1. Check the type (integer, boolean, string, array, etc.)
2. Verify constraints (e.g., minimum/maximum values)
3. Consult [API_GUIDE.md](../API_GUIDE.md#rule-configuration-schema) for option types and constraints

---

### `invalid_suppression_selector`

**HTTP Status**: 400 Bad Request

**When**: A suppression selector has invalid syntax.

**Suppression Selector Formats**:

- **Rule suppression**: `rule:*` or `rule:max-fanout` (suppress all issues or from specific rule)
- **Node suppression**: `node:ID` (suppress issues on specific node by ID)
- **Subgraph suppression**: `subgraph:NAME` (suppress issues in specific subgraph)
- **Negation**: `!node:CriticalPath` (exclude nodes from suppression)

**Example Request with Invalid Selector**:

```json
{
  "code": "graph TD\n  A --> B",
  "config": {
    "rules": {
      "max-fanout": {
        "enabled": true,
        "suppression-selectors": ["node:"]
      }
    }
  }
}
```

**Example Response**:

```json
{
  "error": {
    "code": "invalid_suppression_selector",
    "message": "invalid suppression selector format: node:",
    "details": {
      "path": "config.rules.max-fanout.suppression-selectors[0]",
      "hint": "valid formats: rule:*, node:ID, subgraph:NAME, file:*.js, or negation with ! prefix"
    }
  }
}
```

**Client Guidance**:

1. Ensure selectors follow the valid formats shown in the hint
2. For node suppressions, the ID must match the node identifier in the diagram
3. See [rule-suppressions.md](examples/rule-suppressions.md) for detailed examples and patterns

---

### `deprecated_config_format`

**HTTP Status**: 400 Bad Request

**When**: Legacy configuration shapes are submitted in strict enforcement phase (v1.2.0+).

**Phase 1 (Current)**: Legacy config accepted with deprecation warnings in response headers
**Phase 2 (v1.2.0, Q2 2026)**: Legacy config rejected with HTTP 400

**Legacy Format Examples**:

```json
{
  "schema_version": "v1"
}
```

**Corrected Format**:

```json
{
  "schema-version": "v1"
}
```

**Client Guidance**:

1. Migrate config to use kebab-case field names (e.g., `schema-version` not `schema_version`)
2. Use nested structure: `{"schema-version": "v1", "rules": {...}}`
3. See [migration-guide.md](migration-guide.md#config-schema-phases) for timelines

---

## Request Validation Errors (HTTP 400)

### `missing_code`

**HTTP Status**: 400 Bad Request

**When**: The required `code` field is absent from POST body.

---

### `invalid_json`

**HTTP Status**: 400 Bad Request

**When**: The request body is not valid JSON.

---

## Request Size Errors (HTTP 413)

### `request_too_large`

**HTTP Status**: 413 Payload Too Large

**When**: Request body exceeds 1 MiB limit.

**Client Guidance**:

1. Split large diagrams into smaller, logically-distinct diagrams
2. Batch requests: submit multiple smaller requests instead of one huge request
3. Consider whether all complexity is necessary

---

## Parser Errors (HTTP 500 or 504)

These errors indicate problems during Mermaid parsing or execution.

### `parser_subprocess_error`

**HTTP Status**: 500 Internal Server Error

**When**: Parser subprocess crashed or exited unexpectedly.

**Likely Causes**:

- Out-of-memory conditions
- Parser subprocess killed by signal
- Unexpected parser runtime error

**Client Guidance**:

1. Retry with exponential backoff (e.g., 1s, 2s, 4s)
2. Check server resource constraints (memory, CPU)
3. Report to merm8 team if error persists

---

### `parser_decode_error`

**HTTP Status**: 500 Internal Server Error

**When**: Parser subprocess output is malformed or cannot be decoded.

**Likely Causes**:

- Parser subprocess generated invalid JSON
- Corrupted response due to subprocess crash
- Misdecoding of parser subprocess protocol

**Client Guidance**:

1. Retry the request
2. If issue persists, report to merm8 team with:
   - The diagram code
   - Server logs
   - Exact request body

---

### `parser_contract_violation`

**HTTP Status**: 500 Internal Server Error

**When**: Parser response violates the expected API contract (missing required fields, wrong types, etc.).

**Client Guidance**:

1. Similar to `parser_decode_error`, this indicates an internal inconsistency
2. Retry the request
3. Contact merm8 team if it persists

---

### `parser_timeout`

**HTTP Status**: 504 Gateway Timeout

**When**: Parser subprocess exceeds the configured timeout while validating the diagram.

**Timeout Limits**:

- Default: 30 seconds (configurable per-request: 1-60 seconds)
- Set via config: `{"parser": {"timeout-seconds": 30}}`

**Likely Causes**:

- Very complex diagram with expensive graph traversal
- Pathological input triggering worst-case algorithm behavior
- High server load causing parser delays

**Client Guidance**:

1. **Quick fix**: Increase per-request timeout (up to 60s max)

   ```json
   {
     "code": "...",
     "config": {...},
     "parser": {"timeout-seconds": 45}
   }
   ```

2. **Fundamental issue**: Simplify the diagram
   - Reduce node/edge count
   - Break into multiple smaller diagrams
   - Identify hot spots (e.g., nodes with very high fanout)
3. **Retry**: If caused by temporary load, retry with exponential backoff

---

### `parser_memory_limit`

**HTTP Status**: 500 Internal Server Error

**When**: Parser subprocess memory consumption exceeds limits.

**Memory Limits**:

- Default heap: 512 MiB (configurable via `PARSER_MAX_OLD_SPACE_MB`)
- Per-request override: 128-4096 MiB

**Client Guidance**:

1. Simplify the diagram (fewer nodes/edges)
2. If diagram is inherently large, request higher limit with:

   ```json
   {
     "code": "...",
     "parser": {"max-memory-mb": 1024}
   }
   ```

3. This may require server configuration adjustment

---

## Service Availability Errors (HTTP 503)

### `server_busy`

**HTTP Status**: 503 Service Unavailable

**When**: Parser concurrency limit is reached; server cannot accept new parse requests.

**Concurrency Limit**:

- Default: **8 concurrent parser subprocesses**
- Configurable: `PARSER_CONCURRENCY_LIMIT` environment variable

**Retry Mechanism**:

- Response includes `Retry-After` header (value: **1** second or HTTP date)
- Client should wait the suggested duration before retrying

**Example Response Headers**:

```
HTTP/1.1 503 Service Unavailable
Retry-After: 1
Content-Type: application/json
```

**Example Response Body**:

```json
{
  "error": {
    "code": "server_busy",
    "message": "parser concurrency limit reached; try again"
  }
}
```

**Client Guidance**:

1. **Implement exponential backoff with jitter**:
   - First retry: 1s (as suggested by `Retry-After`)
   - Subsequent retries: 2s, 4s, 8s, ... with small random jitter
   - Max retries: typically 5-10 depending on SLA
2. **Circuit breaker pattern** (optional):
   - If service consistently returns 503, temporarily skip analysis and log warning
   - Restore after observation period (e.g., 5 minutes)
3. **Load shedding** (optional):
   - If retries exceed threshold, reject request client-side rather than hammer server
4. **Increase concurrency** (server-side):
   - If legitimate load exceeds 8 concurrent requests, increase `PARSER_CONCURRENCY_LIMIT`
   - Monitor memory and CPU before increasing

---

## Rate Limiting (HTTP 429)

### `rate_limited`

**HTTP Status**: 429 Too Many Requests

**When**: Request rate exceeds configured limit (if rate limiting is enabled).

**Response Headers**:

```
X-RateLimit-Limit: 120
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1704067890
```

**Client Guidance**:

1. Read `X-RateLimit-Remaining` to know how many requests are available
2. Check `X-RateLimit-Reset` (Unix timestamp) to know when limit resets
3. Implement request queuing and back-off:

   ```
   wait_until = X-RateLimit-Reset
   retry_at = max(now + jitter, wait_until)
   ```

---

## Unsupported Diagram Types (HTTP 200)

### `unsupported_diagram_type`

**HTTP Status**: 200 OK

**When**: Diagram parses successfully but lint rules are not yet implemented for that diagram type.

**Affected Types** (as of v1.0.0):

- `sequence` — parsed, lint rules planned
- `class` — parsed, lint rules planned
- `state` — parsed, lint rules planned
- `er` — parsed, lint rules planned
- `gantt` — parsed, unsupported
- `pie` — parsed, unsupported

**Response Structure**:

```json
{
  "valid": true,
  "diagram-type": "sequence",
  "lint-supported": false,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "unsupported-diagram-type",
      "severity": "info",
      "message": "diagram type \"sequence\" is parsed but lint rules are not available yet"
    }
  ],
  "metrics": {
    "node-count": 0,
    "edge-count": 0,
    "diagram-type": "sequence",
    "..." : "zero counts"
  },
  "error": {
    "code": "unsupported_diagram_type",
    "message": "diagram type is parsed but linting is not supported"
  }
}
```

**Client Guidance**:

1. **Important**: `valid=true` means the diagram syntax is correct; linting is simply unavailable
2. This is **not a syntax error** — diagram parsing succeeded
3. Check `lint-supported` field to determine if rules are available:
   - `true`: lint results are authoritative
   - `false`: lint rules not available; use `diagram-type` to understand what will be supported
4. See [diagram-type-support.md](diagram-type-support.md) for roadmap and planned rule families

---

## Error Response Examples by Endpoint

### POST /v1/analyze

- Configuration errors: 400 (`unknown_rule`, `unknown_option`, `invalid_option`, `invalid_suppression_selector`, `deprecated_config_format`)
- Request errors: 400 (`invalid_json`, `missing_code`, `request_too_large`)
- Parser errors: 500 (`parser_subprocess_error`, `parser_decode_error`, `parser_contract_violation`, `parser_memory_limit`), 504 (`parser_timeout`)
- Service errors: 503 (`server_busy`)
- Rate limiting: 429 (`rate_limited`)

### POST /v1/analyze/raw

- Same as POST /v1/analyze, plus auto-detection of JSON vs. text format

### POST /v1/analyze/sarif

- Same as POST /v1/analyze (returns SARIF format for successful 200 responses)

### POST /v1/analyse (deprecated)

- Same as POST /v1/analyze (with deprecation warning header)

### GET /v1/metrics

- 501 Not Implemented if metrics exporter is not configured

---

## Best Practices

### Client-Side Error Handling

1. **Always check the HTTP status code first**:
   - 2xx: success (even if `lint-supported=false`)
   - 4xx: client error (usually fixable)
   - 5xx: server error (usually transient, retry recommended)
   - 503: service busy (retry with backoff)

2. **Use `error.code` for programmatic handling**:

   ```javascript
   if (response.error?.code === "server_busy") {
     // Implement exponential backoff retry
   } else if (response.error?.code === "unknown_rule") {
     // Log rule mismatch, suggest migration steps
   }
   ```

3. **Log `error.details` for debugging**:
   - `path`: where to fix the problem
   - `supported`: valid alternatives
   - `hint`: additional guidance

4. **Distinguish between syntax errors and linting unavailability**:
   - Syntax error: `valid=false` + `syntax-error` populated (HTTP 200)
   - Linting unavailable: `valid=true` + `lint-supported=false` + `error.code=unsupported_diagram_type`

### Server-Side Configuration

1. **Set appropriate concurrency limits**:
   - Default (8) is suitable for low-to-medium traffic
   - Increase if you see sustained 503 `server_busy` errors
   - Monitor memory and CPU before increasing

2. **Configure timeouts based on expected diagram complexity**:
   - Simple diagrams: 5-10s
   - Medium complexity: 15-30s (default)
   - Complex/pathological: 45-60s (per-request)

3. **Enable rate limiting if needed**:
   - Prevent abuse and ensure fair access
   - Communicate limits to clients via headers

---

## See Also

- [API_GUIDE.md](../API_GUIDE.md) — Complete API reference
- [migration-guide.md](migration-guide.md) — Config schema migration
- [diagram-type-support.md](diagram-type-support.md) — Diagram type roadmap
- [rule-suppressions.md](examples/rule-suppressions.md) — Suppression selector examples
