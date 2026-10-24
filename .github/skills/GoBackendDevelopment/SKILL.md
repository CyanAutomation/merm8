---
name: GoBackendDevelopment
description: Develop and validate the Go HTTP API and its parser, engine, and model integration.
---

# Go Backend Development

## Inputs / Outputs / Non-goals

- Inputs: `cmd/server`, `internal/api`, `internal/parser`, `internal/engine`, and `internal/model`.
- Outputs: Tested Go server behavior that follows the existing API and lifecycle contracts.
- Non-goals: Do not implement Mermaid parsing in Go or assume `internal/model/diagram.go` contains HTTP DTOs.

## Trigger conditions

Use this skill for Go server, handler, configuration, middleware, and backend integration changes.

## Mandatory rules

- Read the handler and `API_GUIDE.md` before changing HTTP behavior. API request/response types are defined in `internal/api/handler.go`; `internal/model/diagram.go` models parsed diagrams and issues.
- Construct the server with the existing dependency pattern (`api.NewHandler(parser, engine.New())`). Keep parser, engine, telemetry, and configuration boundaries intact.
- Preserve versioned routes, kebab-case JSON names, structured error responses, and backwards compatibility. Use the REST API skill for status-code and schema guidance.
- For local server runs, set `PARSER_SCRIPT=./parser-node/parse.ts`; the server's default `/app/parser-node/parse.ts` path is for deployments that provide that location.
- Pass request cancellation and bounded parser settings through the existing parser interfaces. Do not add direct `os/exec` management to handlers.
- Use structured logging and existing middleware rather than adding ad hoc logging or duplicate request controls.

## Validation checklist

- [ ] Run `go test ./internal/api ./cmd/server` for handler/server changes.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test ./...` when parser-backed behavior changes.
- [ ] Run `make test-contract` for API contract changes.
- [ ] For runtime-dependent cases, install parser dependencies and run `PARSER_SCRIPT=./parser-node/parse.ts go test -tags=integration ./cmd/server`.

## Expected output format

- Summary: Describe server behavior changed.
- Evidence: List Go unit, contract, and integration checks run.
- Risks: Note API compatibility, parser runtime, or deployment configuration effects.

## Failure/stop conditions

- Stop before changing a public schema or deprecated route without an explicit compatibility decision.
- If a check requires unavailable Node/parser dependencies, report that limitation and complete independent checks.
