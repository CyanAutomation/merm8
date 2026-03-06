# Parser Tuning Guide

This guide helps you optimize merm8's parser performance for different diagram sizes and complexity levels. The parser is Node.js-based and can be configured via environment variables and per-request settings.

---

## Quick Reference

| Scenario | Timeout | Memory | Concurrency | Expected |
|----------|---------|--------|-------------|----------|
| Small/simple (< 100 nodes) | 2-3s | 256MB | 8 | < 100ms |
| Medium (100-500 nodes) | 5s | 512MB | 8 | 100-500ms |
| Large (500-2000 nodes) | 10s | 1024MB | 4 | 500ms-2s |
| Very large (> 2000 nodes) | 15-30s | 2048MB+ | 2 | 2s+ |

---

## Key Parameters

### 1. PARSER_TIMEOUT_SECONDS (Default: 5)

**What it does**: Maximum wall-clock time allowed for parsing a single diagram.

**Valid range**: 1–60 seconds

**When to adjust**:
- ⬆️ **Increase** if you see `parser_timeout` errors on large diagrams
- ⬇️ **Decrease** if you need faster rejection of malformed input (prioritize responsiveness)

**Trade-off**: Higher timeout = more latency for timeout cases, but allows complex diagrams to complete.

**Examples**:
```bash
# For small, fast diagrams (tight SLA)
PARSER_TIMEOUT_SECONDS=2 go run ./cmd/server

# For large enterprise diagrams
PARSER_TIMEOUT_SECONDS=15 go run ./cmd/server

# Maximum (rare)
PARSER_TIMEOUT_SECONDS=60 go run ./cmd/server
```

**Per-request override**:
```json
{
  "code": "...",
  "parser": {
    "timeout_seconds": 10
  }
}
```

---

### 2. PARSER_MAX_OLD_SPACE_MB (Default: 512)

**What it does**: Cap on Node.js V8 old-space heap size per parser subprocess (in MiB).

**Valid range**: 128–4096 MiB

**When to adjust**:
- ⬆️ **Increase** if you see `parser_memory_limit` errors on large diagrams
- ⬇️ **Decrease** to constrain memory usage in resource-limited environments

**Trade-off**: Higher memory = ability to parse very large diagrams, but higher overall memory footprint; lower memory = protection against runaway processes.

**Rule of thumb**: 
- Simple diagrams: 256MB sufficient
- Medium complexity: 512MB (default)
- Large with deep nesting: 1024MB+
- Very large with many nodes: 2048MB+

**Examples**:
```bash
# For resource-constrained environments (edge deployments)
PARSER_MAX_OLD_SPACE_MB=256 go run ./cmd/server

# Default (balanced)
PARSER_MAX_OLD_SPACE_MB=512 go run ./cmd/server

# Large diagram support
PARSER_MAX_OLD_SPACE_MB=2048 go run ./cmd/server
```

**Per-request override**:
```json
{
  "code": "...",
  "parser": {
    "max_old_space_mb": 768
  }
}
```

---

### 3. PARSER_CONCURRENCY_LIMIT (Default: 8)

**What it does**: Maximum number of simultaneous parser subprocesses allowed.

**Valid range**: 1–∞ (recommended: 2–16 based on available CPU cores)

**When to adjust**:
- ⬆️ **Increase** if CPU is underutilized and you want higher throughput (requires more memory)
- ⬇️ **Decrease** if parser processes are consuming too much memory or CPU

**Trade-off**: Higher concurrency = higher throughput but higher resource usage; lower concurrency = lower latency variance but lower peak throughput.

**Rule of thumb**:
- CPU cores ÷ 2 = reasonable concurrency (e.g., 8 cores → 4 concurrency)
- Or: aim for 50-80% CPU utilization under expected load

**Examples**:
```bash
# For single-threaded/embedded deployments
PARSER_CONCURRENCY_LIMIT=1 go run ./cmd/server

# Default (balanced for 4-core machines)
PARSER_CONCURRENCY_LIMIT=8 go run ./cmd/server

# High-throughput (dedicated parser server, 16+ cores)
PARSER_CONCURRENCY_LIMIT=16 go run ./cmd/server
```

**Behavior when limit is hit**: 
- 9th request arrives while 8 are in-flight
- Server returns **503 Service Unavailable** with `error.code=server_busy`
- Client should retry after `Retry-After: 1` second

---

## Decision Tree: How to Tune

```
Start: Getting timeout or memory errors?
│
├─ YES: parser_timeout error (HTTP 504)?
│  │
│  └─ FIRST: Can you reduce diagram size? (split into smaller sub-diagrams)
│     │
│     ├─ YES → Preferred solution; keep timeout low for responsiveness
│     │
│     └─ NO → Increase PARSER_TIMEOUT_SECONDS gradually (5s → 10s → 15s)
│
├─ YES: parser_memory_limit error (HTTP 500)?
│  │
│  └─ FIRST: Can you reduce diagram complexity? (fewer nodes, less nesting)
│     │
│     ├─ YES → Preferred solution; keep memory baseline low
│     │
│     └─ NO → Increase PARSER_MAX_OLD_SPACE_MB (512 → 1024 → 2048)
│
├─ NO: Server idle despite load (low CPU)?
│  │
│  └─ Consider raising PARSER_CONCURRENCY_LIMIT (8 → 12 → 16)
│     Monitor: max RSS of parser processes
│
└─ NO: Server busy under load (high CPU, many 503 errors)?
   │
   └─ Option A: Increase PARSER_CONCURRENCY_LIMIT if CPU headroom
      Option B: Reduce PARSER_CONCURRENCY_LIMIT to protect latency
      Option C: Load balance across multiple merm8 instances
```

---

## Tuning by Deployment Environment

### Development / Local

```bash
# Sane defaults, fast feedback loop
PARSER_TIMEOUT_SECONDS=5
PARSER_MAX_OLD_SPACE_MB=512
PARSER_CONCURRENCY_LIMIT=8
go run ./cmd/server
```

### CI/CD (GitHub Actions, GitLab CI)

```bash
# Tight SLA, limited concurrency, fast timeout for quick feedback
PARSER_TIMEOUT_SECONDS=3
PARSER_MAX_OLD_SPACE_MB=256
PARSER_CONCURRENCY_LIMIT=2
./merm8-cli diagram1.mmd diagram2.mmd
```

### Production (Cloud Run, Kubernetes)

```bash
# Balanced for scale, memory controlled, concurrency tuned to CPU
PARSER_TIMEOUT_SECONDS=10
PARSER_MAX_OLD_SPACE_MB=1024
PARSER_CONCURRENCY_LIMIT=12
# Plus: set resource limits in container manifest
```

### Production (High-Volume)

```bash
# Generous timeouts for complex diagrams, high concurrency
PARSER_TIMEOUT_SECONDS=20
PARSER_MAX_OLD_SPACE_MB=2048
PARSER_CONCURRENCY_LIMIT=16
# Plus: distribute across multiple instances with load balancer
```

---

## Monitoring & Observability

### Prometheus Metrics to Watch

After solving issues, monitor these metrics to tune ongoing:

```promql
# P95 parser latency
histogram_quantile(0.95, rate(parser_duration_seconds_bucket[5m]))

# Timeout rate
rate(analyze_requests_total{outcome="parser_timeout"}[5m])

# Memory limit errors
rate(analyze_requests_total{outcome="parser_memory_limit_error"}[5m])

# Server busy rate (concurrency limit hit)
rate(analyze_requests_total{outcome="server_busy"}[5m])

# Request concurrency (in-flight)
request_duration_seconds (as gauge proxy: concurrent requests)
```

### Query Examples

**Alert: Timeout rate > 1%**
```yaml
alert: ParserTimeoutRate
expr: rate(analyze_requests_total{outcome="parser_timeout"}[5m]) > 0.01
action: Increase PARSER_TIMEOUT_SECONDS or split diagrams
```

**Alert: Memory limit errors**
```yaml
alert: ParserMemoryLimitErrors
expr: increase(analyze_requests_total{outcome="parser_memory_limit_error"}[5m]) > 0
action: Increase PARSER_MAX_OLD_SPACE_MB
```

**Alert: Server busy (concurrency limit)**
```yaml
alert: ConcurrencyLimitHit
expr: rate(analyze_requests_total{outcome="server_busy"}[5m]) > 0.05
action: Increase PARSER_CONCURRENCY_LIMIT or add more instances
```

---

## Performance Benchmarks

### Measured on: 4-core, 8GB RAM machine (Cloud Run)

| Diagram Type | Nodes | Edges | Depth | Timeout=5s | Timeout=10s | Notes |
|---|---|---|---|---|---|---|
| Simple flowchart | 5 | 4 | 2 | ✅ 12ms | ✅ 12ms | Linear chain |
| Medium flowchart | 50 | 60 | 8 | ✅ 45ms | ✅ 45ms | Branching |
| Complex flowchart | 200 | 300 | 15 | ✅ 180ms | ✅ 180ms | Highly branched |
| Very complex | 1000 | 1500 | 25 | ⚠️ 2800ms | ✅ 2800ms | Multiple hubs |
| Pathological (deep) | 100 | 99 | 99 | ❌ Timeout | ✅ 3200ms | Linear deep stack |
| Pathological (wide) | 5000 | 5000 | 5 | ❌ Timeout | ❌ Timeout | Too large, split needed |

**Key insights**:
- Depth is more problematic than breadth (O(n²) complexity in parser)
- 500+ node diagrams should use timeout ≥ 10s
- Truly pathological cases (1000+ nodes) benefit from splitting into smaller diagrams
- Parser rarely saturates memory below 2000 nodes on 512MB limit

---

## Troubleshooting

### Symptom: Frequent `parser_timeout` errors (HTTP 504)

**Step 1**: Check diagram complexity
```bash
# Count nodes/edges
wc -l diagram.mmd
grep -c "\[" diagram.mmd  # Crude node count
```

**Step 2**: Check if it's actually slow or just timeout too low
- Run locally: `time node parse.mjs < diagram.mmd`
- If > 2 seconds: consider splitting diagram

**Step 3**: Increase timeout incrementally
```bash
PARSER_TIMEOUT_SECONDS=10 go run ./cmd/server
# Test same diagram
# If still times out: PARSER_TIMEOUT_SECONDS=15
```

**Step 4**: If timeout now works, consider why diagram is slow
- Add intermediate subgraphs to reduce depth
- Move unrelated flows to separate diagrams
- Profile Node.js parser: `node --prof parse.mjs`

---

### Symptom: `parser_memory_limit` errors (HTTP 500)

**Step 1**: Check memory limit hit
```bash
# Logs will show: parser_memory_limit error
# Check current setting
echo $PARSER_MAX_OLD_SPACE_MB
```

**Step 2**: Increase memory
```bash
PARSER_MAX_OLD_SPACE_MB=1024 go run ./cmd/server
# Test same diagram
```

**Step 3**: If memory errors persist with 2048MB, diagram is too large to parse
- Split into multiple diagrams
- Remove nodes that aren't essential to lint
- File issue with Mermaid team if parser has memory leak

---

### Symptom: Many `server_busy` errors (HTTP 503) under load

**Step 1**: Check concurrency limit
```bash
# You're hitting this many times/sec:
echo "Analyzing: $(date +%s%N | cut -b1-13) - server busy" >> busy.log
grep -c "server busy" busy.log  # Count
```

**Step 2**: Increase concurrency
```bash
PARSER_CONCURRENCY_LIMIT=12 go run ./cmd/server
# Monitor CPU after change
```

**Step 3**: If CPU still low, increase more; if high, stay at limit
```bash
PARSER_CONCURRENCY_LIMIT=16 go run ./cmd/server
```

**Step 4**: If still hitting limit, add more instances
- Deploy 2-3 merm8 instances behind load balancer
- Each gets fewer requests, less 503

---

## Advanced: Segment Configuration by Request Size

Use per-request parser settings to adapt to diagram size:

```json
{
  "code": "...(small diagram)...",
  "parser": {
    "timeout_seconds": 2,    // Small, fast timeout
    "max_old_space_mb": 256
  }
}
```

vs.

```json
{
  "code": "...(large diagram)...",
  "parser": {
    "timeout_seconds": 15,   // Large, generous timeout
    "max_old_space_mb": 1024
  }
}
```

Client can detect diagram size (`code.length`) and adjust settings:
```javascript
const config = {
  parser: {
    timeout_seconds: code.length < 1000 ? 2 : 10,
    max_old_space_mb: code.length < 1000 ? 256 : 512
  }
};
```

---

## See Also

- [API Guide: Parser Settings](../API_GUIDE.md#operational-environment-variables)
- [Metrics & Observability](./metrics-observability.md)
- [Mermaid Parser Limits](https://github.com/mermaid-js/mermaid/wiki/Performance)
