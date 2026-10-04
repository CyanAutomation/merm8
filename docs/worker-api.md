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
| `POST` | `/v1/analyze/sarif` | Analyze Mermaid source and return SARIF 2.1.0 |
| `POST` | `/v1/semantic-review` | Authenticated JEV semantic review alongside deterministic analysis |

The Worker recognizes and lints flowchart, sequence, class, ER, and state diagrams.
Its rule set includes `max-fanout`, `no-cycles`, `no-disconnected-nodes`, and
`no-duplicate-node-ids` for flowcharts, plus `no-undefined-actors` for sequence,
`no-duplicate-classes` for class, `no-self-referential` for ER, and
`no-unreachable-state` for state diagrams. Use `/v1/diagram-types` and `/v1/rules`
to discover the capabilities of the deployment you are calling.

Every successful analysis response includes `lint-supported`. A `false` value means
the source was parsed successfully but no Worker lint rules ran for that diagram type.
Successful responses also include node, edge, connectivity, fan-in/out, and issue-count
metrics. The response uses HTTP 200 when no family rules are available.

`POST /v1/analyze/sarif` accepts the same JSON body and rule configuration as
`/v1/analyze`. Valid source returns a SARIF 2.1.0 report with rule metadata, source
locations, and stable issue fingerprints. Invalid JSON, rule configuration, Mermaid,
and oversized requests return the same JSON error contract used by `/v1/analyze`.

## Deterministic analysis and semantic review

`POST /v1/analyze` is deterministic and does not call a model or require model
credentials. It remains the endpoint for repeatable structural checks.

`POST /v1/semantic-review` is a separate, optional capability. It reuses the
Worker parser and deterministic family rules, then sends compact structured
diagram state to OpenRouter's Decisions API. JEV returns probabilistic semantic
judgements in `semantic-review`; these are not deterministic lint issues and do
not change the meaning of `/v1/analyze`.

The semantic endpoint requires the Merm8 `API_KEY` using either
`Authorization: Bearer <API_KEY>` or `x-api-key: <API_KEY>`. OpenRouter uses a
separate server-side `OPENROUTER_API_KEY`; that credential is never sent to
clients. Configure it as a Worker secret:

```bash
npx wrangler secret put API_KEY
npx wrangler secret put OPENROUTER_API_KEY
```

The decision model defaults to `~typesafe/jev-latest`. Set the optional
`MERM8_DECISION_MODEL` Worker variable to select another Decisions API model.
Do not put either API key in `wrangler.toml`.

Example request:

```http
POST /v1/semantic-review HTTP/1.1
Authorization: Bearer YOUR_MERM8_API_KEY
Content-Type: application/json

{"code":"flowchart TD\nA[Receive order] --> B[Validate payment]"}
```

The response includes deterministic structural fields and a separate semantic
section. `noul` probabilities are returned directly; each accompanying boolean
is a neutral 0.5 normalization for clients that need a simple display value.

```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "issues": [],
  "metrics": {
    "node-count": 2,
    "edge-count": 1,
    "disconnected-node-count": 0,
    "duplicate-node-count": 0,
    "max-fanin": 1,
    "max-fanout": 1,
    "diagram-type": "flowchart",
    "issue-counts": { "by-severity": {}, "by-rule": {} }
  },
  "semantic-review": {
    "purpose": { "value": "process", "confidence": 0.91 },
    "label-clarity": { "value": true, "probability": 0.87 },
    "branch-clarity": { "value": true, "probability": 0.81 },
    "abstraction-consistency": { "value": false, "probability": 0.76 },
    "ambiguity": { "value": false, "probability": 0.16 },
    "review-priority": { "value": "low", "confidence": 0.79 }
  },
  "meta": { "source": "jev", "model": "typesafe/jev-1.13" }
}
```

Invalid Mermaid is returned as `400 invalid_mermaid`. The semantic state is
bounded to 200 nodes, 400 edges, and 64 KiB; oversized state returns
`413 semantic_state_too_large`.
If the Worker parser recognizes the header but produces no nodes, the endpoint
returns `422 semantic_state_unavailable` instead of asking JEV to infer missing
diagram content. Missing `OPENROUTER_API_KEY` returns
`503 missing_openrouter_credentials`; invalid model configuration returns
`503 semantic_review_unconfigured`. Provider timeouts return `504 jev_timeout`,
invalid provider answers return `502 jev_invalid_response`, and unavailable
provider responses return `502` or `503 jev_upstream_error`.

## Rule configuration validation

The Worker validates rule settings before analysis. Rule configuration must be an
object, rule IDs must be known, `enabled` must be boolean, `severity` must be
`error`, `warning`, or `info`, and `max-fanout.limit` must be a non-negative integer.
Invalid settings return HTTP 400 with an `invalid_option` (or `unknown_rule`) error;
they are never silently replaced with defaults.

## Request limits

`POST /v1/analyze` and `/v1/analyze/sarif` accept JSON request bodies up to 1 MiB. Larger requests
receive `413` with the `payload_too_large` error code. Clients should split
larger diagrams before sending them for analysis.

For public deployments, also configure a Cloudflare WAF rate-limiting rule for
`POST /v1/analyze`. Rate-limiting rules are account-level Cloudflare settings,
so they are intentionally not embedded in the Worker source.

Because semantic review calls an external paid service, protect it with the
required `API_KEY` and apply an appropriate Cloudflare rate limit to
`POST /v1/semantic-review` as well.

## Important differences from the Go server

The Worker does **not** expose the Go server's raw-analysis, Prometheus metrics,
or legacy unversioned endpoints. In particular, do not call:

- `POST /v1/analyze/raw`
- `GET /metrics`

For those capabilities, run the Go server described in the root README and use
the Go-server API guide. Clients should use `/v1/spec` and
`/v1/diagram-types` during setup rather than assuming one deployment supports
every endpoint in this repository.

## Browser access

CORS is restricted by the Worker's `REST_ALLOWED_ORIGINS` setting. The hosted
splash application is an allowed origin. The Worker includes its deployment
provenance in the `X-Merm8-Build` response header.
