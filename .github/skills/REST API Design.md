# REST API Design

## Overview
Building clean, maintainable HTTP APIs. This skill covers RESTful principles, proper HTTP semantics, request/response design, error handling, and versioning.

## Learning Objectives
- [ ] Understand REST principles and conventions
- [ ] Design clear, predictable endpoints
- [ ] Use correct HTTP methods and status codes
- [ ] Create consistent request/response formats
- [ ] Handle errors with structured responses

## Key Concepts

### REST Principles
1. **Resource-Oriented**: APIs expose nouns, not verbs
2. **Standard Methods**: GET, POST, PUT, DELETE
3. **Statelessness**: Each request contains all context
4. **Uniform Interface**: Consistent patterns across endpoints

### Current Endpoint
```http
POST /analyze
Content-Type: application/json

{
  "code": "graph TD\n  A-->B",
  "config": { "rules": { "max-fanout": { "limit": 5 } } }
}
```

### Response Example
```json
{
  "valid": true,
  "syntax_error": null,
  "issues": [],
  "metrics": {
    "node_count": 2,
    "edge_count": 1,
    "max_fanout": 1
  }
}
```

### Status Codes
| Code | Meaning | Use Case |
|------|---------|----------|
| 200 | OK | Valid diagram |
| 400 | Bad Request | Invalid JSON |
| 422 | Unprocessable Entity | Validation failure |
| 500 | Internal Server Error | Parser crash |
| 503 | Service Unavailable | Timeout |

### Error Responses
```json
{
  "error": {
    "type": "validation_error",
    "message": "Invalid Mermaid syntax",
    "details": { "line": 2, "column": 5 }
  }
}
```

## Relevant Code in merm8

| Component | Location | Purpose |
|-----------|----------|---------|
| Analyze handler | internal/api/handler.go | `/analyze` logic |
| Request/response | internal/model/diagram.go | Struct definitions |
| HTTP server | cmd/server/main.go | Routes + server setup |
| Handler tests | internal/api/handler_test.go | Contract verification |

## Development Workflow

### Request/Response Contracts
```go
type AnalyzeRequest struct {
    Code   string            `json:"code"`
    Config *ValidationConfig `json:"config,omitempty"`
}

type AnalyzeResponse struct {
    Valid       bool           `json:"valid"`
    SyntaxError *SyntaxError   `json:"syntax_error"`
    Issues      []Issue        `json:"issues"`
    Metrics     DiagramMetrics `json:"metrics"`
}
```

### Testing the Endpoint
```go
testReq := httptest.NewRequest("POST", "/analyze", strings.NewReader("{\"code\":\"graph TD\\n  A-->B\"}"))
recorder := httptest.NewRecorder()
handler.Analyze(recorder, testReq)

if recorder.Code != http.StatusOK {
    t.Fatalf("expected 200")
}
```

## Common Tasks

### Adding a Field
1. Update `AnalyzeRequest`
2. Parse field in handler
3. Use in engine
4. Update tests

### Handling Errors
Return `http.Error()` or structured JSON with details.

### Versioning Strategies
- **Path**: `/v1/analyze`
- **Header**: `Accept: application/vnd.merm8.v1+json`
- **Query**: `?version=1`

## Resources & Best Practices
- Use consistent naming (kebab-case paths, snake_case JSON)
- Document fields (README or OpenAPI)
- Validate input early (400 vs 422 distinction)
- Maintain backward compatibility when extending schema

## Prerequisites
- HTTP fundamentals (methods/status codes)
- JSON structure and Go tags
- Familiarity with `net/http` package

## Related Skills
- Go Backend Development for implementation details
- Static Analysis & Linting for issue reporting