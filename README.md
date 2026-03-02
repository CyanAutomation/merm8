A deterministic static analysis engine for Mermaid diagrams.

mermaid-lint validates Mermaid syntax using the official parser and applies rule-based structural analysis to enforce diagram quality and governance standards.

This project follows a hybrid architecture:
	•	Go HTTP API (primary service)
	•	Node.js subprocess for official Mermaid parsing
	•	Go-based rule engine
	•	Deterministic JSON responses
	•	No AI / no heuristics

⸻

✨ Why This Exists

Mermaid detects syntax errors, but it does not:
	•	Detect disconnected nodes
	•	Enforce naming conventions
	•	Flag architectural layering violations
	•	Detect excessive complexity
	•	Provide governance-level checks

mermaid-lint fills that gap.

Think of it as:

ESLint for Mermaid diagrams.

⸻

🏗 Architecture

This project uses Option B architecture:

Client
   ↓
Go HTTP API
   ↓
Node Mermaid Parser (subprocess)
   ↓
Structured Diagram Model (Go)
   ↓
Rule Engine
   ↓
JSON Response

Separation of Concerns
	•	Node → Syntax validation (official Mermaid parser)
	•	Go → Structural analysis and rule engine
	•	No rendering
	•	No AI

This keeps parsing authoritative while making linting fully owned and extensible.

⸻

📦 Project Structure

/cmd/server           → HTTP server entrypoint
/internal/api         → Request/response handling
/internal/parser      → Node subprocess bridge
/internal/model       → Internal diagram model
/internal/rules       → Rule definitions
/internal/engine      → Rule execution engine
/parser-node          → Node Mermaid parsing script
/Dockerfile
/docker-compose.yml
/README.md


⸻

🚀 MVP Scope

Supported:
	•	flowchart
	•	graph

Not supported (yet):
	•	sequenceDiagram
	•	classDiagram
	•	stateDiagram
	•	ER
	•	gantt
	•	journey
	•	pie
	•	etc.

⸻

📡 API

POST /analyze

Request

{
  "code": "graph TD\nA --> B\nC",
  "config": {
    "rules": {
      "max-fanout": { "level": "warn", "limit": 5 }
    }
  }
}

Successful Response

{
  "valid": true,
  "syntax_error": null,
  "issues": [
    {
      "rule": "no-disconnected-nodes",
      "severity": "error",
      "message": "Node C is declared but not connected",
      "line": 3,
      "column": 1
    }
  ],
  "metrics": {
    "node_count": 3,
    "edge_count": 1,
    "max_fanout": 1
  }
}

Syntax Error Response

{
  "valid": false,
  "syntax_error": {
    "message": "Parse error on line 2",
    "line": 2,
    "column": 8
  }
}


⸻

🧠 Rule Engine

Rules are deterministic and pluggable.

Each rule implements:

type Rule interface {
    ID() string
    Description() string
    Check(diagram Diagram, config RuleConfig) []Issue
}

Built-in Rules (MVP)
	•	no-disconnected-nodes (error)
	•	no-duplicate-node-ids (error)
	•	max-fanout (warn, configurable limit)

⸻

📊 Metrics

The engine also calculates:
	•	node_count
	•	edge_count
	•	max_fanout
	•	(future) depth
	•	(future) subgraph size
	•	(future) complexity score

⸻

🛡 Security Model

The Node parser:
	•	Runs as a subprocess
	•	Receives input via stdin
	•	Returns JSON via stdout
	•	Has a 2-second execution timeout
	•	Cannot write to disk
	•	Is stateless per request

The Go server:
	•	Uses exec.CommandContext with timeout
	•	Fails safely if parser crashes
	•	Returns HTTP 500 on unexpected parser failure

⸻

🐳 Docker

Build and run:

docker build -t mermaid-lint .
docker run -p 8080:8080 mermaid-lint

The Dockerfile:
	•	Builds Go binary (multi-stage)
	•	Installs Node runtime
	•	Installs Mermaid npm package
	•	Includes parser-node script
	•	Produces a lightweight runtime image

⸻

🧪 Example

curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{
        "code": "graph TD\nA --> B\nC"
      }'


⸻

🔌 Extending With New Rules

To add a rule:
	1.	Create file in /internal/rules
	2.	Implement the Rule interface
	3.	Register rule in the engine
	4.	Add config parsing support if needed

Example rule registration:

engine.RegisterRule(rules.NewNoDisconnectedNodes())

Rules should:
	•	Be deterministic
	•	Avoid side effects
	•	Return structured Issue objects
	•	Not modify diagram model

⸻

🧭 Roadmap

Phase 1 (MVP)
	•	Flowchart support
	•	Core rule engine
	•	Deterministic API
	•	Docker support

Phase 2
	•	Layering rules (architecture governance)
	•	Naming convention enforcement
	•	Complexity scoring
	•	CLI binary (mermaidlint)

Phase 3
	•	GitHub Action
	•	VSCode extension
	•	Rule packs
	•	Config file (.mermaidlintrc)

⸻

🎯 Design Principles
	•	Deterministic over intelligent
	•	Static analysis over suggestions
	•	Extensible rule system
	•	Clean separation of syntax and semantics
	•	CI-ready JSON outputs
	•	Zero AI dependencies

⸻

⚖ License

MIT

⸻

🧩 Future Vision

mermaid-lint is designed as the foundation for:
	•	Diagram governance
	•	Architecture validation
	•	CI enforcement
	•	Documentation quality control
	•	Mermaid-as-code standards

This is not a diagram renderer.

This is diagram static analysis tooling.
