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
	},
	"paths": map[string]interface{}{
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
												"limit": 2,
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
													"line":     5,
													"column":   0,
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
						"description": "Bad request (invalid JSON or missing required field)",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"error": map[string]interface{}{
											"type": "string",
										},
									},
								},
								"examples": map[string]interface{}{
									"missingCode": map[string]interface{}{
										"summary": "Missing 'code' field",
										"value": map[string]interface{}{
											"error": "field 'code' is required",
										},
									},
									"invalidJSON": map[string]interface{}{
										"summary": "Invalid JSON body",
										"value": map[string]interface{}{
											"error": "invalid JSON body",
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
									"type": "object",
									"properties": map[string]interface{}{
										"error": map[string]interface{}{
											"type": "string",
										},
									},
								},
								"example": map[string]interface{}{
									"error": "request body exceeds 1 MiB limit",
								},
							},
						},
					},

					"500": map[string]interface{}{
						"description": "Internal server error",
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"error": map[string]interface{}{
											"type": "string",
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
								"limit": 3,
							},
							"no-disconnected-nodes": map[string]interface{}{
								"enabled": true,
							},
						},
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
						"description": "1-based line number where issue is located (if applicable)",
						"example":     5,
						"nullable":    true,
					},
					"column": map[string]interface{}{
						"type":        "integer",
						"description": "0-based column number where issue is located (if applicable)",
						"example":     0,
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
