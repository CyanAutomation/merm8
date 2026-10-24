---
name: SystemsProgrammingAndProcessManagement
description: Safely manage the Go parser's Node.js processes, worker protocol, limits, and shutdown.
---

# Systems Programming and Process Management

## Inputs / Outputs / Non-goals

- Inputs: `internal/parser`, `parser-node/parse.ts`, parser configuration, and server shutdown flow.
- Outputs: Reliable process lifecycle, bounded execution, and preserved request/response behavior.
- Non-goals: Do not add a second parser process manager to API handlers or bypass the parser package.

## Trigger conditions

Use this skill when changing subprocess invocation, worker pools, parser timeouts, memory limits, cancellation, or shutdown.

## Mandatory rules

- Treat `internal/parser` as the owner of process lifecycle and protocol. The default `PARSER_MODE=pool` reuses long-lived workers; `subprocess` selects one-parse-per-process behavior.
- In worker mode, requests and replies are newline-delimited JSON envelopes. Preserve request IDs, serialize access to each worker, and recycle unhealthy or timed-out workers.
- The parser timeout defaults to five seconds and is configured with `PARSER_TIMEOUT_SECONDS`; do not copy a hard-coded timeout into callers. Respect the existing bounded parser configuration and memory limits.
- For a one-shot `os/exec` call, prefer `cmd.Stdin` plus captured `cmd.Stdout`/`cmd.Stderr` and `cmd.Run()`. If using `StdoutPipe` or `StdinPipe`, check every error and pair `Start`, stream consumption, and `Wait` correctly; do not call `Run` with `StdoutPipe`.
- Close the parser during server shutdown and make cancellation reach the active parse operation.

## Validation checklist

- [ ] Run `go test ./internal/parser` for parser lifecycle or protocol changes.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test -tags=integration ./cmd/server` for runtime integration changes; install `parser-node` dependencies if needed.
- [ ] Run `make test-contract` when API-visible parser behavior changes.
- [ ] Exercise timeout, malformed output, worker replacement, and shutdown cases when those paths change.

## Expected output format

- Summary: State which process mode, lifecycle, or limit changed.
- Evidence: Report targeted parser and integration checks.
- Risks: Describe effects on concurrency, memory, timeouts, or protocol compatibility.

## Failure/stop conditions

- Stop before changing the worker wire protocol without updating the Go and Node sides together.
- If runtime checks cannot run, report the missing runtime or dependency and the remaining verification.
