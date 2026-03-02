# Go Backend Development

## Overview
Developing the HTTP API server that validates Mermaid diagrams. This skill covers Go's `net/http` package, dependency injection patterns, request/response handling, and clean architecture principles.

## Learning Objectives
- [ ] Understand how to build HTTP handlers with Go's standard library
- [ ] Learn dependency injection patterns for testability
- [ ] Implement JSON marshaling and error handling
- [ ] Write clean, testable Go code with proper error propagation
- [ ] Use Go's context package for request lifecycle management

## Key Concepts

### HTTP Handler Pattern
In Go, handlers are functions that accept `(http.ResponseWriter, *http.Request)`. The merm8 API uses:
```go
type Handler struct {
    engine Engine
    parser Parser
}

func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
    // Parse request, validate, call engine, write response
}
```

### Dependency Injection
Rather than importing packages directly, handlers receive dependencies:
```go
// Good: injected parser allows mocking in tests
func NewHandler(engine Engine, parser Parser) *Handler { ... }

// Bad: direct import makes testing difficult
func NewHandler() *Handler {
    parser := NewRealParser()
}
```

### Request/Response Flow
- **Input**: JSON payload with Mermaid diagram code + optional config
- **Processing**: Parse → Validate → Collect issues → Marshal response
- **Output**: JSON with validation results, syntax errors, and metrics

### Error Handling
Go emphasizes explicit error handling:
```go
data, err := json.Marshal(response)
if err != nil {
    http.Error(w, "failed to marshal", http.StatusInternalServerError)
    return
}
```

## Relevant Code in merm8

| Component | Location | Responsibility |
|-----------|----------|-----------------|
| Main entry point | `cmd/server/main.go` | Server initialization, port binding |
| HTTP Handler | `internal/api/handler.go` | `/analyze` endpoint implementation |
| Handler Tests | `internal/api/handler_test.go` | Test examples with mock dependencies |
| Request Model | `internal/model/diagram.go` | Request/response struct definitions |

## Development Workflow

### Creating a New Endpoint
1. Define request/response structs with JSON tags
2. Implement handler function with `(w http.ResponseWriter, *http.Request)` signature
3. Parse/validate request body
4. Call business logic (engine, parser)
5. Marshal and write JSON response with appropriate HTTP status code
6. Write tests using mock dependencies

### Testing
Use interfaces to inject mock implementations:
```go
type mockParser struct{ /* stub */ }
func (m *mockParser) Parse(ctx context.Context, code string) (*Diagram, error) { ... }

handler := NewHandler(engine, mockParser)
// Now handler uses your mock instead of real parser
```

### Running Locally
```bash
go run ./cmd/server/main.go
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "graph TD\n  A-->B"}'
```

## Common Tasks

### Adding a New Field to Request
1. Update the request struct with JSON tag
2. Update handler parsing logic
3. Pass new field to engine
4. Update handler tests

### Adding Logging
Go's standard library (`log` package) is used. Consider using context for request tracing.

### Configuration via Environment Variables
Use `os.Getenv()` to read configuration (e.g., `PORT`, `PARSER_SCRIPT`).

## Resources & Best Practices
- Use `json.Unmarshal()` with error checking
- Leverage `httptest.NewRequest()` and `httptest.NewRecorder()` for handler tests
- Return appropriate HTTP status codes (400 for bad requests, 500 for server errors)
- Always wrap subprocess calls with `context.WithTimeout()`

## Prerequisites
- Basic Go syntax (functions, interfaces, error handling)
- Understanding of JSON marshaling
- HTTP protocol basics (status codes, headers, request/response)
