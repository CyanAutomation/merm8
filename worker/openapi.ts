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
    description: "Worker-native Mermaid structural validation and flowchart linting.",
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
        responses: { "200": { description: "Analysis result" }, "400": errorResponse, "413": { description: "Request body exceeds the 1 MiB limit" } },
      },
    },
  },
} as const;
