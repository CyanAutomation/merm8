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
   http://localhost:8080/docs
   ```

You should see a professional API documentation page with all available endpoints.

---

## Interactive API Testing with Swagger UI

### The Swagger Dashboard

The Swagger UI provides:

- **Left sidebar** — List of all available endpoints (currently `/healthz`, `/ready`, `/rules`, `/analyze`, `/spec`, `/docs`)
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

Use **`GET /rules`** to discover the live built-in rule catalog at runtime.

The response includes:
- Rule identifier
- Default severity
- Rule description
- Default configuration
- Configurable option docs (name/type/constraints)

This is the recommended source for integrations and generated docs.

### Testing the `/analyze` Endpoint

#### Step 1: Click "Try it out"

1. Navigate to the **`POST /analyze`** section
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

**Request error response (HTTP 400/413/500):**
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
- `parser_timeout` — parser timed out (HTTP 504)
- `internal_error` — unexpected internal parser/service failure (HTTP 500)

### Response Fields Explained

**Current type support behavior:**
- `flowchart`/`graph` diagrams are linted by built-in rules.
- `sequence`, `class`, `er`, and `state` diagrams are parsed, and return `valid=false`, `lint-supported=false`, `issues=[]`, a structured `error.code` of `unsupported_diagram_type`, and populated `metrics` computed from the parsed diagram (with empty issue-count maps).

- **`valid`** — Boolean indicating if the Mermaid syntax is syntactically correct
- **`diagram-type`** — Normalized Mermaid type for valid diagrams (`flowchart`, `sequence`, `class`, `er`, `state`, `unknown`)
- **`lint-supported`** — Whether the parsed diagram type currently has active lint rule coverage
- **`syntax-error`** — Object with parsing error details (only present if `valid` is false)
  - `message` — Human-readable error description
  - `line` — 1-based line number where error occurred
  - `column` — 0-based column number where error occurred
- **`issues`** — Array of lint rule violations found (empty if no issues)
  - `rule-id` — The lint rule that triggered
  - `severity` — One of: `error`, `warning`, `info`
    - Deprecated alias: `warn` is accepted for backwards compatibility and normalized to `warning`.
  - `message` — Description of the issue
  - `line` / `column` — Optional location in the diagram code (omitted when unknown)
- **`issues`** can include findings both with source locations (`line`/`column`) and without them when exact positions are unavailable.
- **`issues[].fingerprint`** is a deterministic SHA-256 hash over normalized issue fields (`rule-id`, `severity`, `message`, `line`, `column`, and grouping context) suitable for CI baselining.
- **`issues[].context`** is optional grouping metadata. For node-scoped findings in subgraphs, it includes `subgraph-id` and `subgraph-label`; it is omitted when no grouping applies.
  - `line` / `column` — Location in the diagram code
  - **Ordering guarantee** — Issues are deterministically sorted before returning: by severity priority (`error` → `warning` → `info`), then `rule-id`, then `line`, then `column`, then `message`. If two rules produce the exact same issue signature, duplicates are removed.
- **`metrics`** — Statistics about the diagram structure (also populated for parsed-but-unsupported families and syntax-error responses)
  - `node-count` — Total nodes in the diagram
  - `edge-count` — Total connections/edges
  - `max-fanout` — Maximum outgoing edges from any single node

---

## Direct HTTP Requests

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
import requests
import json

url = "http://localhost:8080/analyze"
payload = {
    "code": "graph TD\n  A[Start] --> B[Process]\n  B --> C[End]",
    "config": {
        "rules": {
            "max-fanout": {"limit": 3}
        }
    }
}

response = requests.post(url, json=payload)
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

const response = await fetch('http://localhost:8080/analyze', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(payload)
});

const data = await response.json();
console.log(`Valid: ${data.valid}`);
console.log(`Issues: ${data.issues.length}`);
for (const issue of data.issues) {
  console.log(`  - ${issue.rule-id}: ${issue.message}`);
}
```

---

## API Endpoints Reference

### GET `/healthz`

**Description:** Liveness-only endpoint for process-up probes  
**Response:** JSON status payload (`{"status":"ok"}`)  
**Usage:**

```bash
curl http://localhost:8080/healthz
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

### GET `/docs`

**Description:** Interactive Swagger UI dashboard for API exploration  
**Response:** HTML page that loads Swagger UI from CDN  
**Usage:** Open in browser: `http://localhost:8080/docs`

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

Legacy migration timeline:

1. **Phase 1 (current)**: legacy snake_case keys/shapes are still accepted, but responses include deprecation signals (`Deprecation: true`, `Warning` header, and response `warnings`).
2. **Phase 2 (planned)**: legacy keys/shapes are rejected with machine-readable `400 deprecated_config_format` errors.

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
