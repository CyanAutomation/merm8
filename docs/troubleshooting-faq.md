# Troubleshooting & FAQ

Common issues, error codes, and solutions for merm8.

---

## HTTP Status Codes

| Code | Meaning | Usual Cause | Solution |
|------|---------|-------------|----------|
| 200 | OK | Request successful | No action needed |
| 400 | Bad Request | Invalid JSON or config | Check request format and config validity |
| 403 | Forbidden | Auth token missing/invalid | Verify `Authorization: Bearer <token>` header |
| 429 | Too Many Requests | Rate limit exceeded | Implement exponential backoff; check `X-RateLimit-*` headers |
| 502 | Bad Gateway | Server communication issue | Restart merm8 process; check network routing |
| 503 | Service Unavailable | Server busy or parser timeout | Reduce concurrency; increase `PARSER_TIMEOUT_SECONDS` |
| 504 | Gateway Timeout | Request exceeded server timeout | Simplify diagram or increase timeout |

---

## Common Issues

### Issue: "Invalid config object"

**Error:**
```json
{
  "valid": false,
  "error": {
    "code": "invalid_option",
    "message": "invalid config object"
  }
}
```

**Causes:**
1. Config is not valid JSON
2. Config is null instead of object
3. Missing `schema-version` field in strict mode

**Solutions:**

```bash
# ❌ Wrong - config is a string
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "...", "config": "invalid"}'

# ✅ Correct - config is an object
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "...", "config": {"schema-version": "v1", "rules": {}}}'

# ✅ Also correct - empty config
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code": "...", "config": {}}'
```

---

### Issue: "unknown rule: core/max-fanout"

**Error:**
```json
{
  "error": {
    "code": "unknown_rule",
    "message": "unknown rule: core/max-fanout"
  }
}
```

**Causes:**
1. Rule ID typo
2. Using legacy rule ID (without `core/` prefix)
3. Rule not registered in current version

**Solutions:**

```bash
# Get list of supported rules
curl http://localhost:8080/v1/rules | jq '.rules[].id'

# Output:
# core/max-fanout
# core/max-depth
# core/no-cycles
# ...

# ❌ Wrong - legacy rule ID
{"config": {"schema-version": "v1", "rules": {"max-fanout": {}}}}

# ✅ Correct - use rule ID from /v1/rules
{"config": {"schema-version": "v1", "rules": {"core/max-fanout": {}}}}
```

---

### Issue: "Parser timeout after Xs"

**Error:**
```json
{
  "valid": false,
  "error": {
    "code": "parser_timeout",
    "message": "Parser timeout after 5 seconds"
  }
}
```

**Causes:**
1. Diagram is very complex
2. Parser timeout too short for diagram
3. System resources exhausted

**Solutions:**

```bash
# Check current timeout
curl http://localhost:8080/v1/info | jq '.parser-timeout-seconds'

# Request with longer timeout
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{
    "code": "...",
    "parser": {"timeout_seconds": 15}
  }'

# Or start server with longer timeout
PARSER_TIMEOUT_SECONDS=15 /start-merm8.sh

# Tips for complex diagrams:
# - Break into smaller sub-diagrams
# - Use subgraphs to reduce visual complexity
# - Reduce node count if possible
# - Simplify edge patterns
```

---

### Issue: "Rate limit exceeded"

**Error:**
```json
{
  "error": {
    "code": "rate_limited",
    "message": "rate limit exceeded"
  }
}
```

**Response Headers:**
```
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1678123456
```

**Solutions:**

```bash
# Calculate wait time
RESET_TIME=$(curl -sI http://localhost:8080/v1/analyze | grep X-RateLimit-Reset | cut -d' ' -f2)
WAIT_MS=$(( ($RESET_TIME * 1000) - $(date +%s000) ))
echo "Wait ${WAIT_MS}ms before retrying"

# Implement backoff
for i in {1..5}; do
  response=$(curl -X POST http://localhost:8080/v1/analyze -d '...')
  if [ $? -eq 0 ]; then break; fi
  
  sleep $((2 ** $i))  # Exponential backoff: 2, 4, 8, 16, 32 seconds
done

# Or increase server rate limit
ANALYZE_RATE_LIMIT_PER_MINUTE=120 /start-merm8.sh
```

---

### Issue: "Deprecation warning: legacy config format"

**Warning in Response:**
```json
{
  "warnings": [
    "legacy flat config shape is deprecated; move rule settings under config.rules and add config.schema-version"
  ]
}
```

**Meaning:**
Your config is in deprecated format but still accepted. It will be removed in v1.2.0 (Q2 2026).

**Migration:**

```bash
# ❌ Deprecated - flat config
{
  "code": "...",
  "config": {
    "max-fanout": {"limit": 3}
  }
}

# ✅ Recommended - versioned config
{
  "code": "...",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {"limit": 3}
    }
  }
}
```

**Check migration timeline:**
```bash
curl http://localhost:8080/v1/config-versions | jq '.deprecations'
```

---

### Issue: "JSON parse error on line X"

**Error:**
```json
{
  "error": {
    "code": "invalid_request",
    "message": "JSON parse error on line 5"
  }
}
```

**Causes:**
1. Malformed JSON in request body
2. Unescaped quotes in code string
3. Missing commas or brackets

**Solutions:**

```bash
# ❌ Wrong - unescaped newlines
curl -X POST http://localhost:8080/v1/analyze \
  -d '{"code": "graph TD
       A-->B", ...}'

# ✅ Correct - escaped newlines
curl -X POST http://localhost:8080/v1/analyze \
  -d '{"code": "graph TD\n  A-->B", ...}'

# ✅ Using file
cat > request.json << 'EOF'
{
  "code": "graph TD\n  A-->B\n  B-->C",
  "config": {"schema-version": "v1", "rules": {}}
}
EOF
curl -X POST http://localhost:8080/v1/analyze -d @request.json

# Test JSON validity
jq . request.json  # Will show parse errors
```

---

## FAQ

### Q: Can I suppress specific nodes from a rule?

**A:** Yes, use `suppression-selectors`:

```json
{
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3,
        "suppression-selectors": ["node:HubNode"]
      }
    }
  }
}
```

See [Rule Suppression Examples](examples/rule-suppressions.md).

---

### Q: What's the difference between `core/max-fanout` and `max-fanout`?

**A:** `core/` is the rule namespace prefix added in v1.0.0.

- **`max-fanout`** (legacy) - Still accepted but deprecated
- **`core/max-fanout`** (recommended) - Use this format

Check `/v1/rules` for the canonical rule ID format.

---

### Q: How do I get SARIF output for CI/CD integration?

**A:** Use the `/v1/analyze/sarif` endpoint:

```bash
curl -X POST http://localhost:8080/v1/analyze/sarif \
  -H "Content-Type: application/json" \
  -d @- << 'EOF'
{
  "code": "...",
  "config": {"schema-version": "v1", "rules": {...}}
}
EOF
```

Output is SARIF 2.1.0 format. See [SARIF Examples](examples/sarif-output.md).

---

### Q: Can I run merm8 with authentication?

**A:** Yes, in production mode:

```bash
DEPLOYMENT_MODE=production \
ANALYZE_AUTH_TOKEN=my-secret-token \
/start-merm8.sh

# Then authenticate requests:
curl -X POST http://localhost:8080/v1/analyze \
  -H "Authorization: Bearer my-secret-token" \
  -d '...'
```

---

### Q: What's the best parser timeout for large diagrams?

**A:** Use this guide:

| Diagram Size | Nodes | Timeout | Memory |
|---|---|---|---|
| Small | < 100 | 2-3s | 256MB |
| Medium | 100-500 | 5s | 512MB |
| Large | 500-2000 | 10s | 1024MB |
| Very Large | > 2000 | 15-30s | 2048MB+ |

Test with your actual diagrams:

```bash
# Time a single diagram
time curl -X POST http://localhost:8080/v1/analyze \
  -d @large-diagram.json | jq '.metrics'
```

---

### Q: How do I monitor merm8 in production?

**A:** Use these endpoints:

```bash
# Liveness (process health)
curl http://localhost:8080/v1/healthz
# Should return 200 with {"status": "ok"}

# Readiness (dependencies ready)
curl http://localhost:8080/v1/ready
# Should return 200 with {"status": "ready"}

# Metrics (Prometheus format)
curl http://localhost:8080/v1/metrics | head -20

# Info (version, parser versions, supported rules)
curl http://localhost:8080/v1/info
```

---

### Q: How can I bulk-process diagrams?

**A:** Process in batches with rate limiting:

```bash
#!/bin/bash

for diagram in *.mmd; do
  echo "Processing $diagram..."
  
  # Convert Mermaid file to analyze request
  payload=$(jq -n \
    --arg code "$(cat "$diagram")" \
    '{
      "code": $code,
      "config": {
        "schema-version": "v1",
        "rules": {}
      }
    }')
  
  # Send request
  curl -X POST http://localhost:8080/v1/analyze \
    -H "Content-Type: application/json" \
    -d "$payload" > "${diagram%.mmd}-results.json"
  
  # Respect rate limits
  sleep 0.5
done
```

---

### Q: Can I use merm8 with GitHub Actions?

**A:** Yes, use SARIF output and upload-sarif action:

```yaml
name: Lint Diagrams
on: [push]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Run merm8 linting
        run: |
          docker run --rm -v $PWD:/workspace \
            merm8:latest \
            /app/merm8-cli /workspace/diagrams/*.mmd \
            > results.sarif
      
      - name: Upload to GitHub Security
        uses: github/codeql-action/upload-sarif@v2
        with:
          sarif_file: results.sarif
```

---

### Q: How do I use merm8 locally for development?

**A:** Use the provided merm8-cli:

```bash
# Build it
go build -o merm8-cli ./cmd/merm8-cli

# Lint a file
./merm8-cli --analyze mydiagram.mmd

# With custom config
./merm8-cli --analyze \
  --config '{"schema-version":"v1","rules":{"core/max-fanout":{"limit":3}}}' \
  mydiagram.mmd

# Get help
./merm8-cli --help
```

---

## Still Having Issues?

1. **Check logs:** Enable debug logging with `LOG_LEVEL=debug`
2. **Verify request format:** Use `jq` to validate JSON
3. **Test endpoint health:** `curl http://localhost:8080/v1/healthz`
4. **Check supported rules:** `curl http://localhost:8080/v1/rules`
5. **Review examples:** See [docs/examples/](examples/) directory

---

