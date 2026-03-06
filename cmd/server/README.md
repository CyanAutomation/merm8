# cmd/server test suites

`cmd/server` keeps fast, deterministic tests in the default suite and isolates runtime-dependent coverage behind an integration build tag.

## Default CI (`go test ./cmd/...`)

Runs automatically in normal CI and local quick checks:

- `main_integration_test.go` (mock parser / in-memory server behavior)
- `contract_integration_test.go` (fixture parser contract checks without Node subprocess runtime)

These tests avoid external runtime dependencies and are expected to stay quick and deterministic.

## Extended integration CI

Run runtime-dependent parser integration tests (Node subprocess + controlled parser script) with:

```bash
go test -tags=integration ./cmd/server
```

This executes `runtime_integration_test.go`, which validates behaviors that depend on the parser runtime process and timeout/concurrency coordination.
