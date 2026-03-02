package api

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
		"/analyze": map[string]interface{}{
			"post": map[string]interface{}{
				"tags":        []string{"Linting"},
				"summary":     "Analyze and lint a Mermaid diagram",
				"description": "Validates Mermaid code syntax and runs deterministic lint rules.\n\nMaximum request body size is 1 MiB. Oversized payloads return HTTP 413.\n\nReturns syntax errors if parsing fails, or lint results if parsing succeeds.",
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
											"max-fanout": map[string]interface{}{
												"limit":    2,
												"severity": "error",
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
											"valid":        true,
											"syntax_error": nil,
											"issues":       []interface{}{},
											"metrics": map[string]interface{}{
												"node_count": 2,
												"edge_count": 1,
												"max_fanout": 1,
											},
										},
									},
									"syntaxError": map[string]interface{}{
										"summary": "Syntax error response",
										"value": map[string]interface{}{
											"valid": false,
											"syntax_error": map[string]interface{}{
												"message": "Unexpected token '>'",
												"line":    2,
												"column":  12,
											},
											"issues": []interface{}{},
										},
									},
									"withIssues": map[string]interface{}{
										"summary": "Valid diagram with lint issues",
										"value": map[string]interface{}{
											"valid":        true,
											"syntax_error": nil,
											"issues": []interface{}{
												map[string]interface{}{
													"rule_id":  "no-disconnected-nodes",
													"severity": "error",
													"message":  "Node 'D' is not connected to the graph",
												},
												map[string]interface{}{
													"rule_id":  "max-fanout",
													"severity": "warn",
													"message":  "Node 'A' has fanout 6, exceeding limit of 5",
													"line":     2,
													"column":   2,
												},
											},
											"metrics": map[string]interface{}{
												"node_count": 4,
												"edge_count": 2,
												"max_fanout": 2,
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
									"invalidConfig": map[string]interface{}{
										"summary": "Invalid lint configuration",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "invalid_config",
												"message": `invalid severity for rule "max-fanout": "warning" (allowed: error, warn, info)`,
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
									"parserTimeout": map[string]interface{}{
										"summary": "Parser timeout",
										"value": map[string]interface{}{
											"valid":  false,
											"issues": []interface{}{},
											"error": map[string]interface{}{
												"code":    "parser_timeout",
												"message": "parser timed out",
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
			"AnalyzeRequest": map[string]interface{}{
				"type":     "object",
				"required": []string{"code"},
				"properties": map[string]interface{}{
					"code": map[string]interface{}{
						"type":        "string",
						"description": "Mermaid diagram code to analyze. Total request body must be 1 MiB or smaller.",
						"example":     "graph TD\n  A[Start] --> B[Process]\n  B --> C[End]",
					},
					"config": map[string]interface{}{
						"type":        "object",
						"description": "Optional lint rule configuration. Supports both flat and nested formats:\n- Flat format: `{\"rule-id\": {\"option\": \"value\"}}`\n- Nested format: `{\"rules\": {\"rule-id\": {\"option\": \"value\"}}}`",
						"example": map[string]interface{}{
							"max-fanout": map[string]interface{}{
								"limit":    3,
								"severity": "error",
							},
							"no-disconnected-nodes": map[string]interface{}{
								"enabled":  true,
								"severity": "warn",
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
				},
			},
			"AnalyzeResponse": map[string]interface{}{
				"type":     "object",
				"required": []string{"valid", "issues"},
				"properties": map[string]interface{}{
					"valid": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the Mermaid code is syntactically valid",
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
						"enum":        []string{"error", "warn", "info"},
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
				"required": []string{"node_count", "edge_count", "max_fanout"},
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
					"max_fanout": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of outgoing edges from any single node",
						"example":     2,
					},
				},
			},
		},
	},
}
