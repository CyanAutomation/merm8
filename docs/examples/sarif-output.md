# SARIF Output Examples

This guide demonstrates the SARIF 2.1.0 format output from the `POST /v1/analyze/sarif` endpoint for different analysis scenarios.

---

## Overview

The `/v1/analyze/sarif` endpoint returns analysis results in [SARIF 2.1.0](https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json) format. This format is commonly used by security tools and integrates with GitHub's security tab, Microsoft Security DevOps, and other SIEM platforms.

### Response Format

All responses include:

- **Report**: Root object wrapping all analysis data
- **Version**: Always "2.1.0"
- **Schema**: SARIF schema URI for validation
- **Runs**: Array of Run objects, one per invocation

---

## Case 1: Valid Diagram with Rule Violations

**Scenario**: A flowchart with excessive fan-out (many outgoing edges) triggers the `max-fanout` rule.

### Request

```http
POST /v1/analyze/sarif HTTP/1.1
Content-Type: application/json

{
  "code": "graph TD\n  A[Hub] --> B[Node1]\n  A --> C[Node2]\n  A --> D[Node3]\n  A --> E[Node4]\n  A --> F[Node5]\n  A --> G[Node6]\n  B --> H[Final]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "max-fanout": {
        "enabled": true,
        "limit": 5,
        "severity": "warning"
      }
    }
  }
}
```

### Response (HTTP 200 OK)

```json
{
  "version": "2.1.0",
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "merm8",
          "informationUri": "https://github.com/CyanAutomation/merm8",
          "rules": [
            {
              "id": "max-fanout",
              "name": "max-fanout",
              "shortDescription": {
                "text": "Flags nodes whose outgoing edge count exceeds a configurable limit (default 5)."
              }
            }
          ]
        }
      },
      "artifacts": [
        {
          "location": {
            "uri": "mermaid://diagram"
          }
        }
      ],
      "invocations": [
        {
          "executionSuccessful": true,
          "properties": {
            "request-uri": "/v1/analyze/sarif"
          }
        }
      ],
      "results": [
        {
          "ruleId": "max-fanout",
          "level": "warning",
          "message": {
            "text": "Node exceeds max-fanout limit: 6 outgoing edges (limit: 5)"
          },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "mermaid://diagram"
                },
                "region": {
                  "startLine": 1
                }
              }
            }
          ],
          "partialFingerprints": {
            "ruleId": "max-fanout",
            "nodeId": "A"
          }
        }
      ]
    }
  ]
}
```

### Key Fields Explained

| Field                                            | Value                                | Meaning                                                                          |
| ------------------------------------------------ | ------------------------------------ | -------------------------------------------------------------------------------- |
| `level`                                          | `"warning"`                          | Severity mapped from rule config (error → error, warning → warning, info → note) |
| `ruleId`                                         | `"max-fanout"`                       | Canonical rule identifier from built-in registry                                 |
| `message.text`                                   | `"Node exceeds max-fanout limit..."` | Human-readable issue description                                                 |
| `locations[0].physicalLocation.region.startLine` | `1`                                  | Line in diagram where issue detected (1-indexed)                                 |
| `partialFingerprints`                            | `{ruleId, nodeId}`                   | Uniquely identifies issue for deduplication                                      |

---

## Case 2: Syntax Error

**Scenario**: Invalid Mermaid syntax (missing arrow operator).

### Request

```http
POST /v1/analyze/sarif HTTP/1.1
Content-Type: application/json

{
  "code": "graph TD\n  A[Start]\n  B[Process]\n  C[End]\n  A \u203B B \u203B C"
}
```

### Response (HTTP 400 Bad Request)

```json
{
  "version": "2.1.0",
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "merm8",
          "informationUri": "https://github.com/CyanAutomation/merm8"
        }
      },
      "artifacts": [
        {
          "location": {
            "uri": "mermaid://diagram"
          }
        }
      ],
      "invocations": [
        {
          "executionSuccessful": false,
          "properties": {
            "error-code": "syntax_error",
            "error-message": "Unexpected token at line 4, column 5"
          }
        }
      ],
      "results": [
        {
          "level": "error",
          "message": {
            "text": "Unexpected token at line 4, column 5. Check arrow syntax: --> (left to right) or any valid Mermaid arrow."
          },
          "locations": [
            {
              "physicalLocation": {
                "artifactLocation": {
                  "uri": "mermaid://diagram"
                },
                "region": {
                  "startLine": 4
                }
              }
            }
          ]
        }
      ]
    }
  ]
}
```

### Key Differences from Case 1

| Field                                | Case 1 (Valid)     | Case 2 (Error)                | Meaning                                |
| ------------------------------------ | ------------------ | ----------------------------- | -------------------------------------- |
| `invocations[0].executionSuccessful` | `true`             | `false`                       | Analysis ran vs. failed early          |
| `invocations[0].properties`          | `request-uri` only | `error-code`, `error-message` | Success flow vs. error details         |
| `results[0].ruleId`                  | Set to rule name   | Absent                        | Parser error has no rule match         |
| `results[0].level`                   | `warning`/`info`   | Always `"error"`              | Severity determined by nature of error |

---

## Case 3: Diagram Type Not Supported for Linting

**Scenario**: Valid Mermaid syntax but diagram type has no lint rules available (yet).

### Request

```http
POST /v1/analyze/sarif HTTP/1.1
Content-Type: application/json

{
  "code": "sequenceDiagram\n  Alice->>John: Hello!\n  John-->>Alice: Hi Alice!"
}
```

### Response (HTTP 200 OK)

```json
{
  "version": "2.1.0",
  "$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
  "runs": [
    {
      "tool": {
        "driver": {
          "name": "merm8",
          "informationUri": "https://github.com/CyanAutomation/merm8"
        }
      },
      "artifacts": [
        {
          "location": {
            "uri": "mermaid://diagram"
          }
        }
      ],
      "invocations": [
        {
          "executionSuccessful": true,
          "properties": {
            "request-uri": "/v1/analyze/sarif",
            "diagram-type": "sequence",
            "lint-applicable": false
          }
        }
      ],
      "results": []
    }
  ]
}
```

### Key Fields

| Field                                       | Value        | Meaning                                   |
| ------------------------------------------- | ------------ | ----------------------------------------- |
| `invocations[0].executionSuccessful`        | `true`       | Parser succeeded; diagram valid           |
| `invocations[0].properties.diagram-type`    | `"sequence"` | Diagram type recognized by parser         |
| `invocations[0].properties.lint-applicable` | `false`      | No rules exist for this diagram type yet  |
| `results`                                   | `[]` (empty) | No issues found (because linting skipped) |

---

## HTTP Status Codes

The `POST /v1/analyze/sarif` endpoint returns status codes that indicate the outcome:

| Status                        | Meaning                                     | SARIF Response                              | Notes                                               |
| ----------------------------- | ------------------------------------------- | ------------------------------------------- | --------------------------------------------------- |
| **200 OK**                    | Analysis succeeded, results in `results[]`  | Valid SARIF with findings                   | May have 0 or more results                          |
| **400 Bad Request**           | Invalid JSON, missing code, or config error | SARIF error in `invocations[0]`             | `properties.error-code` set                         |
| **413 Payload Too Large**     | Request body > 1 MiB                        | SARIF error with code `request_too_large`   | Check max diagram size                              |
| **504 Gateway Timeout**       | Parser timeout (default 5s)                 | SARIF error with code `parser_timeout`      | Consider splitting diagram or increasing timeout    |
| **503 Service Unavailable**   | Parser concurrency limit reached            | SARIF error with code `server_busy`         | Include `Retry-After` header; try again after delay |
| **500 Internal Server Error** | Parser subprocess error or memory limit     | SARIF error with code matching failure type | Include error details in `properties`               |

---

## Integration with GitHub Security Tab

The SARIF output is compatible with GitHub's security tab upload. To upload results:

```bash
# After analysis, save SARIF to file
curl -X POST https://api.github.com/repos/YOUR_ORG/YOUR_REPO/code-scanning/sarifs \
  -H "Authorization: token YOUR_GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  -d '{"commit_sha":"YOUR_COMMIT_SHA","ref":"refs/heads/main","sarif":"<SARIF_CONTENT>"}'
```

Ensure your SARIF output passes GitHub's validation by checking:

1. All issue locations have valid `uri` and `startLine`
2. Rule IDs are consistent across all invocations
3. `executionSuccessful` is set appropriately

---

## Severity Mapping

merm8 rule severities map to SARIF levels as follows:

| Rule Severity | SARIF Level | GitHub Tab Color |
| ------------- | ----------- | ---------------- |
| `error`       | `error`     | 🔴 Red           |
| `warning`     | `warning`   | 🟠 Orange        |
| `info`        | `note`      | 🔵 Blue          |
| Parser error  | `error`     | 🔴 Red           |

---

## See Also

- [SARIF 2.1.0 Specification](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html)
- [GitHub Code Scanning Integration](https://docs.github.com/en/code-security/code-scanning/integrating-with-code-scanning)
- [Mermaid Syntax Guide](https://mermaid.js.org/intro/)
