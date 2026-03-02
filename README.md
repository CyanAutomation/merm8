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
│            max_fanout.go                        │
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

**Response — valid diagram**

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

# Fan-out warning with custom limit
curl -s -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "graph TD\n  A-->B\n  A-->C\n  A-->D",
    "config": {"rules": {"max-fanout": {"limit": 2}}}
  }'
```

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
