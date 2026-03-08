# Migration Guide: Legacy Config → Canonical v1 Format

Complete guide to migrating your merm8 integration from deprecated config formats to the canonical v1 schema.

---

## Overview

merm8 v1.0.0 introduced a versioned config schema (`schema-version: v1`) to support long-term stability and future extensibility. Legacy formats are still accepted but are scheduled for removal.

**Action Required:** Migrate to the canonical v1 format before Q2 2026 when legacy formats are removed.

---

## Deprecation Timeline

| Version | Date    | Status  | Legacy Format Support     |
| ------- | ------- | ------- | ------------------------- |
| v1.0.0  | 2026-03 | Current | ✅ Accepted with warnings |
| v1.1.0  | 2026-06 | Planned | ✅ Accepted with warnings |
| v1.2.0  | 2026-12 | Planned | ❌ **Removed**            |

### Sunset Dates by Format

| Format                                                       | Deprecated | Sun set Date | Action                    |
| ------------------------------------------------------------ | ---------- | ------------ | ------------------------- |
| Flat config (`config.{rule-id}`)                             | v1.0.0     | 2026-12-31   | **URGENT** - Migrate now  |
| Unversioned nested (`config.rules` without `schema-version`) | v1.0.0     | 2026-12-31   | **URGENT** - Migrate now  |
| Snake_case `schema_version`                                  | v1.0.0     | 2026-09-30   | Migrate by Sept 2026      |
| Snake_case option keys                                       | v1.0.0     | 2026-09-30   | Migrate by Sept 2026      |
| Unnamespaced rule IDs (`max-fanout` vs `core/max-fanout`)    | v1.0.0     | 2026-12-31   | **Recommended** to update |

---

## Legacy Formats Still Accepted

1. **Flat config shape** - Rules at root level instead of under `config.rules`
2. **Unversioned nested config** - `config.rules` exists but `schema-version` is missing
3. **Snake_case `schema_version`** - Use `schema_version` instead of `schema-version`
4. **Snake_case option keys** - Use `suppression_selectors` instead of `suppression-selectors`
5. **Unnamespaced rule IDs** - Use `max-fanout` instead of `core/max-fanout`

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

| Response Field           | Deprecated Alias         | Location                | Status                                |
| ------------------------ | ------------------------ | ----------------------- | ------------------------------------- |
| `diagram-type`           | `diagram_type`           | AnalyzeResponse.metrics | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `lint-supported`         | `lint_supported`         | AnalyzeResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `parser-timeout-seconds` | `parser_timeout_seconds` | InfoResponse            | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `node-count`             | `node_count`             | metricsResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `edge-count`             | `edge_count`             | metricsResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `max-fanin`              | `max_fanin`              | metricsResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `max-fanout`             | `max_fanout`             | metricsResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `syntax-error`           | `syntax_error`           | AnalyzeResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |
| `issue-counts`           | `issue_counts`           | metricsResponse         | ⚠️ Deprecated in v1.0, removal v1.2.0 |

---

## Step-by-Step Migration

### Step 1: Identify Your Legacy Format

Check your current request format:

```bash
# Download your current config from environment/config file
cat your-merm8-config.json | jq '.config'

# Compare with patterns below to identify issues
```

**Pattern A: Flat config (most urgent)**

```json
"config": {
  "max-fanout": {"limit": 3}    // ❌ Issue: rules at root level
}
```

**Pattern B: Unversioned nested**

```json
"config": {
  "rules": {"max-fanout": {}}    // ❌ Issue: missing schema-version
}
```

**Pattern C: Snake_case field names**

```json
"config": {
  "schema_version": "v1",        // ❌ Issue: underscore instead of hyphen
  "rules": {"max_fanout": {}}    // ❌ Issue: snake_case rule ID
}
```

### Step 2: Check API Response for Warnings

When you POST a request with legacy config, check for warnings:

```bash
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d @legacy-request.json | jq '.warnings'

# Output if using legacy format:
# [
#   "legacy flat config shape is deprecated; move rule settings under config.rules and add config.schema-version"
# ]
```

### Step 3: Automated Migration Script

Use this script to convert flat config to versioned format:

```bash
#!/bin/bash
# migrate-config.sh - Convert legacy flat config to v1 format

jq -r 'if .config then
  if .config | has("schema-version") or .config | has("rules") then
    # Already has schema or rules - ensure schema-version exists
    if .config | has("schema-version") | not then
      .config."schema-version" = "v1"
    else . end
  else
    # Flat config - move everything under rules
    .config = {
      "schema-version": "v1",
      "rules": (.config | del(.schema-version, .schema_version))
    }
  end
else . end' < legacy.json > migrated.json

# Also convert snake_case to kebab-case
jq '
.config.rules |= if . then
  with_entries(
    .value |= if type == "object" then
      with_entries(
        .key |= gsub("_"; "-")
      )
    else . end
  )
else . end |
.config |= if . then
  with_entries(
    .key |= gsub("_"; "-")
  )
else . end
' < migrated.json > final.json
```

### Step 4: Validate Your Migrated Config

Test the migration:

```bash
# Validate against merm8
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d @final.json | jq '{valid, warnings, meta}'

# Should show: "warnings": [] (no migration warnings)

# Also check your parser/client doesn't break
# Run full integration tests with new config
```

---

## Migration Checklist

- [ ] **Identify** your current config format (flat, unversioned, or legacy fields)
- [ ] **Run** validation to check for warnings: `curl /v1/analyze | jq '.warnings'`
- [ ] **Convert** using migration script or manually
- [ ] **Test** converted config with `/v1/analyze` endpoint
- [ ] **Verify** no warnings in response
- [ ] **Update** your codebase/infrastructure with new config
- [ ] **Review** API response fields for underscore aliases - update client code
- [ ] **Deploy** before v1.2.0 sunset date

---

## Common Migration Scenarios

### Scenario 1: Simple Flat Config

**Before:**

```json
{
  "code": "graph TD\n  A-->B\n  A-->C\n  A-->D\n  A-->E\n  A-->F\n  A-->G",
  "config": {
    "max-fanout": {
      "limit": 3,
      "severity": "error"
    },
    "no-cycles": {
      "allow-self-loop": true
    }
  }
}
```

**After:**

```json
{
  "code": "graph TD\n  A-->B\n  A-->C\n  A-->D\n  A-->E\n  A-->F\n  A-->G",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3,
        "severity": "error"
      },
      "core/no-cycles": {
        "allow-self-loop": true
      }
    }
  }
}
```

### Scenario 2: Config with Suppressions

**Before:**

```json
{
  "code": "...",
  "config": {
    "max-fanout": {
      "suppression_selectors": ["node:HubNode", "node:Gateway"]
    }
  }
}
```

**After:**

```json
{
  "code": "...",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "suppression-selectors": ["node:HubNode", "node:Gateway"]
      }
    }
  }
}
```

### Scenario 3: Complex Multi-Rule Config

**Before:**

```json
{
  "code": "...",
  "config": {
    "schema_version": "v1",
    "rules": {
      "max_fanout": { "limit": 4 },
      "max_depth": { "limit": 5 },
      "no_cycles": {},
      "no_disconnected_nodes": {
        "suppression_selectors": ["node:Deprecated"]
      }
    }
  }
}
```

**After:**

```json
{
  "code": "...",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": { "limit": 4 },
      "core/max-depth": { "limit": 5 },
      "core/no-cycles": {},
      "core/no-disconnected-nodes": {
        "suppression-selectors": ["node:Deprecated"]
      }
    }
  }
}
```

---

## Response Field Migration

Check your code for usage of underscore response fields:

```javascript
// ❌ OLD - Using underscore aliases (deprecated)
if (response.diagram_type === "flowchart") {
  // Use diagram_type
}

// ✅ NEW - Using canonical field names (required v1.2.0+)
if (response["diagram-type"] === "flowchart") {
  // Use diagram-type
}
```

### Maps of Field Changes

| Old Field                | New Field                | Location      | Action             |
| ------------------------ | ------------------------ | ------------- | ------------------ |
| `diagram_type`           | `diagram-type`           | metrics       | Update client code |
| `lint_supported`         | `lint-supported`         | response      | Update client code |
| `parser_timeout_seconds` | `parser-timeout-seconds` | info response | Update client code |
| `node_count`             | `node-count`             | metrics       | Update client code |
| `edge_count`             | `edge-count`             | metrics       | Update client code |
| `max_fanin`              | `max-fanin`              | metrics       | Update client code |
| `max_fanout`             | `max-fanout`             | metrics       | Update client code |
| `syntax_error`           | `syntax-error`           | response      | Update client code |

---

## Verification Tools

### 1. Check Config Validity

```bash
# Get supported rules
curl http://localhost:8080/v1/rules | jq '.rules[].id' | sort

# Validate your config
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d @your-config.json \
  | jq '{valid, warnings, error}'
```

### 2. Check Response Format

```bash
# Verify you have canonical field names
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d @request.json \
  | jq 'keys'

# Should contain: diagram-type, lint-supported, syntax-error, etc.
# Should NOT contain: diagram_type, lint_supported, syntax_error, etc.
```

### 3. Monitor Migration Progress

```bash
# Find configs with warnings (legacy format detected)
for file in *.json; do
  warnings=$(curl -X POST http://localhost:8080/v1/analyze \
    -d @"$file" | jq '.warnings | length')

  if [ "$warnings" -gt 0 ]; then
    echo "$file: has $warnings warnings"
  fi
done
```

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

| Timeline                | Action                                                                                                    |
| ----------------------- | --------------------------------------------------------------------------------------------------------- |
| **v1.0–v1.1** (current) | Underscore aliases returned alongside canonical names in some responses; server logs deprecation warnings |
| **v1.2.0 (Q2 2026)**    | Underscore aliases completely removed; clients using old names will see errors                            |

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
