---
name: CommandLineInterface
description: Maintain merm8-cli input, output, configuration, remote API, and exit-code behavior.
---

# Command-Line Interface

## Inputs / Outputs / Non-goals

- Inputs: `cmd/merm8-cli`, its tests, CLI examples in `README.md`, and the Go API contract for remote mode.
- Outputs: Predictable local or remote analysis with stable text/JSON output and exit behavior.
- Non-goals: Do not change API or parser behavior solely to work around a CLI issue without checking the owning component.

## Trigger conditions

Use this skill when changing CLI flags, file/stdin handling, config loading, text/JSON output, remote requests, or process exit codes.

## Mandatory rules

- Preserve these options and defaults unless the requested behavior changes: `--stdin`, `--format text|json`, `--url`, `--config`, `--fail-on-lint`, `--fail-on-syntax` (defaults true), and `--timeout` (HTTP timeout, defaults to 10 seconds).
- With no file arguments the CLI reads stdin; file paths can be analyzed together, and `--stdin` can combine stdin with file inputs.
- Local mode uses `PARSER_SCRIPT` when set and otherwise `./parser-node/parse.ts`; it requires the Node parser dependencies. Remote mode appends `/analyze` to `--url`, so use a version base such as `http://localhost:8080/v1` for the Go API.
- Preserve JSON output as an object containing a `results` array. Keep text output useful for terminals and standard output free of diagnostics where machine-readable output is requested.
- Preserve exit codes: `0` success, `1` configured syntax/lint findings, `2` argument/config/input/internal failure, and `3` transport failure.
- Keep configuration examples versioned and kebab-case. Prefer the documented `schema-version` plus `rules` format.

## Validation checklist

- [ ] Run `go test ./cmd/merm8-cli` for CLI behavior changes.
- [ ] Cover argument defaults, invalid flags, stdin/files, JSON output, and exit-code cases affected by the change.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test ./cmd/merm8-cli` when local parser integration changes.
- [ ] Check CLI examples in `README.md` when changing flags or remote URL behavior.

## Expected output format

- Summary: State the CLI behavior or option changed.
- Evidence: Report CLI unit/integration checks and representative command behavior.
- Risks: Note automation, output parsing, exit-code, or remote-API compatibility effects.

## Failure/stop conditions

- Stop before changing an existing flag's default or exit code without an explicit compatibility decision.
- If a remote endpoint or response shape differs by runtime, document which runtime the CLI supports and verify it against that runtime's API contract.
