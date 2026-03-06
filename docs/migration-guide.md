# Migration Guide: Legacy Config → Canonical v1 Format

This guide explains how to migrate Phase-1 legacy config inputs to the canonical versioned format.

## Rollout timeline

| Policy | Version/date |
|---|---|
| Accepted since | v1.0.0 |
| Warn since | v1.0.0 |
| Removal target | v1.2.0 (Q2 2026 planned) |

## Legacy formats still accepted in Phase-1

1. `config.schema_version` (snake_case) instead of `config.schema-version`.
2. Unversioned nested config (`config.rules` without `config.schema-version`).
3. Flat config shape (`config.{rule-id}` without `config.rules`).
4. Snake_case option keys under rule config (`suppression_selectors` vs `suppression-selectors`).

## Before/after examples

### 1) `schema_version` → `schema-version`

**Before**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema_version": "v1",
    "rules": { "max-fanout": { "limit": 3 } }
  }
}
```

**After**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": { "max-fanout": { "limit": 3 } }
  }
}
```

### 2) unversioned nested `rules` → add schema version

**Before**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "rules": { "max-fanout": { "limit": 3 } }
  }
}
```

**After**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": { "max-fanout": { "limit": 3 } }
  }
}
```

### 3) flat config → move under `rules`

**Before**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "max-fanout": { "limit": 3 }
  }
}
```

**After**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": { "limit": 3 }
    }
  }
}
```

### 4) snake_case option keys → kebab-case option keys

**Before**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": {
        "suppression_selectors": ["node:A"]
      }
    }
  }
}
```

**After**
```json
{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": {
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

## Runtime warning behavior during Phase-1

When legacy input is used, the API emits:

- `Deprecation: true`
- one or more `Warning: 299 - "..."` headers with migration examples
- `warnings` in JSON response
- `meta.warnings[]` structured metadata (`code`, `message`, `replacement`)
- server log warning with migration hint


## Rule ID namespacing migration (built-ins and plugins)

As part of rule ID extensibility hardening:

- Built-in rule IDs are moving toward explicit `core/<id>` naming in docs and discovery output.
- Existing unnamespaced built-ins (for example `max-fanout`) remain accepted in config during migration.
- Config normalization now accepts `core/<id>` for built-ins and maps it to the canonical built-in key used by the active registry.
- If both `max-fanout` and `core/max-fanout` are supplied in config, entries are merged deterministically into one rule config object.
- Plugin rule IDs remain `custom/<provider>/<id>` and must match a registered runtime rule exactly.

Recommended client posture:

1. Treat `/v1/rules` as source-of-truth for active IDs.
2. Store IDs as opaque strings.
3. Prefer emitting `core/<id>` for built-ins in newly generated config payloads.

---

## Response Field Deprecation: Underscore Aliases

The API returned response fields have underscore variants for backward compatibility. These will be removed in **v1.2.0 (Q2 2026)**.

| Response Field | Deprecated Alias | Location | Status |
|---|---|---|---|
| `diagram-type` | `diagram_type` | AnalyzeResponse.metrics | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `lint-supported` | `lint_supported` | AnalyzeResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `parser-timeout-seconds` | `parser_timeout_seconds` | InfoResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `node-count` | `node_count` | metricsResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `edge-count` | `edge_count` | metricsResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `max-fanin` | `max_fanin` | metricsResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `max-fanout` | `max_fanout` | metricsResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `syntax-error` | `syntax_error` | AnalyzeResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `issue-counts` | `issue_counts` | metricsResponse | ⚠️ Deprecated in v1.0, removal v1.2.0 |

### How to Detect Which You're Using

**Canonical (kebab-case)**: Required from v1.2.0 onward
```json
{
  "diagram-type": "flowchart",
  "lint-supported": true,
  "metrics": {
    "node-count": 5,
    "edge-count": 4
  }
}
```

**Legacy (snake_case)**: Still accepted in v1.0–v1.1, but logged as warnings
```json
{
  "diagram_type": "flowchart",
  "lint_supported": true,
  "metrics": {
    "node_count": 5,
    "edge_count": 4
  }
}
```

### Migration Timeline

| Timeline | Action |
|---|---|
| **v1.0–v1.1** (current) | Underscore aliases returned alongside canonical names in some responses; server logs deprecation warnings |
| **v1.2.0 (Q2 2026)** | Underscore aliases completely removed; clients using old names will see errors |

### Recommended Action

**Update your client code now** to use canonical kebab-case field names:

**Before**
```python
diagram_type = response['diagram_type']
node_count = response['metrics']['node_count']
lint_supported = response['lint_supported']
```

**After**
```python
diagram_type = response['diagram-type']
node_count = response['metrics']['node-count']
lint_supported = response['lint-supported']
```

### Backward-Compatibility Headers

The API may emit `Deprecation: true` and `Sunset: <date>` headers when underscore aliases are used:

```
Deprecation: true
Sunset: Tue, 30 Jun 2026 23:59:59 GMT
Link: </v1/docs#/Linting/post_v1_analyze>; rel="successor-version"
```

Applications should monitor these headers and plan upgrades accordingly.
