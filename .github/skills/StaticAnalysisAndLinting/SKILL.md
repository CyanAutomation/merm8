---
name: StaticAnalysisAndLinting
description: Extend merm8's Go rule engine, rule configuration, and stable issue reporting.
---

# Static Analysis and Linting

## Inputs / Outputs / Non-goals

- Inputs: `internal/rules`, `internal/engine`, `internal/model`, rule configuration schema, and contract docs.
- Outputs: A rule or linting change consistent with the current registry, configuration, and issue contract.
- Non-goals: Do not design a generic plugin framework or alter API response fields without an explicit contract change.

## Trigger conditions

Use this skill when changing built-in rules, rule metadata, severity/config validation, suppressions, or issue reporting.

## Mandatory rules

- Rules implement `ID()` and `Run(d *model.Diagram, cfg rules.Config) []model.Issue`. Check the interface and existing rules before copying a pattern.
- Built-in metadata, allowed options, defaults, and constraints are maintained in `internal/rules/registry.go`; keep them aligned with runtime validation and generated API/schema artifacts.
- The versioned config shape is `{"schema-version":"v1","rules":{"max-fanout":{"limit":5,"severity":"warning"}}}`. Canonical keys use kebab-case.
- Issue severities are lowercase `error`, `warning`, or `info`. The public issue field is `rule-id`; preserve line/column, fingerprint, and context behavior where applicable.
- Built-in rules are composed in diagram-family groups. Custom rules can be supplied through `engine.NewWithRules`; do not imply that the runtime fetches or dynamically loads plugins.
- Keep findings actionable and deterministic, including stable IDs and output ordering.

## Validation checklist

- [ ] Run `go test ./internal/rules ./internal/engine`.
- [ ] Add or update tests for defaults, invalid configuration, severity, and suppression behavior affected by the change.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test ./...` for API-visible rule behavior.
- [ ] If rule metadata or the public config contract changes, update `API_GUIDE.md`, `openapi.yaml`, and generated artifacts as required.

## Expected output format

- Summary: Describe the rule or configuration behavior changed.
- Evidence: Report rule, engine, and contract checks run.
- Risks: Note compatibility, rule ordering, or false-positive/false-negative effects.

## Failure/stop conditions

- Stop before changing config keys or issue fields without checking the published schema and migration policy.
- If a rule needs parser data that is not available, identify the parser/model change separately.
