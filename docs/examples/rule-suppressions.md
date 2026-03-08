# Rule Suppression Examples

This guide demonstrates how to suppress linting rules at different scopes in merm8.

## Suppression Selectors

Merm8 supports three types of suppression selectors:

### 1. Node-level Suppression

Suppress rule violations for specific nodes.

```json
{
  "code": "graph TD\n  A[Final] --> B[Unused]\n  B --> C[End]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-disconnected-nodes": {
        "suppression-selectors": ["node:B"]
      }
    }
  }
}
```

**Result:** Node `B` is exempt from the `no-disconnected-nodes` rule.

### 2. Subgraph-level Suppression

Suppress rule violations for nodes within a subgraph (experimental, flowchart only).

```json
{
  "code": "graph TD\n  subgraph deprecated[Old System]\n    A[Service A]\n    B[Service B]\n  end",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "suppression-selectors": ["subgraph:deprecated"]
      }
    }
  }
}
```

### 3. Global Rule Suppression

Suppress rule violations globally without config - add a suppression comment above the diagram:

**Diagram Code:**

```mermaid
%! suppress rule:core/no-cycles

graph TD
  A[Node A] --> B[Node B]
  B --> C[Node C]
  C --> A
```

When parsed with the `POST /v1/analyze` endpoint, comment-based suppressions are recognized.

### 4. Negation (Exemption Override)

Explicitly exempt specific nodes even when the rule is globally suppressed:

```json
{
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3,
        "suppression-selectors": [
          "node:HighFanoutHub",
          "!node:CriticalConnection"
        ]
      }
    }
  }
}
```

This suppresses `HighFanoutHub` but enforces the rule on `CriticalConnection`.

## Examples by Rule

### no-duplicate-node-ids

**Problem:** Duplicate node IDs discovered.

```json
{
  "code": "graph TD\n  A[Start]\n  B[Middle]\n  A[Duplicate!]\n  A --> B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-duplicate-node-ids": {
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

### no-disconnected-nodes

**Problem:** Nodes exist but have no inbound/outbound edges.

```json
{
  "code": "graph TD\n  A[Connected] --> B[Also]\n  C[Orphaned]\n  D[Lonely]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-disconnected-nodes": {
        "suppression-selectors": ["node:C", "node:D"]
      }
    }
  }
}
```

### max-fanout

**Problem:** Single node exceeds outgoing edge limit.

```json
{
  "code": "graph TD\n  Hub[Central Hub]\n  Hub --> A\n  Hub --> B\n  Hub --> C\n  Hub --> D\n  Hub --> E",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3,
        "suppression-selectors": ["node:Hub"]
      }
    }
  }
}
```

### max-depth

**Problem:** Graph exceeds maximum nesting depth.

```json
{
  "code": "graph TD\n  L1[Level 1] --> L2[Level 2]\n  L2 --> L3[Level 3]\n  L3 --> L4[Level 4]\n  L4 --> L5[Level 5]\n  L5 --> L6[Level 6]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-depth": {
        "limit": 4,
        "suppression-selectors": ["node:L5", "node:L6"]
      }
    }
  }
}
```

### no-cycles

**Problem:** Graph contains circular dependencies.

```json
{
  "code": "graph TD\n  A[Service A] --> B[Service B]\n  B --> C[Service C]\n  C --> A",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-cycles": {
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

## Best Practices

1. **Minimize Suppressions:** Use suppressions sparingly. Violations often signal architectural issues.
2. **Document Suppressions:** Add comments explaining why a suppression is necessary.
3. **Use Specific Selectors:** Prefer node/subgraph selectors over global rule suppression.
4. **Review Regularly:** Periodically audit suppressions to see if issues have been resolved.
5. **Test Coverage:** Ensure your linting configuration is reflected in tests and CI/CD.
