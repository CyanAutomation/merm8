[![CI](https://github.com/CyanAutomation/merm8/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/CyanAutomation/merm8/actions/workflows/ci.yml) [![OpenAPI Sync](https://github.com/CyanAutomation/merm8/actions/workflows/openapi-sync.yml/badge.svg?branch=main)](https://github.com/CyanAutomation/merm8/actions/workflows/openapi-sync.yml) [![Worker Deploy](https://github.com/CyanAutomation/merm8/actions/workflows/deploy-cloudflare.yml/badge.svg?branch=main)](https://github.com/CyanAutomation/merm8/actions/workflows/deploy-cloudflare.yml)

# merm8

merm8 parses Mermaid diagrams and reports syntax errors, structural metrics, and deterministic lint findings. It is for people authoring diagrams, teams adding diagram checks to CI, and developers integrating or operating the analysis service.

The repository contains a Go HTTP API and CLI, a Node.js Mermaid parser used by the Go programs, and a Cloudflare Worker implementation. The Worker has its own smaller, Worker-safe contract. Its optional semantic review is a separate feature; regular lint analysis does not call a language model.

The Go API and CLI run as local Go programs. The Worker can run locally with Wrangler and is the implementation deployed to Cloudflare; it does not expose every Go API feature. Check a deployed Worker's `/v1/spec` before relying on endpoint parity.

## Choose a path

| Goal | Start here |
| --- | --- |
| Run the Go API locally | [Local API quick-start](#local-api-quick-start) |
| Lint diagrams from a terminal or CI | [CLI quick-start](#cli-quick-start) |
| Run or deploy the Cloudflare Worker | [Worker quick-start](#cloudflare-worker-quick-start) |
| Integrate an API client | [API Integration Guide](docs/api-integration-guide.md) |
| Find examples, migration, and operations docs | [Documentation Index](docs/INDEX.md) |

## Local API quick-start

**Requirements:** Go 1.24+, Node.js 24+, and npm.

From the repository root, install the parser dependencies and build the server:

```bash
npm --prefix parser-node ci
go build -o mermaid-lint ./cmd/server
PARSER_SCRIPT=./parser-node/parse.ts ./mermaid-lint
```

The server listens on port `8080` by default. In another terminal, check it and analyze a diagram:

```bash
curl -sS http://localhost:8080/v1/healthz

curl -sS http://localhost:8080/v1/analyze \
  -H 'Content-Type: application/json' \
  -d '{"code":"flowchart TD\n  A[Start] --> B[Finish]"}'
```

The health response is `{"status":"ok"}`. The analysis response includes `valid`, `diagram-type`, `lint-supported`, `issues`, and `metrics`. See the [API reference](API_GUIDE.md) for request options, errors, configuration, and response fields.

The local parser script path is required because the server's default path (`/app/parser-node/parse.ts`) is for its container layout. See [Troubleshooting & FAQ](docs/troubleshooting-faq.md) if startup or parsing fails.

## CLI quick-start

The CLI can parse and lint locally, or send requests to a running API. Build it after installing the parser dependencies:

```bash
npm --prefix parser-node ci
mkdir -p dist
go build -o dist/merm8-cli ./cmd/merm8-cli
```

Analyze Mermaid from standard input in local mode:

```bash
printf 'flowchart TD\n  A[Start] --> B[Finish]\n' \
  | PARSER_SCRIPT=./parser-node/parse.ts ./dist/merm8-cli --stdin
```

For this input, the CLI prints:

```text
stdin: ok
```

Text output reports `ok` when there are no findings. Add `--format json` for structured output, `--config path/to/config.json` for rule settings, or `--url http://localhost:8080` to use the Go API. `--fail-on-lint` makes lint findings return exit code 1 for CI; syntax errors fail by default. See [CLI options and exit codes](#cli-options) below.

## Cloudflare Worker quick-start

The Worker is a smaller implementation than the Go API. It supports flowchart, sequence, class, ER, and state diagrams; consult its [Worker API reference](docs/worker-api.md) for the current rules and endpoint differences.

To run it locally:

```bash
npm ci
npm run dev
```

Wrangler serves the Worker at `http://localhost:8787`. The deterministic analysis endpoint is public and does not require an API key:

```bash
curl -sS http://localhost:8787/v1/analyze \
  -H 'Content-Type: application/json' \
  -d '{"code":"flowchart TD\n  A[Start] --> B[Finish]"}'
```

For deployment, authenticate Wrangler with an account that can deploy this Worker, then run:

```bash
npm ci
npx wrangler login
npm run deploy
```

`API_KEY` is required only for the Worker’s `/mcp` and `/v1/semantic-review` endpoints. Configure it as a Wrangler secret if you use either endpoint:

```bash
npx wrangler secret put API_KEY
```

Semantic review is optional, separate from deterministic analysis, and also requires a server-side OpenRouter credential:

```bash
npx wrangler secret put OPENROUTER_API_KEY
```

The Worker does not support an `MCP_ALLOWED_HOSTNAMES` setting. Its deterministic analysis endpoint is publicly callable, so configure Cloudflare rate limiting/WAF controls for public deployments. See [Worker operations and security notes](docs/worker-api.md#request-limits).

After deployment, use the Worker URL printed by Wrangler:

```bash
WORKER_URL=https://your-worker.example.workers.dev
curl -sS "$WORKER_URL/v1/spec"
curl -sS "$WORKER_URL/v1/analyze" \
  -H 'Content-Type: application/json' \
  -d '{"code":"flowchart TD\n  A[Start] --> B[Finish]"}'
```

Use `GET /v1/spec`, `/v1/diagram-types`, and `/v1/rules` to inspect the deployed Worker contract. The deployment SHA is returned in the `X-Merm8-Build` header.

## Usage

### API

The canonical Go API analysis endpoint is `POST /v1/analyze` and accepts a JSON object with a Mermaid `code` string. Optional lint configuration uses the versioned kebab-case form:

```json
{
  "code": "flowchart TD\n  A --> B\n  A --> C",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": { "enabled": true, "limit": 1 }
    }
  }
}
```

Other useful Go API endpoints include `/v1/healthz`, `/v1/ready`, `/v1/rules`, `/v1/rules/schema`, `/v1/analyze/sarif`, and `/v1/docs`. The Worker does not implement every Go server endpoint; see [Worker API differences](docs/worker-api.md#important-differences-from-the-go-server). The full contract and examples are in [API_GUIDE.md](API_GUIDE.md) and the [OpenAPI spec](openapi.yaml).

### CLI options

- Inputs: file paths, `--stdin`, or standard input when no files are given.
- Modes: local analysis by default; `--url <server>` uses the API.
- Output: `--format text` (default) or `--format json`.
- Config: `--config <file>`.
- CI behavior: `--fail-on-syntax` defaults to true; `--fail-on-lint` defaults to false.
- Exit codes: `0` success, `1` configured syntax/lint findings, `2` local/config/input failure, `3` server transport failure.

Example config and migration details are in [Configuration Examples](docs/examples/config-examples.md) and the [Migration Guide](docs/migration-guide.md).

### Diagram support and rules

The Go parser recognizes and lints flowchart, sequence, class, ER, and state diagrams. The Worker supports the same diagram families with a smaller set of rules. Query `/v1/diagram-types` and `/v1/rules` on the target service for its runtime capabilities; the [Worker API reference](docs/worker-api.md) describes differences.

Go built-in rules include `no-duplicate-node-ids`, `no-disconnected-nodes`, `max-fanout`, `no-cycles`, `max-depth`, sequence actor/nesting checks, class inheritance/declaration checks, ER relationship checks, and state transition/reachability checks. See [rule fix examples](docs/examples/rule-fixes.md), [suppression examples](docs/examples/rule-suppressions.md), and [rule ID namespaces](docs/rule-id-namespaces.md).

## Architecture

- `cmd/server` exposes the Go HTTP API.
- `cmd/merm8-cli` provides local and server-backed command-line analysis.
- `internal/parser` connects Go to the official Mermaid parser in `parser-node`.
- `internal/engine` runs deterministic rule implementations in `internal/rules`.
- `worker/` and `src/` provide the Worker-safe REST and MCP implementation.

The Go API invokes the Node parser with bounded execution and passes its parsed diagram model to the Go rule engine. The Worker uses a native Worker-compatible parsing/analysis implementation rather than the Go subprocess bridge.

## Configuration and security

The Go server uses environment variables for its port, parser path and limits, strict config validation, CORS, and production authentication/rate limiting. The most important production controls are:

- Set `DEPLOYMENT_MODE=production` and provide `ANALYZE_AUTH_TOKEN`; startup rejects production mode without a valid token.
- Set `ANALYZE_RATE_LIMIT_PER_MINUTE` or use the production default, and keep the service behind TLS and appropriate ingress controls.
- Bound CPU and memory for parser workloads. Tune parser pool, timeout, and memory settings from measured workloads.
- Store credentials in a secrets manager; do not commit tokens or `.env` files.

See [API_GUIDE.md — operational environment variables](API_GUIDE.md#operational-environment-variables), [Parser Tuning Guide](docs/parser-tuning.md), [Metrics & Observability](docs/metrics-observability.md), and [Worker security guidance](docs/worker-api.md#request-limits) for full details. The Go server and Worker have different configuration and authentication behavior; do not assume a setting from one applies to the other.

## Development and tests

**Requirements:** Go 1.24+, Node.js 24+, and npm. Install both Node dependency sets before running the complete suite:

```bash
npm ci
npm --prefix parser-node ci
```

Run Worker/TypeScript checks and tests:

```bash
npm test
```

Run Go checks:

```bash
PARSER_SCRIPT=./parser-node/parse.ts go test ./...
go vet ./...
```

The runtime-dependent server integration suite is separate:

```bash
PARSER_SCRIPT=./parser-node/parse.ts go test -tags=integration ./cmd/server
```

The root `npm test` also checks the README's relative file links. Regenerate and verify OpenAPI artifacts after API contract changes:

```bash
go run ./scripts/generate_openapi.go
./scripts/check_openapi_generated.sh
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [IMPLEMENTATION_GUIDE.md](IMPLEMENTATION_GUIDE.md), and [cmd/server test notes](cmd/server/README.md) for contributor and maintainer workflows.

## Documentation

- [Documentation Index](docs/INDEX.md) — guides organized by audience and task.
- [API Guide](API_GUIDE.md) — Go server API, configuration, and troubleshooting.
- [API Integration Guide](docs/api-integration-guide.md) — client integration, errors, monitoring, and examples.
- [Worker API Reference](docs/worker-api.md) — hosted Worker contract and limitations.
- [Migration Guide](docs/migration-guide.md) — configuration and API migrations.
- [Metrics & Observability](docs/metrics-observability.md) — metrics, alerts, and monitoring.
- [Changelog](CHANGELOG.md) — user-visible release history.

## Contributing, support, and license

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. For bugs and feature requests, use [GitHub Issues](https://github.com/CyanAutomation/merm8/issues). The project is licensed under [Apache-2.0](LICENSE).
