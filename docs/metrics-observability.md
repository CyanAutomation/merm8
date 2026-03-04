# Metrics & Observability Guide

This service exposes two metrics-style endpoints:

- `GET /metrics` (Prometheus text exposition, canonical scrape target)
- `GET /internal/metrics` (JSON counters for quick debugging / compatibility)

Versioned aliases are also available:

- `GET /v1/metrics`
- `GET /v1/internal/metrics`

## Endpoint audience and access model

| Endpoint | Format | Intended audience | Access control in app | Recommended exposure |
|---|---|---|---|---|
| `/metrics` | Prometheus text exposition | SRE / platform monitoring systems (Prometheus, Grafana Agent, etc.) | No endpoint-specific auth in app; only `POST /analyze` auth/rate-limit middleware is enforced by default | May be scraped from shared monitoring networks; prefer network policy + ingress allow-list |
| `/internal/metrics` | JSON | Operators and developers troubleshooting analyze/parser outcomes | No endpoint-specific auth in app; same global behavior as other non-analyze GET endpoints | Treat as internal-only endpoint; restrict at ingress/load balancer/service mesh |

> Note: when `DEPLOYMENT_MODE=production`, built-in bearer auth applies to `POST /analyze` paths, not to metrics endpoints.

## Metric inventory

### `/metrics` (Prometheus)

| Metric family | Type | Labels | Unit | Cardinality notes |
|---|---|---|---|---|
| `request_total` | Counter | `route`, `method`, `status` | requests | `route` is bounded to known route patterns + `unknown`; `method` is small fixed set; `status` depends on handlers/middleware and should stay low tens |
| `request_duration_seconds` | Histogram | `route`, `method` | seconds | Same bounded `route` and `method` dimensions; bucket count uses Prometheus default buckets |
| `analyze_requests_total` | Counter | `outcome` | requests | `outcome` is bounded enum: `syntax_error`, `lint_success`, `parser_timeout`, `parser_subprocess_error`, `parser_decode_error`, `parser_contract_violation`, `internal_error` |
| `parser_duration_seconds` | Histogram | `outcome` | seconds | Same bounded `outcome` enum as above; default buckets |

### `/internal/metrics` (JSON payload)

Example shape:

```json
{
  "analyze": {
    "valid_success": 0,
    "syntax_error": 0
  },
  "parser": {
    "timeout": 0,
    "subprocess": 0,
    "decode": 0,
    "contract": 0,
    "internal": 0
  }
}
```

Cardinality is fixed (no dynamic labels/keys).

## Alerting starter suggestions (SLO-oriented)

These are starter points; tune thresholds to your traffic and error budget.

1. **Availability / request error rate**
   - Signal: non-2xx/3xx on `/analyze` and `/analyze/raw` via `request_total`.
   - Example: alert when 5m error ratio > 2% and request volume > minimum floor.
2. **Latency (p95/p99)**
   - Signal: `request_duration_seconds` histogram for analyze routes.
   - Example: alert when p95 > 1.0s for 15m.
3. **Parser runtime regressions**
   - Signal: `parser_duration_seconds` by `outcome="lint_success"`.
   - Example: alert on p95 drift above baseline or sudden sustained increase.
4. **Parser failure modes**
   - Signal: `analyze_requests_total` by failure outcomes (`parser_timeout`, `parser_subprocess_error`, `parser_decode_error`, `parser_contract_violation`, `internal_error`).
   - Example: alert if any severe outcome exceeds a low absolute count for 10m, or failure ratio > 0.5%.
5. **Timeout-specific protection**
   - Signal: `analyze_requests_total{outcome="parser_timeout"}` rate.
   - Example: page if timeout ratio exceeds 1% for 10m.

## Prometheus scrape configuration

Recommended baseline:

- Scrape interval: `15s` for production API targets.
- Scrape timeout: `5s`.
- `honor_labels: false` (default) unless you have a strict reason.
- Keep path-level labeling stable with explicit relabeling.

```yaml
scrape_configs:
  - job_name: merm8
    metrics_path: /metrics
    scrape_interval: 15s
    scrape_timeout: 5s
    static_configs:
      - targets:
          - merm8-prod-1:8080
          - merm8-prod-2:8080
        labels:
          service: merm8
          env: prod

    relabel_configs:
      # Normalize instance to host-only if target includes port.
      - source_labels: [__address__]
        regex: '([^:]+)(?::\\d+)?'
        target_label: instance
        replacement: '$1'

      # Copy service/env from target labels into canonical labels.
      - source_labels: [service]
        target_label: app
      - source_labels: [env]
        target_label: environment

    metric_relabel_configs:
      # Optional: drop unknown route bucket if you do not route unmatched paths.
      - source_labels: [route]
        regex: unknown
        action: drop
```

For very low-traffic environments (dev/staging), `30s` scrape interval is usually enough.

## Recommended Alerts & Runbooks

The `docs/prometheus-alerts.yaml` file contains production-ready Prometheus alert rules for merm8. These rules monitor:

1. **Parser timeout rate** — Alerts when >1% of requests timeout over 10 minutes
   - **Action:** Reduce diagram complexity, increase `PARSER_TIMEOUT_SECONDS`, or scale parser concurrency
   - **Details:** See `ParserTimeoutRateHigh` in `prometheus-alerts.yaml`

2. **Parser memory limit** — Alerts when parser subprocess hits memory ceiling
   - **Action:** Increase `PARSER_MAX_OLD_SPACE_MB` or reduce diagram size
   - **Details:** See `ParserMemoryLimitExceeded` in `prometheus-alerts.yaml`

3. **Parser subprocess errors** — Alerts on parser crashes or fatal errors
   - **Action:** Check logs, verify system resources, contact support
   - **Details:** See `ParserSubprocessErrors` in `prometheus-alerts.yaml`

4. **HTTP error rate** — Alerts when >10% of HTTP requests return 5xx status
   - **Action:** Check application and system logs for root cause
   - **Details:** See `HTTPErrorRateHigh` in `prometheus-alerts.yaml`

5. **Parser service degradation** — Alerts when combined parser failures >5%
   - **Action:** Initiate incident response, scale resources, consider circuit-breaking
   - **Details:** See `ParserServiceDegradation` in `prometheus-alerts.yaml`

6. **Request latency P95** — Alerts when 95th percentile latency exceeds 5s
   - **Action:** Optimize rule execution, increase resources, implement request queuing
   - **Details:** See `RequestLatencyHigh` in `prometheus-alerts.yaml`

### Using These Alerts

Import `prometheus-alerts.yaml` into your Prometheus config:

```yaml
rule_files:
  - 'prometheus-alerts.yaml'  # Path relative to your prometheus.yml
```

Then configure Alertmanager to route alerts appropriately (e.g., to Slack, PagerDuty, etc.).

### Tuning Alert Thresholds

Default thresholds are conservative for production stability:

- **Parser timeout:** 1% — adjust to 0.5% for stricter SLA, 5% for lenient
- **Error rate:** 10% — adjust to 5% if you require tighter SLOs
- **P95 latency:** 5s — adjust down to 1-2s for frontend-critical workloads
- **Service degradation:** 5% — adjust to match your error budget

For isolated dev/staging deployments, disable alerting or increase all thresholds by 5–10x.

## Doc maintenance guard (drift detection)

A test enforces that this document still references all currently exported metric names and `/internal/metrics` keys.

- Test: `go test ./internal/api -run TestMetricsDocsContainCurrentNames`
- If metrics are renamed or added, update this guide in the same PR.
