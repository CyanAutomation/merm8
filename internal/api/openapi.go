package api

import (
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
		{
			"url":         "https://api.example.com",
			"description": "Production server (replace with actual URL)",
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
		"/healthz": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Liveness probe",
				"description": "Returns process liveness status.",
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
		"/ready": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Probes"},
				"summary":     "Readiness probe",
				"description": "Returns readiness status, including parser dependency checks when available.",
				"operationId": "getReady",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Service is ready",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"status": map[string]interface{}{"type": "string", "example": "ready"},
									},
								},
							},
						},
					},
					"503": map[string]interface{}{
						"description": "Service is not ready",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"status": map[string]interface{}{"type": "string", "example": "not_ready"},
										"error":  map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
				},
			},
		},

		"/rules": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "List built-in lint rules",
				"description": "Returns centralized metadata for all built-in rules, including defaults and configurable options.",
				"operationId": "listRules",
				"responses": map[string]interface{}{
					"200": map[string]interface{}{
						"description": "Built-in rule metadata",
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
		"/rules/schema": map[string]interface{}{
			"get": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Get JSON Schema for lint rule configuration",
				"description": "Returns a JSON Schema generated from the rule config registry. The schema validates both flat and nested config payload formats.",
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
		"/analyze": map[string]interface{}{
			"post": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Analyze and lint a Mermaid diagram",
				"description": "Validates Mermaid code syntax and runs deterministic lint rules.\n\nMaximum request body size is 1 MiB. Oversized payloads return HTTP 413.\n\nSupports source comment suppressions such as `%% merm8-disable <rule-id>`, `%% merm8-disable all`, and `%% merm8-disable-next-line <rule-id>`.\n\nReturns syntax errors as semantic responses (`valid=false`) if parsing fails, or lint results if parsing succeeds.\n\nParser infrastructure failures return machine-readable API error codes:\n- `parser_subprocess_error`\n- `parser_decode_error`\n- `parser_contract_violation`\n- `parser_timeout` (HTTP 504)",
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
								"withConfig": map[string]interface{}{
									"summary": "With lint configuration",
									"value": map[string]interface{}{
										"code": "graph LR\n  A --> B\n  B --> C",
										"config": map[string]interface{}{
											"rules": map[string]interface{}{
												"max-fanout": map[string]interface{}{
													"enabled":               true,
													"limit":                 2,
													"severity":              "error",
													"suppression_selectors": []interface{}{"node:A"},
												},
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
						"description": "Analysis completed (valid or syntax error)",
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
											"diagram_type":   "flowchart",
											"lint_supported": true,
											"syntax_error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node_count":              2,
												"edge_count":              1,
												"disconnected_node_count": 0,
												"duplicate_node_count":    0,
												"max_fanin":               1,
												"max_fanout":              1,
												"diagram_type":            "flowchart",
												"direction":               "TD",
												"issue_counts": map[string]interface{}{
													"by_severity": map[string]interface{}{},
													"by_rule":     map[string]interface{}{},
												},
											},
										},
									},
									"syntaxError": map[string]interface{}{
										"summary": "Syntax error response",
										"value": map[string]interface{}{
											"valid":          false,
											"lint_supported": false,
											"syntax_error": map[string]interface{}{
												"message": "Unexpected token '>'",
												"line":    2,
												"column":  12,
											},
											"issues": []interface{}{},
										},
									},
									"sequenceDiagram": map[string]interface{}{
										"summary": "Valid sequence diagram (lint currently unsupported)",
										"value": map[string]interface{}{
											"valid":          true,
											"diagram_type":   "sequence",
											"lint_supported": false,
											"syntax_error":   nil,
											"issues":         []interface{}{},
											"metrics": map[string]interface{}{
												"node_count":              0,
												"edge_count":              0,
												"disconnected_node_count": 0,
												"duplicate_node_count":    0,
												"max_fanin":               0,
												"max_fanout":              0,
												"diagram_type":            "sequence",
												"issue_counts": map[string]interface{}{
													"by_severity": map[string]interface{}{},
													"by_rule":     map[string]interface{}{},
												},
											},
										},
									},
									"withIssues": map[string]interface{}{
										"summary": "Valid diagram with lint issues",
										"value": map[string]interface{}{
											"valid":          true,
											"diagram_type":   "flowchart",
											"lint_supported": true,
											"syntax_error":   nil,
											"issues": []interface{}{
												map[string]interface{}{
													"rule_id":  "no-disconnected-nodes",
													"severity": "error",
													"message":  "Node 'D' is not connected to the graph",
												},
												map[string]interface{}{
													"rule_id":  "max-fanout",
													"severity": "warning",
													"message":  "Node 'A' has fanout 6, exceeding limit of 5",
													"line":     2,
													"column":   2,
												},
											},
											"metrics": map[string]interface{}{
												"node_count":              4,
												"edge_count":              2,
												"disconnected_node_count": 1,
												"duplicate_node_count":    0,
												"max_fanin":               1,
												"max_fanout":              2,
												"diagram_type":            "flowchart",
												"direction":               "TD",
												"issue_counts": map[string]interface{}{
													"by_severity": map[string]interface{}{"error": 1, "warning": 1},
													"by_rule":     map[string]interface{}{"no-disconnected-nodes": 1, "max-fanout": 1},
												},
											},
										},
									},
								},
							},
						},
					},
					"400": map[string]interface{}{
						"description": "Bad request (invalid JSON, missing required field, or invalid config)",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ErrorResponse",
								},
								"examples": map[string]interface{}{
									"missingCode": map[string]interface{}{
										"summary": "Missing 'code' field",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "missing_code",
												"message": "field 'code' is required",
											},
										},
									},
									"invalidJSON": map[string]interface{}{
										"summary": "Invalid JSON body",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "invalid_json",
												"message": "invalid JSON body",
											},
										},
									},
									"unknownRule": map[string]interface{}{
										"summary": "Unknown rule in config",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":      "unknown_rule",
												"message":   "unknown rule: unknown-rule",
												"path":      "config.rules.unknown-rule",
												"supported": []interface{}{"max-fanout", "no-disconnected-nodes", "no-duplicate-node-ids"},
											},
										},
									},
									"unknownOption": map[string]interface{}{
										"summary": "Unknown option in rule config",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":      "unknown_option",
												"message":   "unknown option: threshold",
												"path":      "config.rules.max-fanout.threshold",
												"supported": []interface{}{"enabled", "limit", "severity", "suppression_selectors"},
											},
										},
									},
									"invalidOption": map[string]interface{}{
										"summary": "Invalid option value",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "invalid_option",
												"message": "invalid option value for limit",
												"path":    "config.rules.max-fanout.limit",
											},
										},
									},
								},
							},
						},
					},
					"413": map[string]interface{}{
						"description": "Request entity too large (body exceeds 1 MiB)",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ErrorResponse",
								},
								"example": map[string]interface{}{
									"valid":  false,
									"issues": []interface{}{},
									"error": map[string]interface{}{
										"code":    "request_too_large",
										"message": "request body exceeds 1 MiB limit",
									},
								},
							},
						},
					},

					"500": map[string]interface{}{
						"description": "Internal server error",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ErrorResponse",
								},
								"examples": map[string]interface{}{
									"subprocess": map[string]interface{}{
										"summary": "Parser subprocess failure",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "parser_subprocess_error",
												"message": "parser subprocess failed",
											},
										},
									},
									"decode": map[string]interface{}{
										"summary": "Parser malformed output",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "parser_decode_error",
												"message": "parser returned malformed output",
											},
										},
									},
									"contract": map[string]interface{}{
										"summary": "Parser contract violation",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "parser_contract_violation",
												"message": "parser response violated service contract",
											},
										},
									},
									"internal": map[string]interface{}{
										"summary": "Generic internal failure",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
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
						"description": "Parser timeout",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/ErrorResponse",
								},
								"example": map[string]interface{}{
									"valid":  false,
									"issues": []interface{}{},
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
		"/spec": map[string]interface{}{
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
		"/docs": map[string]interface{}{
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
	},
	"components": map[string]interface{}{
		"schemas": map[string]interface{}{

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
				"required": []string{"id", "severity", "description", "default_config", "configurable_options"},
				"properties": map[string]interface{}{
					"id":             map[string]interface{}{"type": "string", "example": "max-fanout"},
					"severity":       map[string]interface{}{"type": "string", "enum": []string{"error", "warning", "info"}},
					"description":    map[string]interface{}{"type": "string"},
					"default_config": map[string]interface{}{"type": "object", "additionalProperties": true},
					"configurable_options": map[string]interface{}{
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
						"description": "Optional lint rule configuration. Preferred format is versioned: {\"schema_version\":\"v1\",\"rules\":{...}}. Legacy flat and nested formats remain supported during migration.",
						"example": map[string]interface{}{
							"schema_version": "v1",
							"rules": map[string]interface{}{
								"max-fanout": map[string]interface{}{
									"enabled":               true,
									"limit":                 3,
									"severity":              "error",
									"suppression_selectors": []interface{}{"node:A"},
								},
								"no-disconnected-nodes": map[string]interface{}{
									"enabled":  false,
									"severity": "info",
								},
							},
						},
					},
				},
			},

			"ErrorResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"valid", "issues", "error"},
				"properties": map[string]interface{}{
					"valid": map[string]interface{}{
						"type":        "boolean",
						"description": "Always false for non-200 responses",
					},
					"issues": map[string]interface{}{
						"type":        "array",
						"description": "Always empty for API-level errors",
						"items": map[string]interface{}{
							"$ref": "#/components/schemas/Issue",
						},
					},
					"error": map[string]interface{}{
						"$ref": "#/components/schemas/ErrorDetail",
					},
				},
			},
			"ErrorDetail": map[string]interface{}{
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
					"path": map[string]interface{}{
						"type":        "string",
						"description": "JSON path to the invalid config field when config validation fails",
						"example":     "config.rules.max-fanout.limit",
					},
					"supported": map[string]interface{}{
						"type":        "array",
						"description": "Supported values for unknown rule/option errors",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			"AnalyzeResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"valid", "lint_supported", "issues"},
				"properties": map[string]interface{}{
					"valid": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the Mermaid code is syntactically valid",
					},
					"diagram_type": map[string]interface{}{
						"type":        "string",
						"description": "Normalized Mermaid diagram type (flowchart, sequence, class, er, state, unknown).",
						"enum":        []string{"flowchart", "sequence", "class", "er", "state", "unknown"},
						"example":     "flowchart",
					},
					"lint_supported": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether lint rules are currently implemented for the parsed diagram type.",
					},
					"syntax_error": map[string]interface{}{
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
					"metrics": map[string]interface{}{
						"$ref":        "#/components/schemas/Metrics",
						"description": "Aggregate statistics about the diagram. Present if valid.",
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
				"required": []string{"rule_id", "severity", "message"},
				"properties": map[string]interface{}{
					"rule_id": map[string]interface{}{
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
				},
			},
			"Metrics": map[string]interface{}{
				"type":     "object",
				"required": []string{"node_count", "edge_count", "disconnected_node_count", "duplicate_node_count", "max_fanin", "max_fanout", "diagram_type"},
				"properties": map[string]interface{}{
					"node_count": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of nodes in the diagram",
						"example":     5,
					},
					"edge_count": map[string]interface{}{
						"type":        "integer",
						"description": "Total number of edges (connections) in the diagram",
						"example":     4,
					},
					"disconnected_node_count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of nodes with neither incoming nor outgoing edges",
						"example":     1,
					},
					"duplicate_node_count": map[string]interface{}{
						"type":        "integer",
						"description": "Number of duplicate node declarations beyond first occurrences",
						"example":     0,
					},
					"max_fanin": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of incoming edges to any single node",
						"example":     3,
					},
					"max_fanout": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of outgoing edges from any single node",
						"example":     2,
					},
					"diagram_type": map[string]interface{}{
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
					"issue_counts": map[string]interface{}{
						"$ref":        "#/components/schemas/IssueCounts",
						"description": "Issue count aggregations by severity and rule ID",
					},
				},
			},
			"IssueCounts": map[string]interface{}{
				"type":     "object",
				"required": []string{"by_severity", "by_rule"},
				"properties": map[string]interface{}{
					"by_severity": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "integer"},
					},
					"by_rule": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "integer"},
					},
				},
			},
		},
	},
}
