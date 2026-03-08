# AGENTS.md

## Repo overview

This repository is a **Go API + CLI project** with a **Node-based Mermaid parser**.

Key areas:
- API server: `cmd/server`, `internal/api`
- CLI: `cmd/merm8-cli`
- Parser bridge: `internal/parser` and `parser-node/parse.mjs`
- Rules engine: `internal/engine`, `internal/rules`
- Docs: `docs/`, `API_GUIDE.md`, `IMPLEMENTATION_GUIDE.md`

## Build/test commands

Use the canonical project commands from the `Makefile` and README:
- `make vet`
- `make lint`
- `make test-contract`
- `go test ./...`
- `go test -tags=integration ./cmd/server`
- `make benchmark`

Parser-related and integration tests depend on the Node runtime and the `parser-node` bridge; ensure that environment is available when validating parser behavior.

## Change hygiene

- For user-visible behavior changes, update `CHANGELOG.md` under `## [Unreleased]` (per `CONTRIBUTING.md`).
- Update docs in `docs/` and/or `API_GUIDE.md` whenever API contracts, configuration, or examples change.
- Keep canonical API/config naming in **kebab-case** (per README and API docs).

## Code conventions

- Standard Go flow: run `gofmt`, `go vet`, and `go mod tidy` as appropriate.
- Keep tests deterministic where possible; runtime-dependent parser behavior should be covered in integration-tagged coverage (see `cmd/server/README.md` guidance).
- Preserve versioned endpoint preference (`/v1/...`) and legacy alias/deprecation behavior when touching HTTP routes.

## Safety guidance

- Avoid broad refactors unrelated to the task.
- Prefer minimal diffs.
- Keep API response fields and backward compatibility consistent unless the contract change is explicit.
