---
name: RestApiDesign
description: Change merm8 HTTP contracts using the canonical OpenAPI and Go API behavior.
---

# REST API Design

## Inputs / Outputs / Non-goals

- Inputs: `openapi.yaml`, `API_GUIDE.md`, `internal/api`, and the target runtime's route table.
- Outputs: A consistent endpoint, request/response contract, error behavior, and contract coverage.
- Non-goals: Do not assume the Cloudflare Worker and Go server expose identical endpoints.

## Trigger conditions

Use this skill when adding or changing HTTP routes, payloads, status codes, headers, versioning, or API documentation.

## Mandatory rules

- The canonical Go analysis route is `POST /v1/analyze`. The `/v1/analyse` spelling is a deprecated compatibility alias. Keep new examples on the canonical route.
- The preferred config shape is `{"schema-version":"v1","rules":{...}}`. Use the documented kebab-case field names, such as `diagram-type`, `syntax-error`, `node-count`, and `rule-id`.
- Keep syntax and lint outcomes separate from HTTP request failures. Mermaid syntax errors are returned as an analysis response with HTTP 200 and `valid: false`; invalid requests/config return 400; oversized bodies return 413; parser saturation returns 503; parser timeout returns 504; internal parser failures return 500.
- Use the existing structured JSON error envelope (`error.code`, `error.message`, and documented details). Avoid `http.Error` for public API errors.
- Treat `openapi.yaml` and `API_GUIDE.md` as contract references. The Cloudflare Worker has a smaller, separately documented contract; inspect its `/v1/spec` and `docs/worker-api.md` before making parity claims.
- Preserve compatibility, deprecation headers, request limits, and version negotiation unless the task explicitly changes them.

## Validation checklist

- [ ] Add handler tests for successful, invalid, and boundary requests affected by the change.
- [ ] Run `go test ./internal/api` and `make test-contract` for Go API contract changes.
- [ ] Update `API_GUIDE.md` and `openapi.yaml`; regenerate/check generated OpenAPI artifacts with `go run ./scripts/generate_openapi.go` and `./scripts/check_openapi_generated.sh` when relevant.
- [ ] For Worker changes, run the Worker tests and update `docs/worker-api.md` separately.

## Expected output format

- Summary: State the endpoint or contract change.
- Evidence: Report handler, contract, and schema checks run.
- Risks: Note compatibility, client migration, or runtime-parity effects.

## Failure/stop conditions

- Stop before introducing a breaking route or schema change without an explicit migration decision.
- If Go and Worker contracts differ, document the target runtime instead of assuming parity.
