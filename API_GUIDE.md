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

- **Left sidebar** — List of all available endpoints (currently `/analyze`, `/spec`, `/docs`)
- **Main panel** — Detailed endpoint documentation with parameters and response schemas
- **Try it out button** — Execute requests directly from the browser
- **Example requests** — Pre-filled request templates for common scenarios

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

**Diagram with high fan-out (warning):**
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
        "limit": 2
      }
    }
  }
}
```

**Supported rule configurations:**

- `max-fanout` — Set maximum outgoing edges per node
  ```json
  "max-fanout": { "limit": 3 }
  ```

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
  "syntax_error": null,
  "issues": [],
  "metrics": {
    "node_count": 3,
    "edge_count": 2,
    "max_fanout": 1
  }
}
```

**Response with lint issues:**
```json
{
  "valid": true,
  "syntax_error": null,
  "issues": [
    {
      "rule_id": "no-disconnected-nodes",
      "severity": "error",
      "message": "Node 'Isolated' is not connected to the graph"
    }
  ],
  "metrics": {
    "node_count": 3,
    "edge_count": 2,
    "max_fanout": 1
  }
}
```

**Syntax error response:**
```json
{
  "valid": false,
  "syntax_error": {
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
- `parser_timeout` — parser timed out (HTTP 500)
- `internal_error` — unexpected internal parser/service failure (HTTP 500)

### Response Fields Explained

- **`valid`** — Boolean indicating if the Mermaid syntax is syntactically correct
- **`syntax_error`** — Object with parsing error details (only present if `valid` is false)
  - `message` — Human-readable error description
  - `line` — 1-based line number where error occurred
  - `column` — 0-based column number where error occurred
- **`issues`** — Array of lint rule violations found (empty if no issues)
  - `rule_id` — The lint rule that triggered
  - `severity` — One of: `error`, `warn`, `info`
  - `message` — Description of the issue
  - `line` / `column` — Optional location in the diagram code (omitted when unknown)
- **`issues`** can include findings both with source locations (`line`/`column`) and without them when exact positions are unavailable.
  - `line` / `column` — Location in the diagram code
  - **Ordering guarantee** — Issues are deterministically sorted before returning: by severity priority (`error` → `warn` → `info`), then `rule_id`, then `line`, then `column`, then `message`. If two rules produce the exact same issue signature, duplicates are removed.
- **`metrics`** — Statistics about the diagram structure
  - `node_count` — Total nodes in the diagram
  - `edge_count` — Total connections/edges
  - `max_fanout` — Maximum outgoing edges from any single node

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
    print(f"  - {issue['rule_id']}: {issue['message']}")
```

#### JavaScript / Node.js

```javascript
const fetch = require('node-fetch');

const payload = {
  code: "graph TD\n  A --> B\n  A --> C\n  A --> D",
  config: {
    rules: {
      "max-fanout": { limit: 2 }
    }
  }
};

fetch('http://localhost:8080/analyze', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(payload)
})
  .then(res => res.json())
  .then(data => {
    console.log(`Valid: ${data.valid}`);
    console.log(`Issues: ${data.issues.length}`);
    data.issues.forEach(issue => {
      console.log(`  - ${issue.rule_id}: ${issue.message}`);
    });
  });
```

---

## API Endpoints Reference

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
    "rule_id": "no-duplicate-node-ids",
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
    "rule_id": "no-disconnected-nodes",
    "severity": "error",
    "message": "Node 'isolated' is not connected"
  }
  ```

#### `max-fanout`
- **Severity:** warn
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
    "rule_id": "max-fanout",
    "severity": "warn",
    "message": "Node 'A' has fanout of 6, exceeds limit of 5"
  }
  ```

### Configuration Formats

Both flat and nested configuration formats are accepted:

**Flat format:**
```json
{
  "code": "...",
  "config": {
    "max-fanout": { "limit": 3 }
  }
}
```

**Nested format:**
```json
{
  "code": "...",
  "config": {
    "rules": {
      "max-fanout": { "limit": 3 }
    }
  }
}
```

Both will work identically!

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
  "syntax_error": null,
  "issues": [],
  "metrics": {
    "node_count": 3,
    "edge_count": 2,
    "max_fanout": 1
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
  "syntax_error": null,
  "issues": [
    {
      "rule_id": "max-fanout",
      "severity": "warn",
      "message": "Node 'A' has fanout of 6, exceeds limit of 4",
      "line": 2,
      "column": 2
    }
  ],
  "metrics": {
    "node_count": 7,
    "edge_count": 6,
    "max_fanout": 6
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
  "syntax_error": null,
  "issues": [
    {
      "rule_id": "no-disconnected-nodes",
      "severity": "error",
      "message": "Node 'D' is not connected to the graph"
    }
  ],
  "metrics": {
    "node_count": 4,
    "edge_count": 2,
    "max_fanout": 1
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
  "syntax_error": {
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
