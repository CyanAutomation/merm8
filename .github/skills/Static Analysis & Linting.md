# Static Analysis & Linting

## Overview
Understanding principles of static code analysis and linting frameworks. This skill covers how to design composable validation rules, report issues with context, and build extensible checker systems.

## Learning Objectives
- [ ] Understand linter architecture and rule composition
- [ ] Design validation rules with clear severity levels
- [ ] Implement configuration systems for rule customization
- [ ] Provide actionable error messages with line/column information
- [ ] Build extensible frameworks that support plugins

## Key Concepts

### Linter Design Pattern
A linter is a framework that:
1. **Parses** input into a structured representation
2. **Applies** rules (validators) to the structure
3. **Collects** violations (issues) with metadata
4. **Reports** issues with location and severity

```
Input Code → Parser → AST → Rule Engine → Issues → Report
```

### Rule Registry
Rules are stored in a registry and executed in sequence:
```go
type Engine struct {
    rules []Rule
}

func (e *Engine) Validate(diagram *Diagram) []Issue {
    var allIssues []Issue
    for _, rule := range e.rules {
        allIssues = append(allIssues, rule.Validate(diagram)...)
    }
    return allIssues
}
```

### Issue Severity Levels
- **ERROR**: Diagram must be fixed
- **WARNING**: Design smell
- **INFO**: Informative guidance

### Configuration System
```json
{
  "rules": {
    "no-disconnected-nodes": "error",
    "max-fanout": {
      "severity": "warn",
      "limit": 5
    }
  }
}
```

### Error Metadata
Issues include rule ID, message, severity, and location:
```json
{
  "rule": "max-fanout",
  "message": "Node 'handler' has too many outgoing edges",
  "severity": "warning",
  "node_id": "handler",
  "line": 5,
  "column": 3
}
```

## Relevant Code in merm8

| Component | Location | Purpose |
|-----------|----------|---------|
| Rule interface | internal/rules/rule.go | Base definition for rules |
| Engine | internal/engine/engine.go | Orchestrates rule execution |
| Issue model | internal/model/diagram.go | Violation metadata |
| Rules | internal/rules/ | Concrete rule implementations |

## Development Workflow

### Strategy Pattern
Each rule implements `Rule`:
```go
type Rule interface {
    ID() string
    Validate(diagram *Diagram) []Issue
}
```

### Extensibility
```go
type NoSelfLoopsRule struct{}
func (r *NoSelfLoopsRule) ID() string { return "no-self-loops" }
func (r *NoSelfLoopsRule) Validate(d *Diagram) []Issue { /* ... */ }

engine.AddRule(&NoSelfLoopsRule{})
```

## Common Tasks

### Implementing a New Rule
1. Add `internal/rules/<rule>.go`
2. Implement `Rule` interface (`ID`, `Validate`)
3. Return `[]Issue` describing violations
4. Register rule in engine
5. Add tests

### Configurable Rules
```go
type MaxFanoutRule struct { limit int }
func NewMaxFanoutRule(config map[string]interface{}) *MaxFanoutRule {
    limit := config["limit"].(int)
    return &MaxFanoutRule{limit: limit}
}
```

### Reporting All Issues
Avoid early exit; collect all relevant violations for full feedback.

## Resources & Best Practices
- **Deterministic Output**: Keep ordering consistent
- **Performance**: Keep rules linear with diagram size
- **Clear Messages**: Explain why the issue matters
- **Configuration**: Let users tune thresholds

## Prerequisites
- Understanding of validation logic
- Basic algorithms and complexity
- Familiarity with JSON and HTTP APIs

## Related Skills
- Graph Theory & Algorithms for rule logic
- REST API Design for API contract