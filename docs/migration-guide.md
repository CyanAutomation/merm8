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
| v1.2.0  | 2026-Q2 | Planned | Route aliases removed; config sunset dates apply             |

### Sunset Dates by Format

| Format                                                       | Deprecated | Sun set Date | Action                    |
| ------------------------------------------------------------ | ---------- | ------------ | ------------------------- |
| Flat config (`config.{rule-id}`)                             | v1.0.0     | 2026-12-31   | **URGENT** - Migrate now  |
| Unversioned nested (`config.rules` without `schema-version`) | v1.0.0     | 2026-12-31   | **URGENT** - Migrate now  |
| Snake_case `schema_version`                                  | v1.0.0     | 2026-09-30   | Migrate by Sept 2026      |
| Snake_case option keys                                       | v1.0.0     | 2026-09-30   | Migrate by Sept 2026      |

---

## Deprecated API Endpoints

All API endpoints use the `/v1/*` prefix. Only British-English spelling aliases for analyze operations (`POST /v1/analyse`, `POST /v1/analyse/raw`) remain active as temporary compatibility routes; they emit deprecation headers and are scheduled for removal in v1.2.0 (Q2 2026).

### Legacy Spelling Aliases

| Alias | Canonical | Status |
|---|---|---|
| `POST /v1/analyse` | `POST /v1/analyze` | Active with deprecation header |
| `POST /v1/analyse/raw` | `POST /v1/analyze/raw` | Active with deprecation header |

Note: Bare unversioned paths (for example `/analyze`, `/healthz`, `/ready`) were never registered on the server. Clients must always use `/v1/*` paths.

### Migration Steps

1. **Search your codebase** for any HTTP requests to non-v1 paths (e.g., `/analyze`, `/healthz`, `/ready`)
2. **Replace with canonical paths** prefixed with `/v1/` (e.g., `/v1/analyze`, `/v1/healthz`)
3. **Update documentation and examples** to reference `/v1/*` paths
4. **Check client libraries** for hardcoded endpoint paths

### Example: Update Endpoint URLs

**Never supported in v1+ (bare paths do not exist)**:

```bash
curl -X POST http://localhost:8080/analyze \
  -H "Content-Type: application/json" \
  -d '{"code":"graph TD\n  A --> B"}'
```

**Correct**:

```bash
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code":"graph TD\n  A --> B"}'
```
## Legacy Formats Still Accepted

1. **Flat config shape** - Rules at root level instead of under `config.rules`
2. **Unversioned nested config** - `config.rules` exists but `schema-version` is missing
3. **Snake_case `schema_version`** - Use `schema_version` instead of `schema-version`
4. **Snake_case option keys** - Use `suppression_selectors` instead of `suppression-selectors`
5. **Unnamespaced rule IDs** - `max-fanout` is what `GET /v1/rules` returns; `core/` prefix in config is normalized to the bare ID at validation time.

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

## Response Field Contract

The API exclusively returns **kebab-case** field names in all JSON responses (for example: `diagram-type`, `lint-supported`, `node-count`). No snake_case alias variants are emitted by any endpoint.

Clients should use kebab-case field names consistently. Code examples that reference underscore variants (`diagram_type`, `lint_supported`) will fail against the live API.


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
- [ ] **Verify** response field names are kebab-case (no underscores: diagram-type, lint-supported, node-count)
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
      "max-fanout": {
        "limit": 3,
        "severity": "error"
      },
      "no-cycles": {
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
      "max-fanout": {
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
      "max-fanout": { "limit": 4 },
      "max-depth": { "limit": 5 },
      "no-cycles": {},
      "no-disconnected-nodes": {
        "suppression-selectors": ["node:Deprecated"]
      }
    }
  }
}
```

---

## Response Field Contract Note

All API responses exclusively use **kebab-case** field names. No snake_case or underscore variants exist in any endpoint response. The following sections of this guide that reference underscore aliases as deprecated or removable are incorrect and should be disregarded.

Correct client code example:

```javascript
// All fields are kebab-case - no underscore aliases exist
if (response["diagram-type"] === "flowchart") {
  // correct: use diagram-type from response
}
```


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

### Response Field Format

All API responses use **kebab-case** field names exclusively. No snake_case or underscore variants are emitted.

**Correct format example:**

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

Use these kebab-case field names in all client code. There are no deprecated underscore aliases to migrate from.

### Legacy Spelling Alias Deprecation

The British-English spelling aliases (`POST /v1/analyse`, `POST /v1/analyse/raw`) emit deprecation headers:

```
Deprecation: true
Warning: 299 - "POST /v1/analyse is deprecated; use POST /v1/analyze. Planned removal in v1.2.0 (Q2 2026)."
Sunset: Tue, 30 Jun 2026 23:59:59 GMT
Link: </v1/docs#/Linting/post_v1_analyze>; rel="successor-version"
```

These headers appear when requests hit the `/v1/analyse*` endpoints specifically, not related to response field naming. Migrate all HTTP calls from `/v1/analyse` to `/v1/analyze` before June 30, 2026.
