const errorResponse = {
  description: "Request could not be processed.",
  content: { "application/json": { schema: { type: "object", properties: { error: { type: "object" } } } } },
};

/** The Worker intentionally publishes only endpoints it implements. */
export const workerOpenApi = {
  openapi: "3.0.3",
  info: {
    title: "merm8 Worker API",
    version: "1.0.0",
    description: "Worker-native deterministic Mermaid structural analysis and optional JEV-powered semantic review.",
  },
  components: {
    securitySchemes: {
      bearerAuth: { type: "http", scheme: "bearer", description: "Merm8 API_KEY secret." },
      apiKeyHeader: { type: "apiKey", in: "header", name: "x-api-key", description: "Merm8 API_KEY secret." },
    },
  },
  paths: {
    "/v1/healthz": { get: { summary: "Liveness probe", responses: { "200": { description: "Service is live" } } } },
    "/v1/ready": { get: { summary: "Readiness probe", responses: { "200": { description: "Service is ready" } } } },
    "/v1/version": { get: { summary: "Build metadata", responses: { "200": { description: "Build metadata" } } } },
    "/v1/diagram-types": { get: { summary: "Worker-recognized diagram types", responses: { "200": { description: "Capabilities" } } } },
    "/v1/rules": { get: { summary: "Available lint rules", responses: { "200": { description: "Rules" } } } },
    "/v1/analyze": {
      post: {
        summary: "Validate Mermaid source and lint supported diagrams",
        requestBody: { required: true, content: { "application/json": { schema: { type: "object", required: ["code"], properties: { code: { type: "string" }, config: { type: "object" } } } } } },
        responses: { "200": { description: "Analysis result. `lint-supported` reports whether Worker rules ran for the diagram type." }, "400": errorResponse, "413": { description: "Request body exceeds the 1 MiB limit" } },
      },
    },
    "/v1/semantic-review": {
      post: {
        summary: "Run an authenticated JEV semantic review alongside deterministic analysis",
        description: "Calls OpenRouter Decisions API once with six typed questions. Requires the Merm8 API_KEY and server-side OPENROUTER_API_KEY. Semantic judgements and probabilities are separate from deterministic lint issues.",
        security: [{ bearerAuth: [] }, { apiKeyHeader: [] }],
        requestBody: {
          required: true,
          content: { "application/json": { schema: { type: "object", required: ["code"], properties: { code: { type: "string", description: "Mermaid source." } } } } },
        },
        responses: {
          "200": {
            description: "Deterministic structural result with a separate semantic-review object.",
            content: {
              "application/json": {
                schema: {
                  type: "object",
                  required: ["valid", "diagram-type", "lint-supported", "issues", "semantic-review", "meta"],
                  properties: {
                    valid: { type: "boolean" },
                    "diagram-type": { type: "string", enum: ["flowchart", "sequence", "class", "er", "state", "unknown"] },
                    "lint-supported": { type: "boolean" },
                    issues: { type: "array", items: { type: "object" } },
                    "semantic-review": {
                      type: "object",
                      required: ["purpose", "label-clarity", "branch-clarity", "abstraction-consistency", "ambiguity", "review-priority"],
                      properties: {
                        purpose: { type: "object", required: ["value", "confidence"], properties: { value: { type: "string", enum: ["process", "decision-tree", "architecture", "data-model", "interaction", "state-machine", "other"] }, confidence: { type: "number", minimum: 0, maximum: 1 } } },
                        "label-clarity": { type: "object", required: ["value", "probability"], properties: { value: { type: "boolean" }, probability: { type: "number", minimum: 0, maximum: 1 } } },
                        "branch-clarity": { type: "object", required: ["value", "probability"], properties: { value: { type: "boolean" }, probability: { type: "number", minimum: 0, maximum: 1 } } },
                        "abstraction-consistency": { type: "object", required: ["value", "probability"], properties: { value: { type: "boolean" }, probability: { type: "number", minimum: 0, maximum: 1 } } },
                        ambiguity: { type: "object", required: ["value", "probability"], properties: { value: { type: "boolean" }, probability: { type: "number", minimum: 0, maximum: 1 } } },
                        "review-priority": { type: "object", required: ["value", "confidence"], properties: { value: { type: "string", enum: ["none", "low", "medium", "high"] }, confidence: { type: "number", minimum: 0, maximum: 1 } } },
                      },
                    },
                    meta: { type: "object", required: ["source", "model"], properties: { source: { type: "string", enum: ["jev"] }, model: { type: "string" } } },
                  },
                },
              },
            },
          },
          "400": { ...errorResponse, description: "Invalid JSON, request fields, or Mermaid source." },
          "401": { ...errorResponse, description: "The Merm8 API_KEY is missing or invalid." },
          "413": { ...errorResponse, description: "The request body or semantic state exceeds its configured bound." },
          "422": { ...errorResponse, description: "The parser produced no nodes that can be reviewed semantically." },
          "502": { ...errorResponse, description: "The JEV response was invalid or the upstream request was rejected." },
          "503": { ...errorResponse, description: "OpenRouter credentials/model configuration is missing, or the provider is unavailable." },
          "504": { ...errorResponse, description: "The JEV Decisions API request timed out." },
        },
      },
    },
  },
} as const;
