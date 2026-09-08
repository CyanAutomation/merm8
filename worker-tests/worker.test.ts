import assert from "node:assert/strict";
import test from "node:test";
import worker from "../worker/index.js";
import { parseMermaid } from "../src/parser/parse.js";

const env = { API_KEY: "test-key" };

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

test("authorizes MCP with credentials instead of the client-controlled hostname", async () => {
  const response = await worker.fetch(new Request("https://spoofed.example/mcp", {
    method: "POST", headers: { authorization: "Bearer test-key", "content-type": "application/json", accept: "application/json, text/event-stream" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/list" })
  }), env);
  assert.equal(response.status, 200);
});

test("reports each repeated edge endpoint at its actual column", () => {
  const result = parseMermaid("flowchart LR\n  A --> B --> A\n  A --> A");
  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.nodes.map(node => [node.id, node.line, node.column]), [
    ["A", 2, 3],
    ["B", 2, 9]
  ]);
  assert.deepEqual(result.diagram.sourceNodeIds, ["A", "B", "A", "A"]);
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
