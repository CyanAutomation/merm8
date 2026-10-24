---
name: NodeJsAndJavaScript
description: Maintain the Node.js Mermaid parser bridge used by the Go API and CLI.
---

# Node.js and JavaScript

## Inputs / Outputs / Non-goals

- Inputs: `parser-node/parse.ts`, parser AST modules, parser tests, and the Go parser protocol.
- Outputs: A parser bridge that preserves the documented JSON protocol and supported diagram data.
- Non-goals: Do not move Go rule logic into Node or assume the Worker uses this subprocess.

## Trigger conditions

Use this skill when changing parser behavior, Mermaid dependency use, AST extraction, or communication with Go.

## Mandatory rules

- Read `parser-node/parse.ts`, `parser-node/ast-types.ts`, and relevant AST modules before changing the output shape.
- The one-shot mode reads all stdin and emits one JSON result. `--worker` uses newline-delimited JSON envelopes with an `id` and `code`, and emits one correlated result per line.
- Keep stdout limited to protocol JSON. Send diagnostics to stderr.
- `mermaid.parse()` validates syntax; the bridge extracts its own normalized AST with `extractAST`. Do not treat Mermaid's parse return value as the project AST.
- Keep Mermaid dependencies aligned with `parser-node/package.json` and its lockfile. Do not upgrade to `latest` as part of unrelated parser work.

## Validation checklist

- [ ] Run `npm run test:parser` from the repository root.
- [ ] For parser runtime changes, install both dependency sets when needed: `npm ci` and `npm --prefix parser-node ci`.
- [ ] Smoke-test one-shot output with `printf 'flowchart TD\n  A --> B\n' | node --experimental-strip-types parser-node/parse.ts` and verify it is a single valid JSON result.
- [ ] For bridge changes, run `PARSER_SCRIPT=./parser-node/parse.ts go test ./internal/parser` or the relevant Go integration suite.

## Expected output format

- Summary: Describe parser or protocol behavior changed.
- Evidence: Report parser tests and any Go bridge checks run.
- Risks: Call out protocol, Mermaid-version, or AST compatibility effects.

## Failure/stop conditions

- Stop before changing the JSON protocol if Go consumers and fixtures have not been accounted for.
- If Mermaid behavior differs from the project AST contract, preserve the project contract or document a deliberate migration.
