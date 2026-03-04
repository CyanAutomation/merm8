# API Guide — merm8 Mermaid Lint API

This guide walks you through interacting with the merm8 API, using both the interactive Swagger UI and direct HTTP requests.

---

## Quick Start: Using Swagger UI

The easiest way to explore and test the API is through the interactive Swagger UI dashboard.

### Accessing the Swagger UI

1. **Start the server**:
   ```bash
   # From the workspace root
   PARSER_SCRIPT=./parser-node/parse.mjs go run ./cmd/server
   ```

2. **Open in your browser**:
   ```
   http://localhost:8080/v1/docs
   ```

You should see a professional API documentation page with all available endpoints.

### Operational environment variables

For deployment sizing and overload behavior, the parser runtime exposes three key env vars:

| Variable | Default | Behavior |
|---|---|---|
| `PARSER_TIMEOUT_SECONDS` | `5` | Timeout for each parse operation in seconds. Valid range: 1–60. Increase for complex diagrams, decrease to prioritize responsiveness. Exposed via `GET /info` canonical response field `parser-timeout-seconds` (legacy alias `parser_timeout_seconds` is temporarily retained for compatibility and is deprecated). |
| `PARSER_CONCURRENCY_LIMIT` | `8` | Caps in-flight parser subprocesses. When the limit is reached, the server does **not queue indefinitely**; additional `POST /v1/analyze` requests are rejected with `503` and `error.code=server_busy` (`parser concurrency limit reached; try again`) and include `Retry-After: 1` to signal the minimum retry delay in seconds. |
| `PARSER_MAX_OLD_SPACE_MB` | `512` | Sets the Node.js parser subprocess V8 old-space heap cap (`--max-old-space-size=<MB>`), limiting parser memory growth per parse process. |

Use these together with your platform CPU/memory limits to tune throughput versus memory headroom in production.

#### Per-request parser overrides (optional)

`POST /v1/analyze` also accepts an optional `parser` object for bounded overrides:

```json
{
  "code": "graph TD
A-->B",
  "parser": {
    "timeout_seconds": 8,
    "max_old_space_mb": 768
  }
}
```

Validation bounds are enforced server-side regardless of requested value:
- `parser.timeout_seconds`: default **5**, accepted range **1–60** seconds
- `parser.max_old_space_mb`: default **512**, accepted range **128–4096** MiB

Out-of-range values are rejected with `400` and `error.code=invalid_option`. If diagrams are very large/complex, prefer splitting them into smaller diagrams (or batched requests) before raising limits.

#### Parser failure remediation payloads

Timeout and parser-memory failures include structured remediation details under `error.details` when safe:
- `suggestion`: actionable mitigation (reduce diagram size, batch requests, raise limits)
- `limit`: effective timeout or memory cap that was hit
- `observed_size`: request code size in bytes (memory-limit errors)

Parser execution error codes:
- `parser_timeout` (HTTP 504)
- `parser_memory_limit` (HTTP 500)
- `parser_subprocess_error` (HTTP 500)
- `parser_decode_error` (HTTP 500)
- `parser_contract_violation` (HTTP 500)

#### Tuning guidance

For production stability:
- Start with defaults (`timeout=5s`, `max_old_space=512MB`, `concurrency=8`).
- If timeouts occur, first reduce diagram complexity (split large diagrams into smaller subgraphs) before increasing timeout.
- If memory-limit errors occur, batch large analysis jobs and/or raise `PARSER_MAX_OLD_SPACE_MB` conservatively.
- Prefer tiered limits by environment (e.g., lower defaults on shared/dev tiers, higher caps on dedicated/prod tiers).


---


## Endpoint versioning and deprecation

Canonical endpoints are versioned under `/v1`. Legacy unversioned routes are still served as migration aliases and are deprecated, with planned removal in **v1.2.0 (Q2 2026)**.

## Interactive API Testing with Swagger UI

### The Swagger Dashboard

The Swagger UI provides:

- **Left sidebar** — List of all available endpoints (now `/v1/healthz`, `/v1/ready`, `/v1/rules`, `/v1/analyze`, `/v1/analyze/sarif`, `/v1/spec`, `/v1/docs` (with deprecated unversioned aliases))
- **Main panel** — Detailed endpoint documentation with parameters and response schemas
- **Try it out button** — Execute requests directly from the browser
- **Example requests** — Pre-filled request templates for common scenarios



### Scraping Service Metrics (`GET /metrics`)

The API exposes Prometheus metrics at `GET /metrics` in text exposition format.

Metric families:
- `request_total{route,method,status}`
- `request_duration_seconds{route,method}` histogram
- `analyze_requests_total{outcome}`
- `parser_duration_seconds{outcome}` histogram

Common `outcome` values include `syntax_error`, `lint_success`, `parser_timeout`, `parser_subprocess_error`, `parser_decode_error`, `parser_contract_violation`, and `internal_error`.

Example:
```bash
curl -s http://localhost:8080/metrics
```

### Discovering Rules with `/rules`

Use **`GET /rules`** to discover the live enforceable rule catalog at runtime (only rules implemented by the active runtime engine are advertised).

The response includes:
- Rule identifier
- Default severity
- Rule description
- Default configuration
- Configurable option docs (name/type/constraints)

This is the recommended source for integrations and generated docs.

### Testing the `/analyze` Endpoint

#### Step 1: Click "Try it out"

1. Navigate to the **`POST /v1/analyze`** section
2. Click the blue **"Try it out"** button

#### Step 2: Enter a Mermaid Diagram

The request body editor will appear. Enter your Mermaid code:

```json
{
  "code": "graph TD\n  A[Start] --> B[Process]\n  B --> C[End]"
}
```

**Example diagrams to try:**

**Valid diagram (no issues):**
```json
{
  "code": "graph TD\n  A --> B\n  B --> C"
}
```

**Diagram with disconnected nodes:**
```json
{
  "code": "graph TD\n  A --> B\n  C[Isolated]"
}
```

**Diagram with high fan-out (warning severity):**
```json
{
  "code": "graph TD\n  A --> B\n  A --> C\n  A --> D\n  A --> E\n  A --> F\n  A --> G"
}
```

#### Step 3: Configure Lint Rules (Optional)

Add a `config` field to customize rule behavior:

```json
{
  "code": "graph TD\n  A --> B\n  A --> C\n  A --> D",
  "config": {
    "rules": {
      "max-fanout": {
        "enabled": true,
        "severity": "error",
        "limit": 2,
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

**Supported rule configurations:**

- `max-fanout` — Set maximum outgoing edges per node
  ```json
  "max-fanout": { "enabled": true, "severity": "warning", "limit": 3 }
  ```

Unknown rule IDs in config are rejected with `400 invalid_config`.

#### Step 4: Execute and View Results

1. Click the blue **"Execute"** button
2. Scroll down to see:
   - **Response code** (200 for successful analysis)
   - **Response body** (detailed analysis results)
   - **Response headers** (metadata)

### Understanding the Response

| HTTP status | `valid` | `syntax-error` | `issues` | `error` | when it occurs |
|---|---:|---|---|---|---|
| `200` | `true` | `null` | `[]` | `null` | Diagram parsed and linted successfully with no lint findings. |
| `200` | `true` | `null` | Non-empty array | `null` | Diagram parsed and linted successfully, and lint findings were produced. |
| `200` | `false` | Populated object | `[]` | `null` | Mermaid parser reports a syntax parse failure. |
| Non-`200` (`400`/`413`/`429`/`500`/`503`/`504`) | `false` | `null` | `[]` | Populated object | API-level failure (request validation/limits/infrastructure/timeout). |

`issues` is always present as an array. `syntax-error` and `error` are mutually exclusive.

**Successful analysis of a valid diagram:**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "syntax-error": null,
  "issues": [],
  "metrics": {
    "node-count": 3,
    "edge-count": 2,
    "max-fanout": 1
  }
}
```

**Response with lint issues:**
```json
{
  "valid": true,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "no-disconnected-nodes",
      "severity": "error",
      "message": "Node 'Isolated' is not connected to the graph"
    }
  ],
  "metrics": {
    "node-count": 3,
    "edge-count": 2,
    "max-fanout": 1
  }
}
```

**Syntax error response:**
```json
{
  "valid": false,
  "lint-supported": false,
  "syntax-error": {
    "message": "No diagram type detected",
    "line": 0,
    "column": 0
  },
  "issues": []
}
```

**Request error response (HTTP 400/413/429/500/503/504):**
```json
{
  "valid": false,
  "lint-supported": false,
  "syntax-error": null,
  "issues": [],
  "error": {
    "code": "invalid_json",
    "message": "invalid JSON body"
  }
}
```

**Common API error codes:**
- `missing_code` — `code` field is missing or empty (HTTP 400)
- `invalid_json` — body is not valid JSON (HTTP 400)
- `request_too_large` — request body exceeds 1 MiB (HTTP 413)
- `server_busy` — parser concurrency is saturated; retry later (HTTP 503, includes `Retry-After`; clients should back off and honor the header before retrying)
- `parser_timeout` — parser timed out (HTTP 504)
- `internal_error` — unexpected internal parser/service failure (HTTP 500)

### Retry Strategy for `503 server_busy`

When the API returns `503` with `error.code=server_busy`, check the `Retry-After` header first:

- If `Retry-After` is an integer, treat it as delay seconds.
- If `Retry-After` is an HTTP-date, wait until that timestamp.
- If the header is missing or invalid, use a conservative fallback delay (for example, 1 second) before applying exponential backoff.

Recommended retry policy for clients:

1. Retry only on transient statuses (`503`, optionally `429`/`504` depending on workload).
2. Use exponential backoff with jitter.
3. Cap retries to a finite budget (for example, max 5 attempts total).

Stable contract for `server_busy`: the service returns `Retry-After: 1` (delta-seconds) on `503` from `/v1/analyze` and `/v1/analyze/sarif` as a minimum retry floor.

Example backoff schedule (before jitter): `1s`, `2s`, `4s`, `8s`, `16s`.
Add jitter (for example ±20% randomization) per attempt to avoid synchronized retry bursts.

### Response Fields Explained

**Current type support behavior:**
- `flowchart`/`graph` diagrams are linted by built-in rules.
- `sequence`, `class`, `er`, and `state` diagrams are parsed, and return `valid=false`, `lint-supported=false`, `issues=[]`, a structured `error.code` of `unsupported_diagram_type`, and populated `metrics` computed from the parsed diagram (with empty issue-count maps).

- **`valid`** — Boolean indicating if the Mermaid syntax is syntactically correct
- **`diagram-type`** — Normalized Mermaid type for valid diagrams (`flowchart`, `sequence`, `class`, `er`, `state`, `unknown`)
- **`lint-supported`** — Whether the parsed diagram type currently has active lint rule coverage
- **`syntax-error`** — Object with parsing error details (present only for parser syntax failures; otherwise `null`)
  - `message` — Human-readable error description
  - `line` — 1-based line number where error occurred
  - `column` — 0-based column number where error occurred
- **`issues`** — Array of lint rule violations found (always present, empty when there are no issues)
  - `rule-id` — The lint rule that triggered
  - `severity` — One of: `error`, `warning`, `info`
    - Deprecated alias: `warn` is accepted for backwards compatibility and normalized to `warning`.
  - `message` — Description of the issue
  - `line` / `column` — Optional location in the diagram code (omitted when unknown)
- **`issues`** can include findings both with source locations (`line`/`column`) and without them when exact positions are unavailable.
- **`issues[].fingerprint`** is a required deterministic SHA-256 hash over normalized issue fields (`rule-id`, `severity`, `message`, `line`, `column`, and grouping context) suitable for CI baselining.
- **`issues[].context`** is optional grouping metadata. For node-scoped findings in subgraphs, it includes `subgraph-id` and `subgraph-label`; it is omitted when no grouping applies.
  - `line` / `column` — Location in the diagram code
  - **Ordering guarantee** — Issues are deterministically sorted before returning: by severity priority (`error` → `warning` → `info`), then `rule-id`, then `line`, then `column`, then `message`. If two rules produce the exact same issue signature, duplicates are removed.
- **`metrics`** — Statistics about the diagram structure (also populated for parsed-but-unsupported families and syntax-error responses)
  - `node-count` — Total nodes in the diagram
  - `edge-count` — Total connections/edges
  - `max-fanout` — Maximum outgoing edges from any single node

---

## Testing the `/v1/analyze/raw` Endpoint (Raw Mermaid Text)

The `/v1/analyze/raw` endpoint allows you to send raw Mermaid code directly without JSON wrapping. This is simpler for quick testing but does **not** support lint rule configuration (use `/v1/analyze` if you need that).

### Format Auto-Detection

The endpoint auto-detects the input format:
- **Plain text** (Content-Type: `text/plain`) — Entire body is treated as raw Mermaid code
- **JSON** (Content-Type: `application/json`) — Attempts to parse `{"code": "..."}` structure, falls back to raw text if parsing fails

### Using the Swagger UI

1. Navigate to **`POST /v1/analyze/raw`** (or `/analyze/raw` for legacy unversioned)
2. Click **"Try it out"**
3. In the request body, enter raw Mermaid code:
   ```
   graph TD
     A[Start] --> B[Process]
     B --> C[End]
   ```
4. Click **"Execute"**

### Using curl

#### Plain Text Request

```bash
curl -X POST http://localhost:8080/v1/analyze/raw \
  -H "Content-Type: text/plain" \
  --data 'graph TD
  A[Start] --> B[Process]
  B --> C[End]'
```

Or more simply:

```bash
curl -X POST http://localhost:8080/v1/analyze/raw \
  -d 'graph TD
  A[Start] --> B[Process]
  B --> C[End]'
```

#### Sequence Diagram Example

```bash
curl -X POST http://localhost:8080/v1/analyze/raw \
  -d 'sequenceDiagram
  Alice ->> Bob: Hello Bob, how are you?
  Bob-->>Alice: I am good thanks!'
```

#### Using `curl` with a File

```bash
curl -X POST http://localhost:8080/v1/analyze/raw \
  --data-binary @diagram.mmd
```

#### JSON Format (Auto-Detected)

If you prefer JSON, the endpoint auto-detects it:

```bash
curl -X POST http://localhost:8080/v1/analyze/raw \
  -H "Content-Type: application/json" \
  -d '{"code": "graph TD\n  A --> B"}'
```

### Response

The response has the same structure as `/v1/analyze`:
- Successful analysis with no lint issues
- Lint violations (if linting is supported for the diagram type)
- Syntax error details

**Note:** Since `/v1/analyze/raw` does not accept configuration, all lint rules use their defaults.

---

### Using `curl`

If you prefer working with `curl` or other HTTP clients instead of the GUI:

#### Basic Valid Diagram

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A[Start] --> B[End]"
  }'
```

#### With Configuration

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A --> B\n  A --> C\n  A --> D",
    "config": {
      "rules": {
        "max-fanout": {
          "limit": 2
        }
      }
    }
  }'
```

#### Pretty-Print the Response

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "graph TD\n  A --> B"}' | jq .
```

### Using Other HTTP Clients

#### Postman

1. Create a new request (POST)
2. URL: `http://localhost:8080/analyze`
3. Headers tab: Add `Content-Type: application/json`
4. Body tab: Select "raw" → "JSON"
5. Paste your request:
   ```json
   {
     "code": "graph TD\n  A --> B",
     "config": { "rules": { "max-fanout": { "limit": 3 } } }
   }
   ```
6. Click "Send"

#### Python `requests`

```python
import json
import random
import time
from email.utils import parsedate_to_datetime

import requests

url = "http://localhost:8080/analyze"
payload = {
    "code": "graph TD\n  A[Start] --> B[Process]\n  B --> C[End]",
    "config": {
        "rules": {
            "max-fanout": {"limit": 3}
        }
    }
}

def retry_after_seconds(value: str | None) -> float | None:
    if not value:
        return None
    value = value.strip()
    if value.isdigit():
        return float(value)
    try:
        dt = parsedate_to_datetime(value)
        return max(0.0, dt.timestamp() - time.time())
    except Exception:
        return None


max_attempts = 5
base_delay = 1.0

for attempt in range(1, max_attempts + 1):
    response = requests.post(url, json=payload)
    if response.status_code != 503:
        break

    result = response.json()
    if result.get("error", {}).get("code") != "server_busy":
        break

    header_delay = retry_after_seconds(response.headers.get("Retry-After"))
    backoff_delay = base_delay * (2 ** (attempt - 1))
    jitter = random.uniform(0, backoff_delay * 0.2)
    sleep_for = header_delay if header_delay is not None else (backoff_delay + jitter)
    time.sleep(sleep_for)

result = response.json()

print(f"Valid: {result['valid']}")
print(f"Issues: {len(result['issues'])}")
for issue in result['issues']:
    print(f"  - {issue['rule-id']}: {issue['message']}")
```

#### JavaScript / Node.js

```javascript
const payload = {
  code: "graph TD\n  A --> B\n  A --> C\n  A --> D",
  config: {
    rules: {
      "max-fanout": { limit: 2 }
    }
  }
};

const maxAttempts = 5;
const baseDelayMs = 1000;

const retryAfterToMs = (value) => {
  if (!value) return null;
  const trimmed = value.trim();
  if (/^\d+$/.test(trimmed)) return Number(trimmed) * 1000;
  const at = Date.parse(trimmed);
  if (Number.isNaN(at)) return null;
  return Math.max(0, at - Date.now());
};

let response;
for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
  response = await fetch('http://localhost:8080/analyze', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  });

  if (response.status !== 503) break;

  const body = await response.clone().json();
  if (body?.error?.code !== 'server_busy') break;

  const retryAfterMs = retryAfterToMs(response.headers.get('Retry-After'));
  const expBackoffMs = baseDelayMs * (2 ** (attempt - 1));
  const jitterMs = Math.random() * expBackoffMs * 0.2;
  const sleepMs = retryAfterMs ?? (expBackoffMs + jitterMs);
  await new Promise((resolve) => setTimeout(resolve, sleepMs));
}

const data = await response.json();
console.log(`Valid: ${data.valid}`);
console.log(`Issues: ${data.issues.length}`);
for (const issue of data.issues) {
  console.log(`  - ${issue['rule-id']}: ${issue.message}`);
}
```

---

## API Endpoints Reference

### GET `/healthz` (canonical probe) and aliases `/health`, `/`

**Description:** Liveness-only endpoint for process-up probes. `/` is provided as a minimal alias for platforms that require root health checks.  
**Response:** JSON status payload (`{"status":"ok"}`)  
**Usage:**

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/
```

### GET `/ready`

**Description:** Dependency/readiness-only endpoint for critical dependencies (parser runtime/script checks when available). This endpoint may return `503` when dependencies are not ready.  
**Response:**
- `200` with `{"status":"ready"}` when ready
- `503` with `{"status":"not_ready","error":"..."}` when not ready

**Usage:**

```bash
curl -i http://localhost:8080/ready
```

### GET `/version`

**Description:** Informational-only endpoint for service/build metadata (service version, build commit/time, parser/runtime versions when available). This endpoint is stable and unauthenticated for probe tooling and diagnostics, but should not be used as readiness gating.  
**Response:** JSON object of string metadata fields (keys present when values are configured).  

**Usage:**

```bash
curl http://localhost:8080/version
```

### GET `/docs`

**Description:** Interactive Swagger UI dashboard for API exploration  
**Response:** HTML page that loads Swagger UI from CDN  
**Usage:** Open in browser: `http://localhost:8080/v1/docs`

### GET `/spec`

**Description:** Returns the full OpenAPI 3.0 specification as JSON  
**Response:** OpenAPI specification object  
**Usage:** For code generation, API documentation tools, or external integrations

```bash
curl http://localhost:8080/spec | jq .
```

### POST `/analyze`

#### Source-level suppression directives

You can suppress lint findings directly in Mermaid source using comment directives:

- `%% merm8-disable <rule-id>` or `%% merm8-ignore <rule-id>`
- `%% merm8-disable all` or `%% merm8-ignore all`
- `%% merm8-disable-next-line <rule-id>` or `%% merm8-ignore-next-line <rule-id>`
- `%% merm8-disable-next-line all` or `%% merm8-ignore-next-line all`

`all` suppresses every rule. Rule-specific suppressions only affect matching `rule-id` values.

**Description:** Validate and lint a Mermaid diagram  
**Request body:**
```json
{
  "code": "string (required) - Mermaid diagram code",
  "config": "object (optional) - Rule configuration"
}
```

**Response:** Analysis results with validity, syntax errors (if any), lint issues, and metrics


### Deployment probe settings

Recommended defaults for common platforms:

- Liveness: `GET /healthz` (or `GET /` for platforms that only support root probes)
- Readiness: `GET /ready`
- Informational metadata: `GET /version` (diagnostics only; not a readiness/liveness signal)

Kubernetes example:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
readinessProbe:
  httpGet:
    path: /ready
    port: 8080
```

---

## Rule Configuration Guide

### Available Rules

The merm8 engine includes three built-in lint rules:

#### `no-duplicate-node-ids`
- **Severity:** error
- **Purpose:** Ensures each node ID is unique
- **Configuration:** No options
- **Example response:**
  ```json
  {
    "rule-id": "no-duplicate-node-ids",
    "severity": "error",
    "message": "Duplicate node ID 'A'"
  }
  ```

#### `no-disconnected-nodes`
- **Severity:** error
- **Purpose:** Ensures all nodes are connected to the graph
- **Configuration:** No options
- **Example response:**
  ```json
  {
    "rule-id": "no-disconnected-nodes",
    "severity": "error",
    "message": "Node 'isolated' is not connected"
  }
  ```

#### `max-fanout`
- **Severity:** warning
- **Purpose:** Limits maximum outgoing edges from a single node
- **Configuration:** `limit` (integer, default: 5)
- **Example:**
  ```json
  {
    "config": {
      "rules": {
        "max-fanout": { "limit": 3 }
      }
    }
  }
  ```
- **Example response:**
  ```json
  {
    "rule-id": "max-fanout",
    "severity": "warning",
    "message": "Node 'A' has fanout of 6, exceeds limit of 5"
  }
  ```

### Configuration Format and Deprecation Policy

Accepted canonical format (versioned contract):

```json
{
  "code": "...",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": { "limit": 3 }
    }
  }
}
```

#### Deprecation Policy (Phase-1 acceptance)

| Legacy input | Accepted since | Warn since | Remove in |
|---|---|---|---|
| `config.schema_version` (snake_case) | v1.0.0 | v1.0.0 | v1.2.0 (Q2 2026 planned) |
| Unversioned nested config (`config.rules` without `config.schema-version`) | v1.0.0 | v1.0.0 | v1.2.0 (Q2 2026 planned) |
| Flat config shape (`config.{rule-id}` at root) | v1.0.0 | v1.0.0 | v1.2.0 (Q2 2026 planned) |
| Snake_case option keys under a rule (for example `suppression_selectors`) | v1.0.0 | v1.0.0 | v1.2.0 (Q2 2026 planned) |

**Deprecation signals (runtime):**
- HTTP `Warning` header(s): one or more `299 - "..."` values with exact migration examples.
- HTTP `Deprecation: true` header.
- JSON response `warnings` array (string messages).
- JSON response `meta.warnings[]` structured objects with `code`, `message`, and `replacement` example.
- Server log warning event: `legacy config format received` including migration hint text.

See [docs/migration-guide.md](docs/migration-guide.md) for a full rollout timeline and before/after payload examples.

**Migration Guide (Legacy → Canonical)**:

Legacy flat format:
```json
{
  "code": "...",
  "config": {
    "max_fanout": { "limit": 3, "severity": "error" },
    "no_disconnected_nodes": { "enabled": false }
  }
}
```

Canonical format:
```json
{
  "code": "...",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": { "limit": 3, "severity": "error" },
      "no-disconnected-nodes": { "enabled": false }
    }
  }
}
```

Key changes:
- Add `"schema-version": "v1"` at config root
- Wrap rules under `"rules"` key
- Use kebab-case key names: `max_fanout` → `max-fanout`, `no_disconnected_nodes` → `no-disconnected-nodes`

### SARIF Output (`POST /analyze/sarif`)

Use `POST /analyze/sarif` with the same request body as `/analyze` to receive SARIF 2.1.0 (`Content-Type: application/sarif+json`) for valid analyses.

Canonical severity mapping is defined in code at `internal/output/sarif` and used by API docs:
- `error -> error`
- `warning/warn -> warning`
- `info -> note`

Unsupported versions are rejected with `400 unsupported_schema_version` and include a `supported` list.

### Pre-validate Config with `GET /rules/schema`

Fetch the generated JSON Schema and validate `config` in your client before calling `/analyze`.

For pinned tooling/CI usage, use the versioned artifact in this repo: `schemas/config.v1.json`.

```bash
curl -s http://localhost:8080/rules/schema | jq '.schema'
```

Example (Node + Ajv):

```js
import Ajv from "ajv";

const ajv = new Ajv();
const schemaResp = await fetch("http://localhost:8080/rules/schema").then(r => r.json());
const validate = ajv.compile(schemaResp.schema);

const config = {
  schema-version: "v1",
  rules: {
    "max-fanout": {
      enabled: true,
      severity: "warning",
      limit: 3,
      suppression-selectors: ["node:A"]
    }
  }
};

if (!validate(config)) {
  console.error(validate.errors);
}
```

---

## Testing Examples

### Example 1: Valid Diagram with No Issues

**Request:**
```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph LR\n  A[User] --> B[System]\n  B --> C[Database]"
  }'
```

**Response:**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "syntax-error": null,
  "issues": [],
  "metrics": {
    "node-count": 3,
    "edge-count": 2,
    "max-fanout": 1
  }
}
```

### Example 2: High Fan-out Warning

**Request:**
```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A --> B\n  A --> C\n  A --> D\n  A --> E\n  A --> F\n  A --> G",
    "config": {"rules": {"max-fanout": {"limit": 4}}}
  }'
```

**Response:**
```json
{
  "valid": true,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "max-fanout",
      "severity": "warning",
      "message": "Node 'A' has fanout of 6, exceeds limit of 4",
      "line": 2,
      "column": 2
    }
  ],
  "metrics": {
    "node-count": 7,
    "edge-count": 6,
    "max-fanout": 6
  }
}
```

### Example 3: Disconnected Node Error

**Request:**
```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A --> B\n  B --> C\n  D[Isolated Node]"
  }'
```

**Response:**
```json
{
  "valid": true,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "no-disconnected-nodes",
      "severity": "error",
      "message": "Node 'D' is not connected to the graph"
    }
  ],
  "metrics": {
    "node-count": 4,
    "edge-count": 2,
    "max-fanout": 1
  }
}
```

### Example 4: Syntax Error

**Request:**
```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "not valid mermaid syntax"
  }'
```

**Response:**
```json
{
  "valid": false,
  "lint-supported": false,
  "syntax-error": {
    "message": "No diagram type detected",
    "line": 0,
    "column": 0
  },
  "issues": []
}
```

---

## Troubleshooting

### "Connection refused" Error

**Problem:** Server isn't running or listening on the wrong port  
**Solution:**
```bash
# Check if server is running
ps aux | grep -i "go run"

# Start the server explicitly
cd /workspaces/merm8
PARSER_SCRIPT=./parser-node/parse.mjs go run ./cmd/server
```

### "/docs" Returns 404

**Problem:** Swagger UI endpoint not registered  
**Solution:** Ensure you have the latest code that includes the Swagger endpoints

### "Invalid JSON" Error in Response

**Problem:** Request body is malformed  
**Solution:**
- Ensure all JSON strings use double quotes: `"code": "..."`
- Escape newlines in Mermaid code: `"code": "graph TD\n  A --> B"`
- Use valid JSON syntax (check with `jq` or similar)

### JSON Parsing in `curl`

**Escaping newlines in bash:**
```bash
# Using $'...' syntax (ANSI-C quoting)
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d $'{"code": "graph TD\n  A --> B"}'

# Or using echo with -e
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d "$(echo -e '{"code": "graph TD\\n  A --> B"}')"
```

---

## Integration Tips

### Validating Diagrams in a CI/CD Pipeline

Use the API to validate Mermaid diagrams before committing:

```bash
#!/bin/bash
# validate-diagrams.sh

API_URL="http://localhost:8080/analyze"

for diagram_file in diagrams/*.mmd; do
  content=$(cat "$diagram_file")
  response=$(curl -s -X POST "$API_URL" \
    -H "Content-Type: application/json" \
    -d "{\"code\": $(echo "$content" | jq -Rs .)}")
  
  valid=$(echo "$response" | jq .valid)
  
  if [ "$valid" != "true" ]; then
    echo "❌ $diagram_file is invalid"
    echo "$response" | jq .
    exit 1
  fi
done

echo "✅ All diagrams are valid"
```

### Batch Processing

Convert multiple diagrams and collect all issues:

```bash
for code in "graph TD\n  A-->B" "graph LR\n  X-->Y-->Z"; do
  echo "Analyzing: $code"
  curl -s -X POST http://localhost:8080/analyze \
    -H "Content-Type: application/json" \
    -d "{\"code\": $(echo -e "$code" | jq -Rs .)}" | jq .
done
```

---

## Additional Resources

- **OpenAPI Specification:** Available at `http://localhost:8080/spec`
- **GitHub Repository:** https://github.com/CyanAutomation/merm8
- **Mermaid Documentation:** https://mermaid.js.org/

---

**Happy Linting! 🎨**

---

## CLI usage for local + CI (`cmd/merm8-cli`)

You can run merm8 via a CLI in two modes:

1. **Local/offline mode (default)** — parse + lint in-process (good for CI runners without API connectivity).
2. **Server mode (`--url`)** — send code to `POST /v1/analyze`.

Build:

```bash
go build -o merm8-cli ./cmd/merm8-cli
```

### Inputs

- File path(s):
  ```bash
  ./merm8-cli diagrams/a.mmd diagrams/b.mmd
  ```
- stdin:
  ```bash
  cat diagrams/a.mmd | ./merm8-cli --stdin
  ```
- If no files are provided, stdin is used automatically.

### Outputs

- Human-readable text (default):
  ```bash
  ./merm8-cli --format text diagrams/a.mmd
  ```
- JSON (API-like fields):
  ```bash
  ./merm8-cli --format json diagrams/a.mmd
  ```

JSON includes `valid`, `diagram-type`, `lint-supported`, `syntax-error`, `issues`, and `error` where applicable.

### Config passing

Use `--config <file>` for rule overrides:

```bash
./merm8-cli --config lint-config.json diagrams/a.mmd
```

Versioned config shape:

```json
{
  "schema-version": "v1",
  "rules": {
    "max-fanout": { "enabled": true, "limit": 2 }
  }
}
```

### Exit codes

- `0`: success / no configured failure condition
- `1`: lint or syntax findings when fail flags are enabled
- `2`: local/internal/config/input failure
- `3`: transport failure in server mode (`--url`)

### CI examples

Offline CI:

```bash
PARSER_SCRIPT=./parser-node/parse.mjs ./merm8-cli --fail-on-lint --config lint-config.json diagrams/**/*.mmd
```

Server mode CI:

```bash
./merm8-cli --url http://localhost:8080 --fail-on-lint --format json diagrams/**/*.mmd
```
