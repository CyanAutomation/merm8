package api

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/CyanAutomation/merm8/internal/output/sarif"
	"github.com/CyanAutomation/merm8/internal/rules"
)

// openapi contains the OpenAPI specification as a map that gets marshaled to JSON
// This avoids YAML parsing complexity while keeping standard library only
var openapi = map[string]interface{}{
	"openapi": "3.0.0",
	"info": map[string]interface{}{
		"title":       "merm8 - Mermaid Lint API",
		"description": "A deterministic Mermaid static analysis engine that validates and lints Mermaid diagrams.\n\n- Accepts Mermaid code via HTTP POST\n- Validates syntax using the official Mermaid parser\n- Returns structured syntax errors if invalid\n- Runs deterministic lint rules on valid diagrams\n- Returns structured lint results with metrics",
		"version":     "1.0.0",
		"contact": map[string]interface{}{
			"name": "merm8 Project",
			"url":  "https://github.com/CyanAutomation/merm8",
		},
		"license": map[string]interface{}{
			"name": "License",
			"url":  "https://github.com/CyanAutomation/merm8/blob/main/LICENSE",
		},
	},
	"servers": []map[string]interface{}{
		{
			"url":         "http://localhost:8080",
			"description": "Local development server",
		},
	},
	"tags": []map[string]interface{}{
		{
			"name":        "Linting",
			"description": "Mermaid diagram analysis and linting",
		},
		{
			"name":        "Documentation",
			"description": "API documentation and specification",
		},
		{
			"name":        "Probes",
			"description": "Liveness and readiness probe endpoints",
		},
	},
	"paths": map[string]interface{}{
		"/health": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Liveness probe alias",
				"description": "Legacy alias for canonical /v1/healthz; scheduled for removal in v1.2.0 (Q2 2026). Returns process liveness status (dependency checks are handled by /v1/ready).",
				"operationId": "getHealth",
				"deprecated":  true,
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Process is healthy",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"status": map[string]interface{}{"type": "string", "example": "ok"},
									},
								},
							},
						},
					},
				},
			},
		},
		"/v1/healthz": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Liveness probe (canonical v1)",
				"description": "Canonical v1 process liveness-only probe. Returns process liveness status (dependency checks are handled by /ready).",
				"operationId": "getHealthz",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Process is healthy",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"status": map[string]interface{}{"type": "string", "example": "ok"},
									},
								},
							},
						},
					},
				},
			},
		},
		"/v1/ready": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Readiness probe",
				"description": "Dependency/readiness-only probe. Returns readiness status for required dependencies (including parser checks when available) and may return 503 when not ready.",
				"operationId": "getReady",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Service is ready",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ReadyResponse",
								},
							},
						},
					},
					"503": map[string]interface{}{
						"description": "Service is not ready",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ReadyErrorResponse",
								},
								"example": map[string]interface{}{
									"status": "not_ready",
									"error": map[string]interface{}{
										"code":    "not_ready",
										"message": "parser script not found",
									},
								},
							},
						},
					},
				},
			},
		},

		"/info": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Service and parser runtime metadata",
				"description": "Returns service/app version metadata (when configured), parser and Mermaid versions, plus parser-recognized and lint-supported diagram families.",
				"operationId": "getInfo",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Service metadata",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/InfoResponse",
								},
							},
						},
					},
				},
			},
		},

		"/metrics": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Prometheus metrics",
				"description": "Returns service metrics in Prometheus text exposition format. Includes request_total, request_duration_seconds, analyze_requests_total, and parser_duration_seconds metric families.",
				"operationId": "getMetrics",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Metrics in Prometheus text format",
						"content": map[string]interface{}{
							"text/plain": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "string",
								},
							},
						},
					},
					"501": map[string]interface{}{
						"description": "Metrics exporter not configured",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
							},
						},
					},
				},
			},
		},

		"/v1/internal/metrics": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Internal analyze outcome counters",
				"description": "Returns internal JSON counters for analyze outcomes: valid_success, syntax_error, and parser failure categories timeout/subprocess/decode/contract/internal.",
				"operationId": "getInternalMetrics",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Internal analyze outcome counters",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/InternalMetricsResponse",
								},
							},
						},
					},
				},
			},
		},
		"/internal/metrics": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Internal analyze outcome counters (legacy alias)",
				"description": "Deprecated compatibility alias for GET /v1/internal/metrics. Scheduled for removal in v1.2.0 (Q2 2026).",
				"operationId": "getInternalMetricsLegacy",
				"deprecated":  true,
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Internal analyze outcome counters",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/InternalMetricsResponse",
								},
							},
						},
					},
				},
			},
		},

		"/v1/rules": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "List built-in lint rules",
				"description": "Returns metadata for rules implemented by the currently registered runtime rule set, including defaults and configurable options.",
				"operationId": "listRules",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Implemented rule metadata",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/RulesResponse",
								},
							},
						},
					},
				},
			},
		},
		"/v1/rules/schema": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Get JSON Schema for lint rule configuration",
				"description": "Returns a JSON Schema generated from the currently implemented rule config registry. The schema validates only the canonical versioned config payload format and excludes unimplemented rules.",
				"operationId": "getRuleConfigSchema",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Rule configuration schema",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"schema"},
									"properties": map[string]interface{}{
										"schema": map[string]interface{}{
											"$ref": "#/components/schemas/RuleConfigSchema",
										},
									},
								},
							},
						},
					},
				},
			},
		},

		"/diagram-types": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "List parser-recognized and lint-supported diagram types",
				"description": "Returns diagram families/types recognized by the parser and currently lint-supported by registered rules.",
				"operationId": "listDiagramTypes",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Diagram type support matrix",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/DiagramTypesResponse",
								},
							},
						},
					},
				},
			},
		},
		"/v1/analyze": map[string]interface{}{
			"post": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Analyze and lint a Mermaid diagram",
				"description": "Validates Mermaid code syntax and runs deterministic lint rules.\n\nMaximum request body size is 1 MiB. Oversized payloads return HTTP 413.\n\nOperational note: tune `PARSER_CONCURRENCY_LIMIT` and `PARSER_MAX_OLD_SPACE_MB` (documented in API_GUIDE.md under Operational environment variables). When parser concurrency is exhausted, `/analyze` returns HTTP 503 with `error.code=server_busy`; parser subprocess heap is capped with Node `--max-old-space-size`.\n\nSupports source comment suppressions such as `%% merm8-disable <rule-id>`, `%% merm8-disable all`, and `%% merm8-disable-next-line <rule-id>`.\n\nReturns syntax errors as semantic responses (`valid=false`) if parsing fails, or lint results if parsing succeeds.\n\nParser infrastructure failures return machine-readable API error codes:\n- `parser_subprocess_error`\n- `parser_decode_error`\n- `parser_contract_violation`\n- `parser_timeout` (HTTP 504)\n- `parser_memory_limit` (HTTP 500)",
				"operationId": "analyzeCode",
				"requestBody": map[string]interface{}{
					"required": true,
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{
								"$ref": "#/components/schemas/AnalyzeRequest",
							},
							"examples": map[string]interface{}{
								"simple": map[string]interface{}{
									"summary": "Simple graph",
									"value": map[string]interface{}{
										"code": "graph TD\n  A[Start] --> B[End]",
									},
								},
								"withConfigVersioned": map[string]interface{}{
									"summary": "Preferred versioned config payload (recommended)",
									"value": map[string]interface{}{
										"code": "graph LR\n  A --> B\n  B --> C",
										"config": map[string]interface{}{
											"schema-version": "v1",
											"rules": map[string]interface{}{
												"max-fanout": map[string]interface{}{
													"enabled":               true,
													"limit":                 2,
													"severity":              "error",
													"suppression-selectors": []interface{}{"node:A"},
												},
											},
										},
									},
								},
								"withConfigNestedLegacy": map[string]interface{}{
									"summary": "Legacy nested rules payload (migration support)",
									"value": map[string]interface{}{
										"code": "graph LR\n  A --> B\n  B --> C",
										"config": map[string]interface{}{
											"rules": map[string]interface{}{
												"max-fanout": map[string]interface{}{
													"enabled":               true,
													"limit":                 2,
													"severity":              "error",
													"suppression-selectors": []interface{}{"node:A"},
												},
											},
										},
									},
								},
								"withConfigFlatLegacy": map[string]interface{}{
									"summary": "Legacy flat rule payload (migration support)",
									"value": map[string]interface{}{
										"code": "graph LR\n  A --> B\n  B --> C",
										"config": map[string]interface{}{
											"max-fanout": map[string]interface{}{
												"enabled":               true,
												"limit":                 2,
												"severity":              "error",
												"suppression-selectors": []interface{}{"node:A"},
											},
										},
									},
								},
								"syntaxError": map[string]interface{}{
									"summary": "Invalid Mermaid syntax",
									"value": map[string]interface{}{
										"code": "graph TD\n  A --> B -->",
									},
								},
							},
						},
					},
				},
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Analysis completed. For HTTP 200: `error` is always null, `issues` is always present as an array, and `syntax-error` is populated only for parser syntax failures (otherwise null).",
						"headers": map[string]interface{}{
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     119,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"examples": map[string]interface{}{
									"validDiagram": map[string]interface{}{
										"summary": "Valid diagram with no issues",
										"value": map[string]interface{}{
											"valid":          true,
											"diagram-type":   "flowchart",
											"lint-supported": true,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              2,
												"edge-count":              1,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               1,
												"max-fanout":              1,
												"diagram-type":            "flowchart",
												"direction":               "TD",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
										},
									},
									"syntaxError": map[string]interface{}{
										"summary": "Syntax error response",
										"value": map[string]interface{}{
											"valid":          false,
											"diagram-type":   "flowchart",
											"lint-supported": true,
											"syntax-error": map[string]interface{}{
												"message": "Unexpected token '>'",
												"line":    2,
												"column":  12,
											},
											"issues": []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "flowchart",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
										},
									},
									"sequenceDiagram": map[string]interface{}{
										"summary": "Parsed sequence diagram (lint currently unsupported)",
										"value": map[string]interface{}{
											"valid":          false,
											"diagram-type":   "sequence",
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "sequence",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
										},
									},
									"withIssues": map[string]interface{}{
										"summary": "Valid diagram with lint issues",
										"value": map[string]interface{}{
											"valid":          true,
											"diagram-type":   "flowchart",
											"lint-supported": true,
											"syntax-error":   nil,
											"issues": []interface{}{
												map[string]interface{}{
													"rule-id":  "no-disconnected-nodes",
													"severity": "error",
													"message":  "Node 'D' is not connected to the graph",
												},
												map[string]interface{}{
													"rule-id":  "max-fanout",
													"severity": "warning",
													"message":  "Node 'A' has fanout 6, exceeding limit of 5",
													"line":     2,
													"column":   2,
												},
											},
											"metrics": map[string]interface{}{
												"node-count":              4,
												"edge-count":              2,
												"disconnected-node-count": 1,
												"duplicate-node-count":    0,
												"max-fanin":               1,
												"max-fanout":              2,
												"diagram-type":            "flowchart",
												"direction":               "TD",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{"error": 1, "warning": 1},
													"by-rule":     map[string]interface{}{"no-disconnected-nodes": 1, "max-fanout": 1},
												},
											},
										},
									},
								},
							},
						},
					},
					"400": map[string]interface{}{
						"description": "Bad request (invalid JSON, missing required field, or invalid config). For non-200 API failures, `valid=false`, `syntax-error=null`, `issues=[]`, and `error` is populated.",
						"headers": map[string]interface{}{
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     119,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"examples": map[string]interface{}{
									"missingCode": map[string]interface{}{
										"summary": "Missing 'code' field",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "missing_code",
												"message": "field 'code' is required",
											},
										},
									},
									"invalidJSON": map[string]interface{}{
										"summary": "Invalid JSON body",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "invalid_json",
												"message": "invalid JSON body",
											},
										},
									},
									"unknownRule": map[string]interface{}{
										"summary": "Unknown rule in config",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "unknown_rule",
												"message": "unknown rule: unknown-rule",
												"details": map[string]interface{}{
													"path":      "config.rules.unknown-rule",
													"supported": []interface{}{"max-depth", "max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids"},
												},
											},
										},
									},
									"unknownOption": map[string]interface{}{
										"summary": "Unknown option in rule config",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "unknown_option",
												"message": "unknown option: threshold",
												"details": map[string]interface{}{
													"path":      "config.rules.max-fanout.threshold",
													"supported": []interface{}{"enabled", "limit", "severity", "suppression-selectors"},
												},
											},
										},
									},
									"invalidOption": map[string]interface{}{
										"summary": "Invalid option value",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "invalid_option",
												"message": "invalid option value for limit",
												"details": map[string]interface{}{"path": "config.rules.max-fanout.limit"},
											},
										},
									},
								},
							},
						},
					},
					"413": map[string]interface{}{
						"description": "Request entity too large (body exceeds 1 MiB). For non-200 API failures, `error` is populated, `syntax-error` is null, and `issues` is an empty array.",
						"headers": map[string]interface{}{
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     119,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"example": map[string]interface{}{
									"valid":          false,
									"lint-supported": false,
									"syntax-error":   nil,
									"issues":         []interface{}{},
									"metrics": map[string]interface{}{
										"node-count":              0,
										"edge-count":              0,
										"disconnected-node-count": 0,
										"duplicate-node-count":    0,
										"max-fanin":               0,
										"max-fanout":              0,
										"diagram-type":            "unknown",
										"issue-counts": map[string]interface{}{
											"by-severity": map[string]interface{}{},
											"by-rule":     map[string]interface{}{},
										},
									},
									"error": map[string]interface{}{
										"code":    "request_too_large",
										"message": "request body exceeds 1 MiB limit",
									},
								},
							},
						},
					},

					"429": map[string]interface{}{
						"description": "Rate limited. For non-200 API failures, `error` is populated, `syntax-error` is null, and `issues` is an empty array.",
						"headers": map[string]interface{}{
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     0,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"example": map[string]interface{}{
									"valid":          false,
									"lint-supported": false,
									"syntax-error":   nil,
									"issues":         []interface{}{},
									"metrics": map[string]interface{}{
										"node-count":              0,
										"edge-count":              0,
										"disconnected-node-count": 0,
										"duplicate-node-count":    0,
										"max-fanin":               0,
										"max-fanout":              0,
										"diagram-type":            "unknown",
										"issue-counts": map[string]interface{}{
											"by-severity": map[string]interface{}{},
											"by-rule":     map[string]interface{}{},
										},
									},
									"error": map[string]interface{}{
										"code":    "rate_limited",
										"message": "rate limit exceeded",
									},
								},
							},
						},
					},
					"503": map[string]interface{}{
						"description": "Service unavailable. For non-200 API failures, `error` is populated, `syntax-error` is null, and `issues` is an empty array.",
						"headers": map[string]interface{}{
							"Retry-After": map[string]interface{}{
								"description": "Suggested delay before retrying a `server_busy` response. Value is either delta-seconds or an HTTP-date.",
								"schema":      map[string]interface{}{"type": "string"},
								"example":     "5",
							},
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     119,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"example": map[string]interface{}{
									"valid":          false,
									"lint-supported": false,
									"syntax-error":   nil,
									"issues":         []interface{}{},
									"metrics": map[string]interface{}{
										"node-count":              0,
										"edge-count":              0,
										"disconnected-node-count": 0,
										"duplicate-node-count":    0,
										"max-fanin":               0,
										"max-fanout":              0,
										"diagram-type":            "unknown",
										"issue-counts": map[string]interface{}{
											"by-severity": map[string]interface{}{},
											"by-rule":     map[string]interface{}{},
										},
									},
									"error": map[string]interface{}{
										"code":    "server_busy",
										"message": "parser concurrency limit reached; try again",
									},
								},
							},
						},
					},
					"500": map[string]interface{}{
						"description": "Internal server error. For non-200 API failures, `error` is populated, `syntax-error` is null, and `issues` is an empty array.",
						"headers": map[string]interface{}{
							"X-RateLimit-Limit": map[string]interface{}{
								"description": "Maximum number of requests allowed in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     120,
							},
							"X-RateLimit-Remaining": map[string]interface{}{
								"description": "Number of requests remaining in the current time window",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     119,
							},
							"X-RateLimit-Reset": map[string]interface{}{
								"description": "Unix timestamp when the rate limit window resets",
								"schema":      map[string]interface{}{"type": "integer"},
								"example":     1234567890,
							},
						},
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"examples": map[string]interface{}{
									"subprocess": map[string]interface{}{
										"summary": "Parser subprocess failure",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "parser_subprocess_error",
												"message": "parser subprocess failed",
											},
										},
									},
									"decode": map[string]interface{}{
										"summary": "Parser malformed output",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "parser_decode_error",
												"message": "parser returned malformed output",
											},
										},
									},
									"contract": map[string]interface{}{
										"summary": "Parser contract violation",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "parser_contract_violation",
												"message": "parser response violated service contract",
											},
										},
									},
									"internal": map[string]interface{}{
										"summary": "Generic internal failure",
										"value": map[string]interface{}{
											"valid":          false,
											"lint-supported": false,
											"syntax-error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node-count":              0,
												"edge-count":              0,
												"disconnected-node-count": 0,
												"duplicate-node-count":    0,
												"max-fanin":               0,
												"max-fanout":              0,
												"diagram-type":            "unknown",
												"issue-counts": map[string]interface{}{
													"by-severity": map[string]interface{}{},
													"by-rule":     map[string]interface{}{},
												},
											},
											"error": map[string]interface{}{
												"code":    "internal_error",
												"message": "internal server error",
											},
										},
									},
								},
							},
						},
					},
					"504": map[string]interface{}{
						"description": "Parser timeout. For non-200 API failures, `error` is populated, `syntax-error` is null, and `issues` is an empty array.",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/AnalyzeResponse",
								},
								"example": map[string]interface{}{
									"valid":          false,
									"lint-supported": false,
									"syntax-error":   nil,
									"issues":         []interface{}{},
									"metrics": map[string]interface{}{
										"node-count":              0,
										"edge-count":              0,
										"disconnected-node-count": 0,
										"duplicate-node-count":    0,
										"max-fanin":               0,
										"max-fanout":              0,
										"diagram-type":            "unknown",
										"issue-counts": map[string]interface{}{
											"by-severity": map[string]interface{}{},
											"by-rule":     map[string]interface{}{},
										},
									},
									"error": map[string]interface{}{
										"code":    "parser_timeout",
										"message": "parser timed out while validating Mermaid code",
									},
								},
							},
						},
					},
				},
			},
		},
		"/v1/analyze/sarif": map[string]interface{}{
			"post": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Analyze and lint a Mermaid diagram (SARIF output)",
				"description": "Runs the same analysis pipeline as POST /v1/analyze, returning SARIF 2.1.0 when analysis is valid.",
				"operationId": "analyzeCodeSARIF",
				"requestBody": map[string]interface{}{
					"required": true,
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeRequest"},
						},
					},
				},
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "SARIF 2.1.0 report",
						"content": map[string]interface{}{
							"application/sarif+json": map[string]interface{}{
								"schema": map[string]interface{}{"$ref": "#/components/schemas/SARIFReport"},
								"examples": map[string]interface{}{
									"validDiagram": map[string]interface{}{
										"summary": "Valid diagram with no issues",
										"value": map[string]interface{}{
											"version": "2.1.0",
											"runs": []interface{}{map[string]interface{}{
												"tool":    map[string]interface{}{"driver": map[string]interface{}{"name": "merm8"}},
												"results": []interface{}{},
											}},
										},
									},
									"diagramWithIssues": map[string]interface{}{
										"summary": "Diagram with lint violations",
										"value": map[string]interface{}{
											"version": "2.1.0",
											"runs": []interface{}{map[string]interface{}{
												"tool": map[string]interface{}{"driver": map[string]interface{}{"name": "merm8"}},
												"results": []interface{}{
													map[string]interface{}{
														"ruleId":  "no-cycles",
														"level":   "error",
														"message": map[string]interface{}{"text": "Cycle detected"},
													},
													map[string]interface{}{
														"ruleId":  "max-fanout",
														"level":   "warning",
														"message": map[string]interface{}{"text": "Node 'A' has fanout 6, exceeding limit of 5"},
													},
												},
											}},
										},
									},
								},
							},
						},
					},
					"400": map[string]interface{}{
						"description": "Bad request",
						"content":     map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}},
					},
					"413": map[string]interface{}{
						"description": "Request too large",
						"content":     map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}},
					},
					"500": map[string]interface{}{
						"description": "Parser/service internal failure",
						"content":     map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}},
					},
					"504": map[string]interface{}{
						"description": "Parser timeout",
						"content":     map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}},
					},
				},
			},
		},
		"/v1/spec": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Documentation"},
				"summary":     "Get OpenAPI specification",
				"description": "Returns the OpenAPI 3.0 specification for this API in JSON format",
				"operationId": "getSpec",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "OpenAPI specification",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
								},
							},
						},
					},
				},
			},
		},
		"/v1/docs": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Documentation"},
				"summary":     "Interactive API documentation",
				"description": "Serve Swagger UI for interactive API exploration and testing",
				"operationId": "getDocs",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Swagger UI HTML page",
						"content": map[string]interface{}{
							"text/html": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "string",
								},
							},
						},
					},
				},
			},
		},

		"/analyze": map[string]interface{}{
			"post": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Analyze and lint a Mermaid diagram (legacy alias)",
				"description": "Deprecated compatibility alias for POST /v1/analyze. Scheduled for removal in v1.2.0 (Q2 2026).",
				"operationId": "analyzeCodeLegacy",
				"deprecated":  true,
				"requestBody": map[string]interface{}{
					"required": true,
					"content": map[string]interface{}{
						"application/json": map[string]interface{}{
							"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeRequest"},
						},
					},
				},
				"responses": map[string]interface{}{
					"200": map[string]interface{}{"description": "Success", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}}},
					"400": map[string]interface{}{"description": "Bad request", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}}},
					"413": map[string]interface{}{"description": "Request too large", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}}},
					"500": map[string]interface{}{"description": "Parser/service internal failure", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}}},
					"504": map[string]interface{}{"description": "Parser timeout", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/AnalyzeResponse"}}}},
				},
			},
		},
		"/rules": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "List built-in lint rules (legacy alias)",
				"description": "Deprecated compatibility alias for GET /v1/rules. Scheduled for removal in v1.2.0 (Q2 2026).",
				"operationId": "listRulesLegacy",
				"deprecated":  true,
				"responses": map[string]interface{}{
					"200": map[string]interface{}{"description": "Built-in rule metadata", "content": map[string]interface{}{"application/json": map[string]interface{}{"schema": map[string]interface{}{"$ref": "#/components/schemas/RulesResponse"}}}},
				},
			},
		},
		"/rules/schema": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Get JSON Schema for lint rule configuration (legacy alias)",
				"description": "Deprecated compatibility alias for GET /v1/rules/schema. Scheduled for removal in v1.2.0 (Q2 2026).",
				"operationId": "getRuleConfigSchemaLegacy",
				"deprecated":  true,
				"responses": map[string]interface{}{
					"200": map[string]interface{}{"description": "Rule configuration schema"},
				},
			},
		},
		"/spec": map[string]interface{}{
			"get": map[string]interface{}{"tags": []string{"Documentation"}, "summary": "Get OpenAPI specification (legacy alias)", "description": "Deprecated compatibility alias for GET /v1/spec. Scheduled for removal in v1.2.0 (Q2 2026).", "operationId": "getSpecLegacy", "deprecated": true, "responses": map[string]interface{}{"200": map[string]interface{}{"description": "OpenAPI specification"}}},
		},
		"/docs": map[string]interface{}{
			"get": map[string]interface{}{"tags": []string{"Documentation"}, "summary": "Interactive API documentation (legacy alias)", "description": "Deprecated compatibility alias for GET /v1/docs. Scheduled for removal in v1.2.0 (Q2 2026).", "operationId": "getDocsLegacy", "deprecated": true, "responses": map[string]interface{}{"200": map[string]interface{}{"description": "Swagger UI HTML page"}}},
		},
	},
	"components": map[string]interface{}{
		"schemas": map[string]interface{}{
			"SARIFReport": map[string]interface{}{
				"type":     "object",
				"required": []string{"version", "runs"},
				"properties": map[string]interface{}{
					"$schema": map[string]interface{}{"type": "string", "example": "https://json.schemastore.org/sarif-2.1.0.json"},
					"version": map[string]interface{}{"type": "string", "enum": []string{"2.1.0"}},
					"runs": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "object"},
					},
				},
				"description": sarif.SeverityMappingDoc,
			},

			"ReadyResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"status"},
				"properties": map[string]interface{}{
					"status":          map[string]interface{}{"type": "string", "example": "ready"},
					"parser_version":  map[string]interface{}{"type": "string"},
					"mermaid_version": map[string]interface{}{"type": "string"},
				},
			},

			"ReadyErrorResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"status", "error"},
				"properties": map[string]interface{}{
					"status": map[string]interface{}{"type": "string", "example": "not_ready"},
					"error":  map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},

			"InfoResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"parser-recognized", "lint-supported"},
				"properties": map[string]interface{}{
					"service-version":        map[string]interface{}{"type": "string"},
					"parser-version":         map[string]interface{}{"type": "string"},
					"mermaid-version":        map[string]interface{}{"type": "string"},
					"parser-timeout-seconds": map[string]interface{}{"type": "integer"},
					"parser-recognized": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},
					"lint-supported": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},

					// Deprecated snake_case compatibility aliases.
					"service_version":        map[string]interface{}{"type": "string", "deprecated": true, "description": "Deprecated alias for service-version; remove usage before next major version."},
					"parser_version":         map[string]interface{}{"type": "string", "deprecated": true, "description": "Deprecated alias for parser-version; remove usage before next major version."},
					"mermaid_version":        map[string]interface{}{"type": "string", "deprecated": true, "description": "Deprecated alias for mermaid-version; remove usage before next major version."},
					"parser_timeout_seconds": map[string]interface{}{"type": "integer", "deprecated": true, "description": "Deprecated alias for parser-timeout-seconds; remove usage before next major version."},
					"parser_recognized": map[string]interface{}{
						"type":        "array",
						"deprecated":  true,
						"description": "Deprecated alias for parser-recognized; remove usage before next major version.",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},
					"lint_supported": map[string]interface{}{
						"type":        "array",
						"deprecated":  true,
						"description": "Deprecated alias for lint-supported; remove usage before next major version.",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},
				},
			},

			"InternalMetricsResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"analyze", "parser"},
				"properties": map[string]interface{}{
					"analyze": map[string]interface{}{
						"type":     "object",
						"required": []string{"valid_success", "syntax_error"},
						"properties": map[string]interface{}{
							"valid_success": map[string]interface{}{"type": "integer", "format": "int64"},
							"syntax_error":  map[string]interface{}{"type": "integer", "format": "int64"},
						},
					},
					"parser": map[string]interface{}{
						"type":     "object",
						"required": []string{"timeout", "subprocess", "decode", "contract", "internal"},
						"properties": map[string]interface{}{
							"timeout":    map[string]interface{}{"type": "integer", "format": "int64"},
							"subprocess": map[string]interface{}{"type": "integer", "format": "int64"},
							"decode":     map[string]interface{}{"type": "integer", "format": "int64"},
							"contract":   map[string]interface{}{"type": "integer", "format": "int64"},
							"internal":   map[string]interface{}{"type": "integer", "format": "int64"},
						},
					},
				},
			},

			"DiagramTypesResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"parser-recognized", "lint-supported"},
				"properties": map[string]interface{}{
					"parser-recognized": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},
					"lint-supported": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
							"enum": []string{"flowchart", "sequence", "class", "er", "state"},
						},
					},
				},
			},
			"RulesResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"rules"},
				"properties": map[string]interface{}{
					"rules": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"$ref": "#/components/schemas/RuleMetadata",
						},
					},
				},
			},
			"RuleMetadata": map[string]interface{}{
				"type":     "object",
				"required": []string{"id", "severity", "description", "default-config", "configurable-options"},
				"properties": map[string]interface{}{
					"id":             map[string]interface{}{"type": "string", "example": "max-fanout"},
					"severity":       map[string]interface{}{"type": "string", "enum": []string{"error", "warning", "info"}},
					"description":    map[string]interface{}{"type": "string"},
					"default-config": map[string]interface{}{"type": "object", "additionalProperties": true},
					"configurable-options": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"$ref": "#/components/schemas/RuleOption",
						},
					},
				},
			},
			"RuleOption": map[string]interface{}{
				"type":     "object",
				"required": []string{"name", "type", "description"},
				"properties": map[string]interface{}{
					"name":        map[string]interface{}{"type": "string"},
					"type":        map[string]interface{}{"type": "string"},
					"description": map[string]interface{}{"type": "string"},
					"constraints": map[string]interface{}{"type": "string"},
				},
			},
			"RuleConfigSchema": rules.ConfigJSONSchema(),
			"AnalyzeRequest": map[string]interface{}{
				"type":     "object",
				"required": []string{"code"},
				"properties": map[string]interface{}{
					"code": map[string]interface{}{
						"type":        "string",
						"description": "Mermaid diagram code to analyze. Total request body must be 1 MiB or smaller. Supports suppression comments like `%% merm8-disable max-fanout` and `%% merm8-disable all`.",
						"example":     "graph TD\n  A[Start] --> B[Process]\n  B --> C[End]",
					},
					"config": map[string]interface{}{
						"$ref":        "#/components/schemas/RuleConfigSchema",
						"description": "Optional lint rule configuration. Canonical format is {\"schema-version\":\"v1\",\"rules\":{...}}. Phase 1 accepts legacy flat/nested/snake_case config with deprecation warnings; Phase 2 rejects legacy config with 400 deprecated_config_format.",
						"example": map[string]interface{}{
							"schema-version": "v1",
							"rules": map[string]interface{}{
								"max-fanout": map[string]interface{}{
									"enabled":               true,
									"limit":                 3,
									"severity":              "error",
									"suppression-selectors": []interface{}{"node:A"},
								},
								"no-disconnected-nodes": map[string]interface{}{
									"enabled":  false,
									"severity": "info",
								},
							},
						},
					},
					"parser": map[string]interface{}{
						"type":        "object",
						"description": "Optional per-request parser execution limits. Values are bounded and validated before parsing.",
						"properties": map[string]interface{}{
							"timeout_seconds":  map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 60},
							"max_old_space_mb": map[string]interface{}{"type": "integer", "minimum": 128, "maximum": 4096},
						},
					},
				},
			},

			"Error": map[string]interface{}{
				"type":     "object",
				"required": []string{"code", "message"},
				"properties": map[string]interface{}{
					"code": map[string]interface{}{
						"type":        "string",
						"description": "Machine-readable error code",
						"example":     "invalid_json",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable error message",
						"example":     "invalid JSON body",
					},
					"details": map[string]interface{}{
						"type":                 "object",
						"description":          "Optional context-specific error details object (e.g. path, supported)",
						"additionalProperties": true,
					},
				},
			},
			"AnalyzeResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"valid", "lint-supported", "issues", "syntax-error", "metrics"},
				"properties": map[string]interface{}{
					"valid": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the Mermaid code is syntactically valid and lint-supported (unsupported parsed families return valid=false).",
					},
					"diagram-type": map[string]interface{}{
						"type":        "string",
						"description": "Normalized Mermaid diagram type (flowchart, sequence, class, er, state, unknown).",
						"enum":        []string{"flowchart", "sequence", "class", "er", "state", "unknown"},
						"example":     "flowchart",
					},
					"lint-supported": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether lint rules are currently implemented for the reported diagram type. Syntax-error responses use parser-detected type fallback when available.",
					},
					"syntax-error": map[string]interface{}{
						"$ref":        "#/components/schemas/SyntaxError",
						"description": "Syntax error details if `valid` is false. Null if valid.",
						"nullable":    true,
					},
					"issues": map[string]interface{}{
						"type":        "array",
						"description": "Lint rule violations found in the diagram",
						"items": map[string]interface{}{
							"$ref": "#/components/schemas/Issue",
						},
					},
					"warnings": map[string]interface{}{
						"type":        "array",
						"description": "Deprecation warnings emitted when legacy config keys/shapes are used.",
						"items":       map[string]interface{}{"type": "string"},
					},
					"error": map[string]interface{}{
						"$ref":        "#/components/schemas/Error",
						"description": "API-level error details for non-200 responses.",
						"nullable":    true,
					},
					"metrics": map[string]interface{}{
						"$ref":        "#/components/schemas/Metrics",
						"description": "Aggregate statistics about the diagram. Present for successful analyze responses, including syntax errors (zeroed counters with fallback diagram-type) and parsed but lint-unsupported families.",
						"nullable":    true,
					},
				},
			},
			"SyntaxError": map[string]interface{}{
				"type":     "object",
				"required": []string{"message", "line", "column"},
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Error message from the Mermaid parser",
						"example":     "Unexpected token '>'",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "1-based line number where the error occurred",
						"example":     2,
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "0-based column number where the error occurred",
						"example":     12,
					},
				},
			},
			"Issue": map[string]interface{}{
				"type":     "object",
				"required": []string{"rule-id", "severity", "message", "fingerprint"},
				"properties": map[string]interface{}{
					"rule-id": map[string]interface{}{
						"type":        "string",
						"description": "Identifier of the lint rule that triggered",
						"example":     "no-disconnected-nodes",
					},
					"severity": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"error", "warning", "info"},
						"description": "Severity level of the issue",
					},
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable description of the issue",
						"example":     "Node 'isolated' is not connected to the graph",
					},
					"line": map[string]interface{}{
						"type":        "integer",
						"description": "1-based line number where issue is located when known. Omitted when unknown.",
						"example":     5,
						"nullable":    true,
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "0-based column number where issue is located when known. Omitted when unknown.",
						"example":     2,
						"nullable":    true,
					},
					"fingerprint": map[string]interface{}{
						"type":        "string",
						"description": "Deterministic SHA-256 hash of the normalized issue signature for CI tracking and dedupe.",
						"example":     "b57b8d8ac7c95f6e8f6e30a38ae2e6bcd2c6576d7f20d3155014a4f14f7a8f46",
					},
					"context": map[string]interface{}{
						"$ref":        "#/components/schemas/IssueContext",
						"description": "Optional grouping context for node-scoped findings; omitted when no grouping applies.",
						"nullable":    true,
					},
				},
			},
			"IssueContext": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"subgraph-id": map[string]interface{}{
						"type":        "string",
						"description": "ID of the subgraph containing the affected node.",
						"example":     "cluster-1",
					},
					"subgraph-label": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable label of the subgraph containing the affected node.",
						"example":     "Core Services",
					},
				},
			},
			"Metrics": map[string]interface{}{
				"type":     "object",
				"required": []string{"node-count", "edge-count", "disconnected-node-count", "duplicate-node-count", "max-fanin", "max-fanout", "diagram-type"},
				"properties": map[string]interface{}{
					"node-count": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of nodes in the diagram",
						"example":     5,
					},
					"edge-count": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of edges (connections) in the diagram",
						"example":     4,
					},
					"disconnected-node-count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of nodes with neither incoming nor outgoing edges",
						"example":     1,
					},
					"duplicate-node-count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of duplicate node declarations beyond first occurrences",
						"example":     0,
					},
					"max-fanin": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of incoming edges to any single node",
						"example":     3,
					},
					"max-fanout": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of outgoing edges from any single node",
						"example":     2,
					},
					"diagram-type": map[string]interface{}{
						"type":        "string",
						"description": "Diagram type copied from parsed metadata",
						"enum":        []string{"flowchart", "sequence", "class", "er", "state", "unknown"},
						"example":     "flowchart",
					},
					"direction": map[string]interface{}{
						"type":        "string",
						"description": "Diagram direction when provided by parser",
						"example":     "TD",
					},
					"issue-counts": map[string]interface{}{
						"$ref":        "#/components/schemas/IssueCounts",
						"description": "Issue count aggregations by severity and rule ID",
					},
				},
			},
			"IssueCounts": map[string]interface{}{
				"type":     "object",
				"required": []string{"by-severity", "by-rule"},
				"properties": map[string]interface{}{
					"by-severity": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "integer"},
					},
					"by-rule": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "integer"},
					},
				},
			},
		},
	},
}

// getServersFromEnv constructs the servers array based on environment configuration.
// If MERM8_API_URL is set, it will be used as the primary server.
// Otherwise, defaults to localhost:8080 for development.
func getServersFromEnv() []map[string]interface{} {
	apiURL := strings.TrimSpace(os.Getenv("MERM8_API_URL"))

	if apiURL == "" {
		// Default development servers
		return []map[string]interface{}{
			{
				"url":         "http://localhost:8080",
				"description": "Local development server",
			},
		}
	}

	// Production server from environment
	return []map[string]interface{}{
		{
			"url":         apiURL,
			"description": "Production server",
		},
		{
			"url":         "http://localhost:8080",
			"description": "Local development server",
		},
	}
}

// OpenAPISpec returns the OpenAPI spec with dynamic servers based on environment.
func OpenAPISpec() map[string]interface{} {
	// Deep copy the openapi spec to avoid modifying the global
	specJSON, _ := json.Marshal(openapi)
	var spec map[string]interface{}
	json.Unmarshal(specJSON, &spec)

	// Update servers from environment
	spec["servers"] = getServersFromEnv()

	return spec
}
