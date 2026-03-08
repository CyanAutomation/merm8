---
name: GraphTheoryAndAlgorithms
description: Analyze and implement graph-based validation rules for Mermaid diagrams in Go.
---

# Skill Instructions

## Inputs / Outputs / Non-goals

- Inputs: Go source files for rules, engine, and model; test files; Mermaid diagram ASTs.
- Outputs: Efficient, correct rule implementations for graph validation; test coverage for new rules.
- Non-goals: Do not change diagram parsing logic or API handler code unless required for rule integration.

## Trigger conditions

Use this skill when prompts include or imply:

- "Add/modify a lint rule for diagrams"
- "Implement graph validation logic"
- "Optimize or test rule performance"

## Mandatory rules

- Follow domain constraints and avoid silent public API/schema changes.
- Keep changes scoped and deterministic.
- Record assumptions and unresolved ambiguities.

## Validation checklist

- [ ] Required commands/checks were run (go test, go build).
- [ ] Relevant tests were updated/executed.
- [ ] Risk/impact was documented.

## Expected output format

- Summary: What changed in rule logic and why.
- Evidence: Test results, code diffs, performance notes.
- Risks: Known risks and mitigations (e.g., missed edge cases, performance regressions).

## Failure/stop conditions

- Stop if requirements are ambiguous in a way that can cause breaking changes.
- Stop if required validation cannot be performed and report the blocker.

---

# Graph Theory & Algorithms

## Overview

Understanding the graph-based validation logic that powers merm8's linting rules. This skill involves analyzing diagram structure, detecting patterns, and implementing efficient algorithms for validation.

## Learning Objectives

- [ ] Understand directed graph representation (nodes and edges)
- [ ] Implement graph traversal algorithms (DFS, BFS)
- [ ] Detect graph properties (connectivity, fanout, cycles)
- [ ] Design extensible validation rules as graph analysis patterns
- [ ] Optimize rule performance for large diagrams

## Key Concepts

### Graph Representation

Mermaid diagrams are directed graphs:

```go
type Diagram struct {
    Nodes map[string]*Node
    Edges []*Edge
}

type Edge struct {
    From string
    To   string
    Type string
}
```

### Graph Operations

- **Fanout**: Count edges leaving a node (max-fanout rule uses this).
- **Connectivity**: Determine reachable nodes to spot disconnected nodes.
- **Duplication**: Use a set to detect repeated node IDs that break structure.

### Rule Implementation Template

```go
type Rule interface {
    ID() string
    Validate(diagram *Diagram) []Issue
}

type MaxFanoutRule struct {
    limit int
}

func (r *MaxFanoutRule) Validate(d *Diagram) []Issue {
    // Count out-edges per node and report violations
}
```

## Relevant Code in merm8

| Component          | Location                                | Purpose                       |
| ------------------ | --------------------------------------- | ----------------------------- |
| Rule interface     | internal/rules/rule.go                  | Base `Rule` definition        |
| Max fanout         | internal/rules/max_fanout.go            | Enforces outgoing edge limits |
| Disconnected nodes | internal/rules/no_disconnected_nodes.go | Detects isolated nodes        |
| Duplicate IDs      | internal/rules/no_duplicate_node_ids.go | Flag repeated node IDs        |
| Engine             | internal/engine/engine.go               | Runs registered rules         |
| Tests              | internal/rules/rules_test.go            | Examples of rule validation   |

## Development Workflow

### Building an Adjacency List

```go
outgoing := make(map[string][]string)
for _, edge := range diagram.Edges {
    outgoing[edge.From] = append(outgoing[edge.From], edge.To)
}
```

### Detecting Disconnected Nodes

```go
reachable := bfs(diagram, startNode)
for nodeID := range diagram.Nodes {
    if !reachable[nodeID] {
        // Report disconnected node
    }
}
```

### Fanout Calculation

```go
fanout := len(outgoing[nodeID])
if fanout > limit {
    // Report violation
}
```

### Adding a New Rule

1. Create `internal/rules/<rule>.go`
2. Implement the `Rule` interface
3. Return `[]Issue` with violations
4. Register rule in engine
5. Add tests to `rules_test.go`

## Common Tasks

### Configurable Rules

```go
type MaxFanoutRule struct {
    limit int
}

func NewMaxFanoutRule(config map[string]interface{}) *MaxFanoutRule {
    limit := config["limit"].(int)
    return &MaxFanoutRule{limit: limit}
}
```

### Optimizing Performance

- Use maps for constant-time node lookups
- Cache adjacency lists instead of recomputing per rule
- Avoid redundant traversals

### Testing Rules

```go
diagram := &Diagram{
    Nodes: map[string]*Node{
        "A": {ID: "A"},
        "B": {ID: "B"},
    },
    Edges: []*Edge{{From: "A", To: "B"}},
}
issues := rule.Validate(diagram)
```

## Resources & Best Practices

- **Adjacency Lists**: Use maps of slices for graph representation
- **Early Exit**: Stop traversal once violation is found if order isn’t important
- **Clear Messages**: Include node IDs in issue text
- **Configuration**: Allow thresholds to be overridden via config

## Prerequisites

- Basic data structures (maps, slices)
- Graph concepts (nodes, edges, directed graphs)
- Algorithmic complexity awareness

## Related Skills

- Systems Programming & Process Management for parser workflow
- Static Analysis & Linting for rule architecture
