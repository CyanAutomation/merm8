# Complete Request/Response Examples

Comprehensive examples of all analyze endpoint scenarios with actual JSON payloads and responses.

---

## 1. Minimal Valid Request

**Scenario:** Empty diagram with no rules configured

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  A-->B",
  "config": {}
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "550e8400-e29b-41d4-a716-446655440000",
  "timestamp": 1678123456000,
  "syntax-error": null,
  "issues": [],
  "suggestions": [],
  "warnings": [],
  "meta": {
    "warnings": []
  },
  "metrics": {
    "parser-duration-ms": 18,
    "lint-duration-ms": 2,
    "total-duration-ms": 20
  }
}
```

---

## 2. Valid Diagram with Single Rule

**Scenario:** Check max-fanout with limit of 2

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json
Accept-Version: 1.0

{
  "code": "graph TD\n  A[Hub] -->|out1| B\n  A -->|out2| C\n  A -->|out3| D",
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

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "660e8400-e29b-41d4-a716-446655440001",
  "timestamp": 1678123456100,
  "content-version": "1.0",
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "core/max-fanout",
      "rule-type": "structural",
      "severity": "warning",
      "message": "Node 'A' has 3 outgoing edges, exceeds limit of 2",
      "location": {
        "line": 1,
        "column": 15
      },
      "node-id": "A",
      "suppression-status": "active"
    }
  ],
  "suggestions": [
    "Consider refactoring to split high-fanout nodes",
    "Or increase the limit if this pattern is intentional"
  ],
  "warnings": [],
  "meta": {
    "warnings": []
  },
  "metrics": {
    "parser-duration-ms": 22,
    "lint-duration-ms": 5,
    "total-duration-ms": 27,
    "rules-checked": 1
  }
}
```

---

## 3. Multiple Rules with Config

**Scenario:** Check several rules simultaneously

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  subgraph sg[SG]\n    A-->B\n    B-->C\n    C-->A\n  end\n  D[Isolated]",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/no-cycles": {
        "severity": "error"
      },
      "core/no-disconnected-nodes": {
        "severity": "warning"
      },
      "core/max-depth": {
        "limit": 2
      }
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "770e8400-e29b-41d4-a716-446655440002",
  "timestamp": 1678123456200,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "core/no-cycles",
      "rule-type": "structural",
      "severity": "error",
      "message": "Cycle detected: A → B → C → A",
      "location": {
        "line": 2,
        "column": 8
      },
      "cycle": {
        "nodes": ["A", "B", "C"],
        "edges": 3
      }
    },
    {
      "rule-id": "core/no-disconnected-nodes",
      "rule-type": "structural",
      "severity": "warning",
      "message": "Node 'D' is disconnected from main graph",
      "location": {
        "line": 6,
        "column": 10
      },
      "node-id": "D"
    }
  ],
  "suggestions": [
    "Break the cycle A → B → C → A by removing one edge",
    "Connect isolated node D to the main graph",
    "Review if this pattern is intentional"
  ],
  "warnings": [],
  "metrics": {
    "parser-duration-ms": 25,
    "lint-duration-ms": 12,
    "total-duration-ms": 37,
    "rules-checked": 3,
    "issues-by-severity": {
      "error": 1,
      "warning": 1
    }
  }
}
```

---

## 4. Suppression Example

**Scenario:** Suppress specific node from rule

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  A[Critical Hub] -->|1| B\n  A -->|2| C\n  A -->|3| D\n  A -->|4| E\n  A -->|5| F",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 3,
        "suppression-selectors": ["node:A"]
      }
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "880e8400-e29b-41d4-a716-446655440003",
  "timestamp": 1678123456300,
  "syntax-error": null,
  "issues": [],
  "suggestions": [],
  "warnings": [],
  "meta": {
    "warnings": []
  },
  "metrics": {
    "parser-duration-ms": 20,
    "lint-duration-ms": 8,
    "total-duration-ms": 28,
    "suppressions-applied": [
      {
        "rule-id": "core/max-fanout",
        "selector": "node:A",
        "matched-nodes": 1
      }
    ]
  }
}
```

---

## 5. Parse Error Response

**Scenario:** Invalid Mermaid syntax

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  INVALID SYNTAX HERE @#$%",
  "config": {"schema-version": "v1", "rules": {}}
}
```

**Response (200 OK):**
```json
{
  "valid": false,
  "diagram-type": null,
  "lint-supported": false,
  "request-id": "990e8400-e29b-41d4-a716-446655440004",
  "timestamp": 1678123456400,
  "syntax-error": {
    "line": 1,
    "column": 19,
    "message": "Unexpected token '@'",
    "kind": "syntax_error",
    "context": "graph TD\n  INVALID SYNTAX HERE @#$%"
  },
  "issues": [],
  "error": null,
  "metrics": {
    "parser-duration-ms": 15
  }
}
```

---

## 6. Config Error Response

**Scenario:** Invalid configuration

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD; A-->B",
  "config": {
    "schema-version": "v1",
    "rules": {
      "unknown-rule": {}
    }
  }
}
```

**Response (400 Bad Request):**
```json
{
  "valid": false,
  "error": {
    "code": "unknown_rule",
    "message": "unknown rule: unknown-rule",
    "path": "config.rules.unknown-rule",
    "supported": [
      "core/max-fanout",
      "core/max-depth",
      "core/no-cycles",
      "core/no-disconnected-nodes",
      "core/no-duplicate-node-ids"
    ]
  },
  "request-id": "aa0e8400-e29b-41d4-a716-446655440005",
  "timestamp": 1678123456500
}
```

---

## 7. Parser Timeout Response

**Scenario:** Diagram too complex for timeout

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  ... (very complex diagram with many nodes and edges)",
  "parser": {
    "timeout_seconds": 2
  },
  "config": {"schema-version": "v1", "rules": {}}
}
```

**Response (503 Service Unavailable):**
```json
{
  "valid": false,
  "error": {
    "code": "parser_timeout",
    "message": "Parser timeout after 2 seconds",
    "kind": "timeout"
  },
  "request-id": "bb0e8400-e29b-41d4-a716-446655440006",
  "timestamp": 1678123456600,
  "metrics": {
    "parser-duration-ms": 2001
  }
}
```

---

## 8. Rate Limited Response

**Scenario:** Too many requests

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  ... (subsequent requests after hitting limit)
}
```

**Response (429 Too Many Requests):**
```json
{
  "valid": false,
  "error": {
    "code": "rate_limited",
    "message": "rate limit exceeded",
    "kind": "rate_limit"
  },
  "request-id": "cc0e8400-e29b-41d4-a716-446655440007",
  "timestamp": 1678123456700
}
```

**Response Headers:**
```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1678123500
Retry-After: 60
```

---

## 9. Deprecated Config Warning

**Scenario:** Using legacy flat config (still accepted)

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD; A-->B; A-->C; A-->D; A-->E; A-->F",
  "config": {
    "max-fanout": {
      "limit": 3
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "dd0e8400-e29b-41d4-a716-446655440008",
  "timestamp": 1678123456800,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "core/max-fanout",
      "severity": "warning",
      "message": "Node 'A' has 5 outgoing edges, exceeds limit of 3",
      "node-id": "A"
    }
  ],
  "warnings": [
    "legacy flat config shape is deprecated; move rule settings under config.rules and add config.schema-version. Example: {\"config\":{\"schema-version\":\"v1\",\"rules\":{\"max-fanout\":{\"limit\":3}}}}"
  ],
  "meta": {
    "warnings": [
      {
        "code": "deprecated_config_format",
        "message": "Legacy flat config shape",
        "replacement": "Use config.schema-version: v1 with config.rules"
      }
    ]
  }
}
```

---

## 10. Complex Enterprise Diagram

**Scenario:** Full microservices architecture validation

**Request:**
```http
POST /v1/analyze HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  subgraph api[API Layer]\n    GW[API Gateway]\n    Auth[Auth Service]\n  end\n  subgraph svc[Services]\n    User[User Service]\n    Order[Order Service]\n    Product[Product Service]\n  end\n  subgraph db[Database]\n    UserDB[User DB]\n    OrderDB[Order DB]\n    ProductDB[Product DB]\n  end\n  GW -->|authenticate| Auth\n  GW -->|route| User\n  GW -->|route| Order\n  GW -->|route| Product\n  Auth --> UserDB\n  User --> UserDB\n  Order --> OrderDB\n  Product --> ProductDB",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {
        "limit": 4,
        "severity": "warning"
      },
      "core/max-depth": {
        "limit": 3,
        "severity": "warning"
      },
      "core/no-cycles": {
        "severity": "error"
      },
      "core/no-duplicate-node-ids": {
        "severity": "error"
      }
    }
  }
}
```

**Response (200 OK):**
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "ee0e8400-e29b-41d4-a716-446655440009",
  "timestamp": 1678123456900,
  "syntax-error": null,
  "issues": [],
  "suggestions": [],
  "warnings": [],
  "metrics": {
    "parser-duration-ms": 35,
    "lint-duration-ms": 18,
    "total-duration-ms": 53,
    "diagram-stats": {
      "node-count": 13,
      "edge-count": 11,
      "max-fanin": 2,
      "max-fanout": 4,
      "max-depth": 3,
      "cycles": 0,
      "subgraphs": 3,
      "disconnected-nodes": 0
    },
    "rules-checked": 4,
    "issues-by-severity": {}
  }
}
```

---

## 11. Raw Analysis Format

**Scenario:** Using /analyze/raw endpoint (returns markdown comments)

**Request:**
```http
POST /v1/analyze/raw HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{
  "code": "graph TD\n  A[Hub] --> B\n  A --> C\n  A --> D\n  A --> E\n  A --> F\n  A --> G",
  "config": {
    "schema-version": "v1",
    "rules": {
      "core/max-fanout": {"limit": 3}
    }
  }
}
```

**Response (200 OK):** (same as regular /analyze, with raw field added)
```json
{
  "valid": true,
  "diagram-type": "flowchart",
  "lint-supported": true,
  "request-id": "ff0e8400-e29b-41d4-a716-446655440010",
  "timestamp": 1678123457000,
  "syntax-error": null,
  "issues": [
    {
      "rule-id": "core/max-fanout",
      "severity": "warning",
      "message": "Node 'A' has 6 outgoing edges, exceeds limit of 3",
      "node-id": "A",
      "raw-comment": "%! suppress rule:core/max-fanout for node:A"
    }
  ],
  "suggestions": [
    "Add comment at top of diagram: %! suppress rule:core/max-fanout"
  ]
}
```

---

## Helper Scripts

### Python: Analyze with Retry

```python
import requests
import time
import json

def analyze_with_retry(base_url, payload, max_retries=3):
    """Analyze diagram with automatic retry on 503."""
    
    headers = {
        'Content-Type': 'application/json',
        'Accept-Version': '1.0'
    }
    
    for attempt in range(max_retries):
        try:
            response = requests.post(
                f'{base_url}/v1/analyze',
                json=payload,
                headers=headers,
                timeout=10
            )
            
            # Success
            if response.status_code == 200:
                return response.json()
            
            # Rate limited
            if response.status_code == 429:
                reset = int(response.headers.get('X-RateLimit-Reset', 0))
                wait = max(reset - time.time(), 1)
                print(f"Rate limited. Waiting {wait:.1f}s...")
                time.sleep(wait + 0.1)
                continue
            
            # Server busy
            if response.status_code == 503:
                wait = 2 ** attempt  # Exponential backoff
                print(f"Server busy. Waiting {wait}s...")
                time.sleep(wait)
                continue
            
            raise Exception(f"HTTP {response.status_code}: {response.text}")
        
        except requests.Timeout:
            if attempt < max_retries - 1:
                wait = 2 ** attempt
                print(f"Timeout. Retrying in {wait}s...")
                time.sleep(wait)
            else:
                raise
    
    raise Exception("Max retries exceeded")

# Usage
payload = {
    "code": "graph TD; A[Start]-->B[End]",
    "config": {
        "schema-version": "v1",
        "rules": {}
    }
}

result = analyze_with_retry('http://localhost:8080', payload)
print(json.dumps(result, indent=2))
```

---

