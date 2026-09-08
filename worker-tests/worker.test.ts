import assert from "node:assert/strict";
import test from "node:test";
import worker from "../worker/index.js";

const env = { API_KEY: "test-key", MCP_ALLOWED_HOSTNAMES: "example.test" };

test("serves Worker-native health and discovery endpoints", async () => {
  const health = await worker.fetch(new Request("https://example.test/v1/healthz"), env);
  assert.equal(health.status, 200);
  assert.equal((await health.json() as { status: string }).status, "ok");

  const types = await worker.fetch(new Request("https://example.test/v1/diagram-types"), env);
  assert.deepEqual((await types.json() as { "lint-supported": string[] })["lint-supported"], ["flowchart"]);
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
