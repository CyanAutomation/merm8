---
name: GraphTheoryAndAlgorithms
description: Implement deterministic, diagram-family-aware rules over merm8's parsed Mermaid model.
---

# Graph Theory and Algorithms

## Inputs / Outputs / Non-goals

- Inputs: `internal/model/diagram.go`, rule implementations and groups under `internal/rules`, and engine behavior.
- Outputs: A correct, deterministic rule implementation with focused tests.
- Non-goals: Do not assume every Mermaid diagram is a flowchart graph or change parsing/API contracts incidentally.

## Trigger conditions

Use this skill when adding or changing graph traversal, reachability, cycle, depth, connectivity, or fanout logic in Go rules.

## Mandatory rules

- Read the model first. `model.Diagram.Nodes` and `Edges` are slices; build indexes locally where useful rather than assuming a map-backed model.
- Implement the current `rules.Rule` interface: `ID() string` and `Run(d *model.Diagram, cfg rules.Config) []model.Issue`.
- Add `Families()` when a rule applies only to selected diagram families. Current groups are flowchart, sequence, class, ER, and state.
- Preserve deterministic issue ordering. Sort IDs or findings when their source is a Go map and output order is observable.
- Consider source-preserved fields such as `SourceNodeIDs`, `DisconnectedNodeIDs`, and `DuplicateNodeIDs`; Mermaid AST normalization can omit source distinctions.
- Add rules to the appropriate group in `internal/rules/rule_groups.go`. The engine uses `NewWithRules` for an explicit rule set; there is no `engine.AddRule` method.

## Validation checklist

- [ ] Run `go test ./internal/rules ./internal/engine`.
- [ ] Cover empty input, duplicate or missing endpoints, cycles, and boundary values that apply to the rule.
- [ ] Check deterministic results when maps or graph traversal are involved.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test ./...` when parsed diagram behavior changes.

## Expected output format

- Summary: Name the rule and its diagram family and behavior.
- Evidence: Report targeted tests and any broader Go checks run.
- Risks: Note complexity, behavior changes, or issue-order implications.

## Failure/stop conditions

- Stop before applying a flowchart-only assumption to another diagram family without checking that family's model and rules.
- If parser output cannot represent a needed graph property, identify that dependency rather than silently approximating it.
