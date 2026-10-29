# Caching behavior

## Cloudflare Worker

`POST /v1/analyze` and its SARIF and legacy aliases use a per-isolate LRU for
identical source and rule configuration. Entries are limited to 128, 4 MiB of
key and serialized result data, and 60 seconds. The `Cache-Status` response
header reports `merm8; hit` or `merm8; fwd=uri-miss`; CORS clients may read it.
Rate limiting still runs for every request, including cache hits.

Semantic review reuses successful provider decisions for an identical semantic
state, question set, and requested model for up to two minutes. It retains at
most 32 results and 2 MiB of serialized result data per isolate. Cache keys are
SHA-256 digests; diagram source is not stored in a cache key. Errors are never
cached. Concurrent identical requests share one in-flight provider call, with at
most 32 distinct in-flight keys tracked per isolate. The existing request
limiter still applies to hits. Logs include a `cache-result` field.

Worker cache memory is local to a runtime isolate. Isolates can be evicted at
any time, and Cloudflare does not guarantee that consecutive requests reach the
same isolate. These caches are best-effort and do not persist or share diagram
content across locations.

`GET /v1/spec`, `/v1/rules`, and `/v1/diagram-types` cache their serialized
metadata for five minutes and return build-specific ETags. `/v1/version` uses a
one-minute freshness period. Health and readiness routes remain uncached.

## Go server

The parser caches successful parses and syntax errors separately. Their entry
count, TTL, and estimated byte limits are configurable with the
`PARSER_CACHE_*` variables documented in [parser tuning](parser-tuning.md).
`parser_cache_events_total` reports hits, misses, and evictions, while
`parser_cache_estimated_bytes` reports estimated retained bytes by cache type.

Deterministic rule results and aggregate diagram metrics are cached for 30
seconds, up to 256 entries and 8 MiB of serialized results, per server process.
`analysis_cache_events_total` reports hits, misses, and evictions. Per-rule
execution timings and issue counters are recorded when rules run; a cache hit
does not create synthetic rule execution measurements. Dynamic request IDs and
timestamps are still generated for each response.

Go `/v1/spec` and Swagger assets retain their one-hour freshness period and
support conditional requests using ETags.
