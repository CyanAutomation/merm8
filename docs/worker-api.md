# Cloudflare Worker API Reference

The hosted merm8 service is a Cloudflare Worker. It is a Worker-safe,
intentionally smaller implementation than the local Go server. This document
describes the hosted contract; use `GET /v1/spec` as the authority for a
specific deployment.

## Endpoints

| Method | Endpoint | Purpose |
| --- | --- | --- |
| `GET` | `/v1/healthz` | Liveness probe |
| `GET` | `/v1/ready` | Readiness and parser status |
| `GET` | `/v1/version` | Build and service metadata |
| `GET` | `/v1/spec` | OpenAPI description of this Worker |
| `GET` | `/v1/docs` | Redirect to the OpenAPI description |
| `GET` | `/v1/diagram-types` | Recognized and lint-supported diagram types |
| `GET` | `/v1/rules` | Available lint rules |
| `POST` | `/v1/analyze` | Validate Mermaid source and lint supported diagrams |

The Worker recognizes flowchart, sequence, class, ER, and state diagrams. Unlike the
Go server, whose default engine lints all five families, this smaller Worker runtime
currently provides only the flowchart rule set. Use `/v1/diagram-types` to discover
the capabilities of the deployment you are calling.

Every successful analysis response includes `lint-supported`. A `false` value means
the source was parsed successfully but no Worker lint rules ran for that diagram type.
The response still has HTTP 200 and an empty `issues` array.

## Rule configuration validation

The Worker validates rule settings before analysis. Rule configuration must be an
object, rule IDs must be known, `enabled` must be boolean, `severity` must be
`error`, `warning`, or `info`, and `max-fanout.limit` must be a non-negative integer.
Invalid settings return HTTP 400 with an `invalid_option` (or `unknown_rule`) error;
they are never silently replaced with defaults.

## Request limits

`POST /v1/analyze` accepts JSON request bodies up to 1 MiB. Larger requests
receive `413` with the `payload_too_large` error code. Clients should split
larger diagrams before sending them for analysis.

For public deployments, also configure a Cloudflare WAF rate-limiting rule for
`POST /v1/analyze`. Rate-limiting rules are account-level Cloudflare settings,
so they are intentionally not embedded in the Worker source.

## Important differences from the Go server

The Worker does **not** expose the Go server's raw-analysis, SARIF, metrics,
or legacy unversioned endpoints. In particular, do not call:

- `POST /v1/analyze/raw`
- `POST /v1/analyze/sarif`
- `GET /metrics`

For those capabilities, run the Go server described in the root README and use
the Go-server API guide. Clients should use `/v1/spec` and
`/v1/diagram-types` during setup rather than assuming one deployment supports
every endpoint in this repository.

## Browser access

CORS is restricted by the Worker's `REST_ALLOWED_ORIGINS` setting. The hosted
splash application is an allowed origin. The Worker includes its deployment
provenance in the `X-Merm8-Build` response header.
