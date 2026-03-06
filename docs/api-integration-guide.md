# API Integration Guide

Complete guide to integrating merm8 into your applications with proper version negotiation, error handling, and configuration management.

---

## Quick Start

### Installation & Setup

```bash
# 1. Start merm8 server
docker run -p 8080:8080 -e PARSER_TIMEOUT_SECONDS=10 merm8:latest

# 2. Simple health check
curl http://localhost:8080/v1/healthz

# 3. First analyze request
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d @- << 'EOF'
{
  "code": "graph TD\n  A-->B\n  B-->C",
  "config": {
    "schema-version": "v1",
    "rules": {}
  }
}
EOF
```

---

## API Version Negotiation

### Supported Versions

| API Version | Released | Status | Support Until |
|---|---|---|---|
| 1.0 | 2026-03-06 | Current | 2026-12-31 (planned) |

### Version Negotiation Headers

Use `Accept-Version` to request a specific API version:

```bash
# Request with version negotiation
curl -H "Accept-Version: 1.0" http://localhost:8080/v1/analyze

# Response always includes Content-Version header
# Content-Version: 1.0
```

**In client code:**

```javascript
// JavaScript fetch
const response = await fetch('http://localhost:8080/v1/analyze', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Accept-Version': '1.0'
  },
  body: JSON.stringify(analyzePayload)
});

const apiVersion = response.headers.get('Content-Version');
console.log(`Using API version: ${apiVersion}`);
```

```python
# Python requests
import requests

response = requests.post(
  'http://localhost:8080/v1/analyze',
  headers={
    'Accept-Version': '1.0',
    'Content-Type': 'application/json'
  },
  json=analyze_payload
)

api_version = response.headers.get('Content-Version')
print(f"Using API version: {api_version}")
```

```go
// Go http
req, _ := http.NewRequest("POST", "http://localhost:8080/v1/analyze", body)
req.Header.Set("Accept-Version", "1.0")
req.Header.Set("Content-Type", "application/json")

resp, _ := client.Do(req)
apiVersion := resp.Header.Get("Content-Version")
log.Printf("Using API version: %s", apiVersion)
```

---

## Configuration Schema Versions

### Current Support

```bash
# Check config schema version support
curl http://localhost:8080/v1/config-versions | jq .

# Output:
# {
#   "current": "v1",
#   "supported": ["v1"],
#   "deprecations": [
#     {
#       "version": "unversioned",
#       "status": "deprecated",
#       "sunset-date": "2026-12-31T23:59:59Z",
#       ...
#     }
#   ],
#   "compatibility": {...}
# }
```

### Using Config Versions

**Recommended (v1 - canonical format):**

```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3
      }
    }
  }
}
```

**Legacy (still accepted, but deprecated):**

```json
{
  "code": "graph TD; A-->B",
  "config": {
    "max-fanout": {
      "limit": 3
    }
  }
}
```

---

## Rate Limiting

### Rate Limit Headers

Responses include rate limit information in headers:

```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 45
X-RateLimit-Reset: 1678123456
```

### Handling Rate Limits

```javascript
async function analyzeWithRateLimit(diagram, retries = 3) {
  for (let i = 0; i < retries; i++) {
    const response = await fetch('/v1/analyze', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(diagram)
    });

    if (response.status === 200) {
      return await response.json();
    }

    if (response.status === 429) {
      const resetTime = parseInt(response.headers.get('X-RateLimit-Reset'));
      const waitMs = (resetTime * 1000) - Date.now();
      
      console.log(`Rate limited. Waiting ${waitMs}ms...`);
      await new Promise(r => setTimeout(r, waitMs + 100));
      continue;
    }

    throw new Error(`Request failed: ${response.status}`);
  }
}
```

---

## Request/Response Examples

### 1. Valid Diagram, No Issues

**Request:**
```http
POST /v1/analyze HTTP/1.1
Content-Type: application/json
Accept-Version: 1.0

{
  "code": "graph TD\n  A[Start] --> B[End]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": { "limit": 2 }
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "123e4567-e89b-12d3-a456-426614174000",
  "timestamp": 1678123456789,
  "syntax-error": null,
  "issues": [],
  "suggestions": [],
  "meta": {
    "warnings": []
  },
  "metrics": {
    "parser-duration-ms": 45,
    "lint-duration-ms": 12,
    "total-duration-ms": 57
  }
}
```

### 2. Diagram with Parse Error

**Request:**
```http
POST /v1/analyze HTTP/1.1
Content-Type: application/json

{
  "code": "graph TD\n  A-->B\n  INVALID SYNTAX HERE",
  "config": {"schema-version": "v1", "rules": {}}
}
```

**Response (200 OK):**
```json
{
  "valid": false,
  "diagram-type": null,
  "lint-supported": false,
  "request-id": "abc12345...",
  "timestamp": 1678123456789,
  "syntax-error": {
    "line": 3,
    "column": 11,
    "message": "Unexpected token",
    "kind": "parse_error"
  },
  "issues": [],
  "metrics": {
    "parser-duration-ms": 23
  }
}
```

### 3. Diagram with Lint Issues

**Request:**
```http
POST /v1/analyze HTTP/1.1
Content-Type: application/json

{
  "code": "graph TD\n  A-->B;B-->C;A-->C;A-->D;A-->E;A-->F;A-->G",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": { "limit": 3, "severity": "error" }
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "def67890...",
  "timestamp": 1678123456789,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "core/max-fanout",
      "rule-type": "structural",
      "severity": "error",
      "message": "Node 'A' has 6 outgoing edges, exceeds limit of 3",
      "location": {
        "line": 1,
        "column": 15
      },
      "node-id": "A",
      "suppression-status": "active"
    }
  ],
  "metrics": {
    "parser-duration-ms": 18,
    "lint-duration-ms": 8,
    "total-duration-ms": 26
  }
}
```

### 4. Parser Timeout

**Request:**
```http
POST /v1/analyze HTTP/1.1
Content-Type: application/json

{
  "code": "graph TD; ... (very large diagram)",
  "parser": { "timeout_seconds": 2 }
}
```

**Response (503 Service Unavailable):**
```json
{
  "valid": false,
  "error": {
    "code": "parser_timeout",
    "message": "Parser timeout after 2 seconds",
    "details": "Diagram too complex for configured timeout"
  },
  "metrics": {
    "parser-duration-ms": 2001
  }
}
```

---

## SARIF Output Format

### Using SARIF Endpoint

```bash
curl -X POST http://localhost:8080/v1/analyze/sarif \
  -H "Content-Type: application/json" \
  -d @- << 'EOF'
{
  "code": "graph TD; A-->B; A-->C; A-->D; A-->E; A-->F; A-->G",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": { "limit": 3 }
    }
  }
}
EOF
```

### Integration with GitHub Security Tab

```yaml
# .github/workflows/scan.yml
name: Diagram Linting

on: [push, pull_request]

jobs:
  merm8:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Analyze diagrams
        run: |
          docker run -v ${{ github.workspace }}:/workspace \
            merm8:latest \
            /workspace/diagrams/*.mmd > sarif-output.json
      
      - name: Upload SARIF
        uses: github/codeql-action/upload-sarif@v2
        with:
          sarif_file: sarif-output.json
```

---

## Error Handling Strategies

### Retry Pattern with Exponential Backoff

```go
package main

import (
  "fmt"
  "math"
  "time"
)

func analyzeWithRetry(diagram []byte, maxRetries int) ([]byte, error) {
  var lastErr error
  
  for attempt := 0; attempt < maxRetries; attempt++ {
    resp, err := http.Post(
      "http://localhost:8080/v1/analyze",
      "application/json",
      bytes.NewReader(diagram),
    )
    
    if err == nil && resp.StatusCode == 200 {
      return io.ReadAll(resp.Body)
    }
    
    if err == nil && resp.StatusCode == 503 {
      // Server busy - use Retry-After if available
      retryAfter := resp.Header.Get("Retry-After")
      defaultWait := math.Pow(2, float64(attempt)) * 1000
      
      waitMs := int64(defaultWait)
      if retryAfter != "" {
        waitMs = parseInt(retryAfter) * 1000
      }
      
      time.Sleep(time.Duration(waitMs) * time.Millisecond)
      continue
    }
    
    lastErr = err
  }
  
  return nil, lastErr
}
```

### Circuit Breaker Pattern

```javascript
class Merm8Client {
  constructor(baseUrl, failureThreshold = 5) {
    this.baseUrl = baseUrl;
    this.failures = 0;
    this.failureThreshold = failureThreshold;
    this.circuitOpen = false;
    this.lastFailureTime = null;
  }

  async analyze(diagram) {
    if (this.circuitOpen) {
      // Allow retry after 30 seconds
      if (Date.now() - this.lastFailureTime > 30000) {
        this.circuitOpen = false;
        this.failures = 0;
      } else {
        throw new Error('Circuit breaker open');
      }
    }

    try {
      const response = await fetch(`${this.baseUrl}/v1/analyze`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(diagram)
      });

      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      
      this.failures = 0;
      return await response.json();
    } catch (error) {
      this.failures++;
      this.lastFailureTime = Date.now();
      
      if (this.failures >= this.failureThreshold) {
        this.circuitOpen = true;
      }
      
      throw error;
    }
  }
}
```

---

## Performance Tuning

### Client-side Caching

```javascript
// Cache analysis results for identical diagrams
const resultCache = new Map();

async function analyzeWithCache(diagram) {
  const cacheKey = JSON.stringify(diagram);
  
  if (resultCache.has(cacheKey)) {
    return resultCache.get(cacheKey);
  }
  
  const result = await fetch('/v1/analyze', {
    method: 'POST',
    body: JSON.stringify(diagram)
  }).then(r => r.json());
  
  resultCache.set(cacheKey, result);
  return result;
}
```

### Batch Processing

```bash
# Analyze multiple diagrams efficiently
for diagram in diagrams/*.mmd; do
  echo "Processing $diagram..."
  cat "$diagram" | jq -R -s '{code: .}' | \
    curl -X POST http://localhost:8080/v1/analyze \
         -H "Content-Type: application/json" \
         -d @- > "${diagram%.mmd}.json"
done
```

---

## Monitoring & Observability

### Health Checks

```bash
# Liveness probe (process health)
curl http://localhost:8080/v1/healthz

# Readiness probe (dependencies ready)
curl http://localhost:8080/v1/ready

# Extended health metrics
curl http://localhost:8080/v1/health/metrics
```

### Metrics Collection

```bash
# Prometheus metrics
curl http://localhost:8080/v1/metrics

# Expected metrics:
# - request_total{route, method, status}
# - request_duration_seconds{route, method}
# - analyze_requests_total{outcome}
# - parser_duration_seconds{outcome}
# - rule_execution_duration_seconds{rule_id}
```

---

## Best Practices

1. **Always use versioned config format** (schema-version: v1)
2. **Check Content-Version header** to verify API compatibility
3. **Implement retry logic** for transient failures (503, timeouts)
4. **Use SARIF format** for CI/CD and security tool integration
5. **Monitor rate limit headers** to adjust request frequency
6. **Cache identical requests** when possible
7. **Set appropriate parser timeouts** for your diagram complexity
8. **Use request-id header** for debugging and tracing

