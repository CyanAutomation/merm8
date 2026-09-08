import assert from "node:assert/strict";
import test from "node:test";
import worker from "../worker/index.js";
import { parseMermaid } from "../src/parser/parse.js";

const env = { API_KEY: "test-key" };

test("serves the splash origin with CORS headers and handles preflight", async () => {
  const corsEnv = { ...env, REST_ALLOWED_ORIGINS: "https://merm8-splash.vercel.app" };
  const preflight = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "OPTIONS",
    headers: {
      origin: "https://merm8-splash.vercel.app",
      "access-control-request-method": "POST",
      "access-control-request-headers": "content-type",
    },
  }), corsEnv);

  assert.equal(preflight.status, 204);
  assert.equal(preflight.headers.get("access-control-allow-origin"), "https://merm8-splash.vercel.app");
  assert.match(preflight.headers.get("access-control-allow-methods") ?? "", /POST/);

  const health = await worker.fetch(new Request("https://example.test/v1/healthz", {
    headers: { origin: "https://merm8-splash.vercel.app" },
  }), corsEnv);
  assert.equal(health.headers.get("access-control-allow-origin"), "https://merm8-splash.vercel.app");
});

test("serves Worker-native health and discovery endpoints", async () => {
  const health = await worker.fetch(new Request("https://example.test/v1/healthz"), env);
  assert.equal(health.status, 200);
  assert.equal((await health.json() as { status: string }).status, "ok");

  const types = await worker.fetch(new Request("https://example.test/v1/diagram-types"), env);
  assert.deepEqual((await types.json() as { "lint-supported": string[] })["lint-supported"], ["flowchart"]);

  const rules = await worker.fetch(new Request("https://example.test/v1/rules"), env);
  const payload = await rules.json() as { rules: Array<{ id: string; description: string; severity: string }> };
  assert.ok(payload.rules.some(rule => rule.id === "no-cycles" && rule.description && rule.severity === "error"));
});

test("serves a usable OpenAPI document and secure response headers", async () => {
  const spec = await worker.fetch(new Request("https://example.test/v1/spec"), env);
  assert.equal(spec.status, 200);
  assert.equal(spec.headers.get("x-content-type-options"), "nosniff");
  assert.equal(spec.headers.get("content-security-policy"), "default-src 'none'; frame-ancestors 'none'");
  assert.equal(spec.headers.get("x-merm8-build"), "development");
  const document = await spec.json() as { openapi: string; paths: Record<string, unknown> };
  assert.equal(document.openapi, "3.0.3");
  assert.ok("/v1/analyze" in document.paths);

  const root = await worker.fetch(new Request("https://example.test/"), env);
  assert.equal(root.status, 200);
  assert.deepEqual(await root.json(), { status: "ok" });
});

test("analyses a flowchart without a process or container", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\nA --> B\nB --> A" })
  }), env);
  assert.equal(response.status, 200);
  const result = await response.json() as { valid: boolean; issues: Array<{ "rule-id": string }> };
  assert.equal(result.valid, true);
  assert.ok(result.issues.some(issue => issue["rule-id"] === "no-cycles"));
});

test("honours splash nested rule configuration", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({
      code: "flowchart TD\nA --> B\nB --> A\nX",
      config: {
        "schema-version": "v1",
        rules: {
          "no-cycles": { enabled: false },
          "no-disconnected-nodes": { enabled: false },
        },
      },
    }),
  }), env);
  const result = await response.json() as { issues: Array<{ "rule-id": string }> };
  assert.ok(!result.issues.some(issue => issue["rule-id"] === "no-cycles"));
  assert.ok(!result.issues.some(issue => issue["rule-id"] === "no-disconnected-nodes"));
});

test("does not confuse edge references with duplicate node declarations", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({
      code: "graph TD\n    A[Start] --> B{Decision}\n    B -->|Yes| C[Do something]\n    B -->|No| D[Do something else]\n    C --> E[End]\n    D --> E",
      config: { "schema-version": "v1", rules: { "no-duplicate-node-ids": { enabled: true } } },
    }),
  }), env);
  const result = await response.json() as { valid: boolean; issues: Array<{ "rule-id": string }> };
  assert.equal(result.valid, true);
  assert.ok(!result.issues.some(issue => issue["rule-id"] === "no-duplicate-node-ids"));
});

test("reports a repeated explicit node declaration", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\n  A[First]\n  A[Second]" }),
  }), env);
  const result = await response.json() as { issues: Array<{ "rule-id": string }> };
  assert.ok(result.issues.some(issue => issue["rule-id"] === "no-duplicate-node-ids"));
});

test("rejects malformed flowchart relations", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\n  A --> --> B" }),
  }), env);
  const result = await response.json() as { valid: boolean; error?: { code: string; line: number } };
  assert.equal(result.valid, false);
  assert.equal(result.error?.code, "syntax_error");
  assert.equal(result.error?.line, 2);
});

test("protects MCP and exposes the analysis tool through Streamable HTTP", async () => {
  const denied = await worker.fetch(new Request("https://example.test/mcp", { method: "POST" }), env);
  assert.equal(denied.status, 401);

  const response = await worker.fetch(new Request("https://example.test/mcp", {
    method: "POST", headers: { authorization: "Bearer test-key", "content-type": "application/json", accept: "application/json, text/event-stream", host: "example.test" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "analyze_mermaid", arguments: { code: "flowchart TD\nA --> B" } } })
  }), env);
  assert.equal(response.status, 200);
  const wire = await response.text();
  const body = JSON.parse(wire.match(/^data: (.+)$/m)?.[1] ?? "{}") as { result: { structuredContent: { valid: boolean } } };
  assert.equal(body.result.structuredContent.valid, true);
});

test("authorizes MCP with credentials instead of the client-controlled hostname", async () => {
  const response = await worker.fetch(new Request("https://spoofed.example/mcp", {
    method: "POST", headers: { authorization: "Bearer test-key", "content-type": "application/json", accept: "application/json, text/event-stream" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" })
  }), env);
  assert.equal(response.status, 200);
});

test("records only explicit node declarations for duplicate-ID analysis", () => {
  const result = parseMermaid("flowchart LR\n  A --> B --> A\n  A --> A");
  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.nodes.map(node => [node.id, node.line, node.column]), [
    ["A", 2, 3],
    ["B", 2, 9]
  ]);
  assert.deepEqual(result.diagram.sourceNodeIds, []);
});

test("logs malformed JSON details while returning a generic client error", async (t) => {
  const errors: unknown[][] = [];
  t.mock.method(console, "error", (...args: unknown[]) => { errors.push(args); });
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" }, body: "{"
  }), env);
  assert.equal(response.status, 400);
  assert.equal(errors.length, 1);
  assert.equal(errors[0][0], "JSON parse error:");
  assert.ok(errors[0][1] instanceof Error);
});
