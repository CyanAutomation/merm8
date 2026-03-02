Docs available at: https://merm8-api-482194634678.europe-west1.run.app/docs

# merm8 — mermaid-lint

A **deterministic Mermaid static analysis engine** — no AI, no LLMs, pure static analysis.

This is intended to be a Mermaid linting service that:

1. Accepts Mermaid code via HTTP POST
2. Uses official Mermaid parser (Node) to validate syntax
3. Returns structured syntax errors if invalid
4. If valid:
   - Convert parsed AST into internal Go diagram model
   - Run deterministic rule engine
   - Return structured lint results

---

## Architecture

```
┌─────────────────────────────────────────────────┐
│                  HTTP Client                    │
│          POST /analyze  (JSON body)             │
└───────────────────┬─────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────────────┐
│              Go HTTP API  (:8080)               │
│                                                 │
│  internal/api  ── handler.go                    │
│      │                                          │
│      ├─► internal/parser  ── parser.go          │
│      │       │  exec.CommandContext (timeout 2s)│
│      │       │  stdin ──► node parse.mjs        │
│      │       │  stdout ◄── JSON AST / error     │
│      │       ▼                                  │
│      │   parser-node/parse.mjs  (Node.js)       │
│      │   [official mermaid.parse()]              │
│      │                                          │
│      └─► internal/engine ── engine.go           │
│               │  Runs all Rule implementations  │
│               ▼                                 │
│          internal/rules/                        │
│            no_duplicate_node_ids.go             │
│            no_disconnected_nodes.go             │
│            max-fanout.go                        │
│                                                 │
│  internal/model ── diagram.go (shared types)   │
└─────────────────────────────────────────────────┘
```

---

## Quick Start

### Local (requires Go 1.24+ and Node 20+)

```bash
# Install Node parser dependencies
cd parser-node && npm install && cd ..

# Build and run the Go server
go build -o mermaid-lint ./cmd/server
PARSER_SCRIPT=./parser-node/parse.mjs ./mermaid-lint
```

### Docker

```bash
docker compose up --build
```

The service listens on **port 8080**.

---

## API

### `POST /analyze`

**Request body**

```json
{
  "code": "graph TD\n  A-->B\n  B-->C",
  "config": {
    "rules": {
      "max-fanout": { "limit": 3 }
    }
  }
}
```

> `config` is optional. Both flat `{"max-fanout": {...}}` and nested `{"rules": {"max-fanout": {...}}}` formats are accepted.

> Request body size limit: **1 MiB**. Oversized payloads return `413` with JSON: `{"error":"request body exceeds 1 MiB limit"}`.

**Response — valid diagram**

```json
{
  "valid": true,
  "syntax_error": null,
  "issues": [],
  "metrics": {
    "node_count": 3,
    "edge_count": 2,
    "max-fanout": 1
  }
}
```

**Response — syntax error**

```json
{
  "valid": false,
  "syntax_error": {
    "message": "No diagram type detected...",
    "line": 0,
    "column": 0
  },
  "issues": []
}
```

### Example `curl` calls

```bash
# Valid flowchart
curl -s -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "graph TD\n  A-->B\n  B-->C"}'

# Invalid diagram
curl -s -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "this is not valid mermaid"}'

# Fan-out check with custom limit (warn severity)
curl -s -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A-->B\n  A-->C\n  A-->D",
    "config": {"rules": {"max-fanout": {"limit": 2}}}
  }'
```

### Interactive API Documentation

**Swagger UI** is available at `http://localhost:8080/docs` when the server is running. This provides:

- Interactive API explorer with schema documentation
- "Try it out" feature to test endpoints directly
- Request/response examples for each operation
- Full OpenAPI specification browsing

**OpenAPI Specification** is available at `http://localhost:8080/spec` in JSON format, useful for code generation and API tooling integration.

**For detailed usage instructions**, see [API_GUIDE.md](API_GUIDE.md) which covers:
- How to use the Swagger UI dashboard
- Direct HTTP request examples (curl, Python, JavaScript)
- Rule configuration guide
- Integration tips and troubleshooting

---

## Rule System

Rules live in `internal/rules/` and implement the `Rule` interface:

```go
type Rule interface {
    ID()  string
    Run(d *model.Diagram, cfg Config) []model.Issue
}
```

### Built-in Rules

| Rule ID                  | Severity | Description                                          |
|--------------------------|----------|------------------------------------------------------|
| `no-duplicate-node-ids`  | error    | Each node ID must be unique within the diagram.      |
| `no-disconnected-nodes`  | error    | Every node must participate in at least one edge.    |
| `max-fanout`             | warn     | No node may have more outgoing edges than the limit. |

Default `max-fanout` limit: **5**.

### Adding a New Rule

1. Create `internal/rules/my_rule.go`:

```go
package rules

import "github.com/CyanAutomation/merm8/internal/model"

type MyRule struct{}

func (r MyRule) ID() string { return "my-rule" }

func (r MyRule) Run(d *model.Diagram, cfg Config) []model.Issue {
    // your logic here
    return nil
}
```

2. Register it in `internal/engine/engine.go`:

```go
rules: []rules.Rule{
    rules.NoDuplicateNodeIDs{},
    rules.NoDisconnectedNodes{},
    rules.MaxFanout{},
    rules.MyRule{},    // ← add here
},
```

That's it — no registration maps, no config files.

---

## Project Structure

```
/cmd/server          Go entry point (main.go)
/internal/api        HTTP handler (POST /analyze)
/internal/parser     Go ↔ Node subprocess bridge
/internal/model      Shared diagram types (Diagram, Node, Edge, Issue)
/internal/rules      Rule interface + built-in rule implementations
/internal/engine     Runs all registered rules against a Diagram
/parser-node         Node.js Mermaid parser script + package.json
/Dockerfile          Multi-stage Docker build
/docker-compose.yml  Local development compose file
```

---

## Testing

### Prerequisites

- **Go** 1.24+
- **Node.js** 20+ and npm
- **curl** (for smoke tests)

### Running Unit Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests for a specific package
go test ./internal/api/...
go test ./internal/rules/...
go test ./internal/engine/...
```

### Running Parser Integration Tests

The parser integration tests require Node.js dependencies. Install them first:

```bash
cd parser-node && npm install && cd ..
```

Then run the parser tests:

```bash
# Run parser subprocess integration tests (requires parser-node npm install)
PARSER_SCRIPT=./parser-node/parse.mjs go test ./internal/parser/...
```

Or use the environment variable to point to the parser script:

```bash
export PARSER_SCRIPT=./parser-node/parse.mjs
go test ./internal/parser/...
```

### Smoke Tests

After building and starting the service, run the smoke test script:

```bash
# Start the service first:
go build -o mermaid-lint ./cmd/server
PARSER_SCRIPT=./parser-node/parse.mjs ./mermaid-lint

# In another terminal, run smoke tests:
bash smoke-test.sh
```

The smoke test validates:
- ✅ Valid diagram parsing with correct response structure
- ✅ Syntax error handling (200 response with error details)
- ✅ Missing 'code' field rejection
- ✅ Complex diagrams with multiple nodes/edges
- ✅ Custom rule configuration application
- ✅ Graceful handling of edge cases

### Test Coverage Summary

### Testing Architecture

The test suite uses two complementary approaches:

1. **Mock-based Handler Tests**: Fast, deterministic via `ParserInterface` dependency injection
   - Mock parser returns predefined diagrams without subprocess overhead
   - Tests handler business logic in isolation
   - Implementation: `ParserInterface` interface + `mockParser` type in handler_test.go
   - Examples: `TestAnalyze_ValidDiagram_SuccessPath`, `TestAnalyze_ConfigApplied_MaxFanout`, `TestAnalyze_MultipleRulesAggregate`

2. **Integration Parser Tests**: Test real Node.js subprocess
   - Require `PARSER_SCRIPT` env var to point to parse.mjs
   - Exercise actual Mermaid parsing with official parser
   - Some tests explicitly skip if Mermaid version lacks features (e.g., subgraphs, special characters)
   - Run with `-v` flag to see which tests were skipped and why
   - Examples: `TestParser_ValidFlowchart`, `TestParser_InvalidMermaid`, `TestParser_MultipleEdges`

**Skipped Tests (Expected Behavior):**
- `TestParser_WithSubgraphs` — Skips if Mermaid doesn't extract subgraphs from AST
- `TestParser_SpecialCharacters` — Skips if special character parsing isn't supported
- `TestParser_Timeout` — Skips with documentation: direct timeout testing isn't feasible; verified via code review

See test comments for rationale behind each skipped test.

| Component | Tests | Status |
|-----------|-------|--------|
| Rules (no-duplicate-node-ids) | ✅ | Complete |
| Rules (no-disconnected-nodes) | ✅ | Complete |
| Rules (max-fanout) | ✅ | Complete |
| Engine | ✅ | Complete |
| Handler (API) | ✅ | Enhanced |
| Parser (subprocess) | ✅ | Comprehensive |

---

## Environment Variables

| Variable        | Default                          | Description                         |
|-----------------|----------------------------------|-------------------------------------|
| `PORT`          | `8080`                           | TCP port the HTTP server listens on |
| `PARSER_SCRIPT` | `/app/parser-node/parse.mjs`     | Path to the Node.js parser script   |

---

## Future Roadmap

- [ ] Support additional diagram types (sequence, class, ER, state)
- [ ] `no-cycles` rule for flowcharts
- [ ] `max-depth` rule
- [ ] Per-rule suppression comments in diagram source
- [ ] Configurable rule severity overrides
- [ ] SARIF output format for CI integration
- [ ] Health-check endpoint (`GET /healthz`)
- [ ] Metrics endpoint (Prometheus-compatible)
