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

