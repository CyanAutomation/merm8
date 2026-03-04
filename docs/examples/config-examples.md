# Configuration Format Examples

This guide demonstrates the three config formats supported by merm8: flat (legacy), nested (legacy), and versioned (canonical).

## Canonical Format (v1.0+) ✅

**Recommended format for all new integrations.**

```json
{
  "code": "graph TD\n  A-->B\n  B-->C\n  D[Disconnected]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 2
      },
      "core/no-disconnected-nodes": {
        "suppression-selectors": ["node:D"]
      }
    }
  }
}
```

**Field Breakdown:**
- `schema-version`: Set to `"v1"` — required for canonical format
- `rules`: Object mapping rule IDs to their configurations
  - Rule ID format: `core/<rule-name>` for built-in rules
  - Each rule config is an object with rule-specific options

**Advantages:**
- ✅ Clear versioning path for future stability
- ✅ Strict validation (no extraneous fields allowed)
- ✅ Only format accepted when strict mode is enabled

---

## Nested Format (v0.9.x – v1.0.x, deprecated) ⚠️

**Legacy format. Still accepted in v1.0.x without strict mode, but deprecated.**

```json
{
  "code": "graph TD\n  A-->B\n  B-->C",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 2
      }
    }
  }
}
```

**Difference from Canonical:**
- Identical structure — nested format is just a variant of canonical.
- Will emit deprecation warning if `config.rules` is present but `config.schema-version` is absent.

---

## Flat Format (v0.8.x – v0.9.x, deprecated) ⚠️

**Legacy format. Accepted without strict mode, but generates deprecation warnings.**

```
POST /v1/analyze
Content-Type: application/json

{
  "code": "graph TD\n  A-->B\n  B-->C",
  "config": {
    "max-fanout": {
      "limit": 2
    },
    "no-disconnected-nodes": {}
  }
}
```

**Structure:**
- Rule configs are direct properties under `config` (not nested under `config.rules`)
- No `schema-version` field

**Deprecation Timeline:**
- v1.0.0 (current): Supported with warnings
- v1.1.0 (Q1 2026): Still supported with warnings
- v1.2.0 (Q2 2026): **Removed**

---

## Migration Guide

### From Flat to Canonical

**Before:**
```json
{
  "code": "graph TD\n  A-->B",
  "config": {
    "max-fanout": {"limit": 3},
    "no-cycles": {}
  }
}
```

**After:**
```json
{
  "code": "graph TD\n  A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {"limit": 3},
      "core/no-cycles": {}
    }
  }
}
```

**Changes:**
1. Add `schema-version: "v1"`
2. Wrap rules under `rules` object
3. Prefix rule IDs with `core/`

### From snake_case to kebab-case

**Before (v1.0.0 – v1.1.0):**
```json
{
  "config": {
    "schema_version": "v1",
    "rules": {
      "core/max_fanout": {
        "limit": 3
      }
    }
  }
}
```

**After (v1.0.0+):**
```json
{
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3
      }
    }
  }
}
```

**Timeline:**
- v1.0.0 (current): Both accepted (snake_case generates warnings)
- v1.1.0 (Q1 2026): snake_case removed
- v1.2.0 (Q2 2026): Only kebab-case accepted

---

## Rule Configuration Reference

### no-duplicate-node-ids
No configuration options — rule has no limits or settings.

```json
"core/no-duplicate-node-ids": {}
```

### no-disconnected-nodes
No built-in options, supports suppression selectors.

```json
"core/no-disconnected-nodes": {
  "suppression-selectors": ["node:OptionalModule"]
}
```

### max-fanout
Configurable outgoing edge limit per node.

```json
"core/max-fanout": {
  "limit": 5,
  "suppression-selectors": ["node:MessageHub"]
}
```

**Default:** `limit: 10`

### max-depth
Configurable graph nesting depth limit.

```json
"core/max-depth": {
  "limit": 8,
  "suppression-selectors": ["node:DeepNode"]
}
```

**Default:** `limit: 10`

### no-cycles
No configuration options — rule detects all circular dependencies.

```json
"core/no-cycles": {
  "suppression-selectors": ["node:LegacyCircular"]
}
```

---

## Complete Example

```json
{
  "code": "graph TD\n  A[API] --> B[Service]\n  B --> C[DB]\n  D[Cache] --> C\n  A --> A[Cycle!]\n  E[Orphan]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-duplicate-node-ids": {},
      "core/no-disconnected-nodes": {
        "suppression-selectors": ["node:E"]
      },
      "core/max-fanout": {
        "limit": 3
      },
      "core/max-depth": {
        "limit": 5
      },
      "core/no-cycles": {
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

---

## Error Handling

### Invalid schema-version
```json
{
  "config": {
    "schema-version": "v2",
    "rules": {}
  }
}
```

**Response (HTTP 400):**
```json
{
  "valid": false,
  "error": {
    "code": "unsupported_schema_version",
    "message": "unsupported config schema-version: v2",
    "details": {
      "supported": ["v1"]
    }
  }
}
```

### Unknown rule
```json
{
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/unknown-rule": {}
    }
  }
}
```

**Response (HTTP 400):**
```json
{
  "valid": false,
  "error": {
    "code": "unknown_rule",
    "message": "unknown rule: core/unknown-rule",
    "details": {
      "supported": ["core/max-depth", "core/max-fanout", "core/no-cycles", "core/no-disconnected-nodes", "core/no-duplicate-node-ids"]
    }
  }
}
```

### Missing required field
```json
{
  "config": {
    "schema-version": "v1"
  }
}
```

**Response (HTTP 400):**
```json
{
  "valid": false,
  "error": {
    "code": "invalid_option",
    "message": "config.rules is required when config.schema-version is set"
  }
}
```
