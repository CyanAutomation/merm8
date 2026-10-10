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
  assert.deepEqual((await types.json() as { "lint-supported": string[] })["lint-supported"], ["flowchart", "sequence", "class", "er", "state"]);

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
  const document = await spec.json() as {
    openapi: string;
    paths: Record<string, { post?: { responses: Record<string, unknown> } }>;
  };
  assert.equal(document.openapi, "3.0.3");
  assert.ok("/v1/analyze" in document.paths);
  const analyze = document.paths["/v1/analyze"];
  assert.ok(analyze?.post);
  assert.ok("413" in analyze.post!.responses);
  assert.ok(!("/v1/analyze/raw" in document.paths));
  assert.ok("/v1/analyze/sarif" in document.paths);

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

test("runs a deterministic rule for each recognized non-flowchart diagram type", async () => {
  const cases = [
    { code: "sequenceDiagram\nparticipant Alice\nAlice->>Bob: Hello", rule: "no-undefined-actors" },
    { code: "classDiagram\nclass Animal\nclass Animal", rule: "no-duplicate-classes" },
    { code: "erDiagram\nCUSTOMER ||--o{ CUSTOMER : relates", rule: "no-self-referential" },
    { code: "stateDiagram-v2\n[*] --> Ready\nReady --> Done\nLost --> End", rule: "no-unreachable-state" },
  ];

  for (const item of cases) {
    const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
      method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ code: item.code }),
    }), env);
    assert.equal(response.status, 200);
    const result = await response.json() as { "lint-supported": boolean; issues: Array<{ "rule-id": string }> };
    assert.equal(result["lint-supported"], true, item.code);
    assert.ok(result.issues.some(issue => issue["rule-id"] === item.rule), `${item.rule} should be reported`);
  }
});

test("returns structure metrics for Worker analysis", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\nA --> B\nB --> C\nX[isolated]" }),
  }), env);
  const result = await response.json() as {
    metrics: { "node-count": number; "edge-count": number; "disconnected-node-count": number; "max-fanin": number; "max-fanout": number; "diagram-type": string; "issue-counts": { "by-rule": Record<string, number> } };
  };
  assert.deepEqual(result.metrics, {
    "node-count": 4,
    "edge-count": 2,
    "disconnected-node-count": 1,
    "duplicate-node-count": 0,
    "max-fanin": 1,
    "max-fanout": 1,
    "diagram-type": "flowchart",
    "issue-counts": { "by-severity": { error: 1 }, "by-rule": { "no-disconnected-nodes": 1 } },
  });
});

test("exports SARIF 2.1.0 from Worker analysis", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze/sarif", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\nA --> B\nB --> A" }),
  }), env);
  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /application\/sarif\+json/);
  const report = await response.json() as {
    version: string;
    runs: Array<{ tool: { driver: { name: string; rules: Array<{ id: string }> } }; results: Array<{ ruleId: string; level: string; locations: Array<{ physicalLocation: { artifactLocation: { uri: string }; region: { startLine: number } } }>; partialFingerprints: Record<string, string> }> }>;
  };
  assert.equal(report.version, "2.1.0");
  assert.equal(report.runs[0].tool.driver.name, "merm8");
  assert.ok(report.runs[0].tool.driver.rules.some(rule => rule.id === "no-cycles"));
  assert.ok(report.runs[0].results.some(result => result.ruleId === "no-cycles" && result.level === "error"));
  assert.equal(report.runs[0].results[0].locations[0].physicalLocation.artifactLocation.uri, "diagram.mmd");
  assert.equal(report.runs[0].results[0].locations[0].physicalLocation.region.startLine, 2);
  assert.ok(report.runs[0].results[0].partialFingerprints.issueFingerprint);
});

test("rejects malformed max-fanout configuration instead of silently using defaults", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({
      code: "flowchart TD\nA --> B",
      config: { rules: { "max-fanout": { limit: "five" } } },
    }),
  }), env);

  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), {
    error: { code: "invalid_option", message: "max-fanout.limit must be a non-negative integer" },
  });
});

test("validates rule configuration envelopes and option values", async () => {
  const cases = [
    { config: "invalid", expected: { code: "invalid_request", message: "config must be an object" } },
    { config: { rules: [] }, expected: { code: "invalid_request", message: "config.rules must be an object" } },
    { config: { mystery: {} }, expected: { code: "unknown_rule", message: "unknown rule: mystery" } },
    { config: { "no-cycles": { enabled: "yes" } }, expected: { code: "invalid_option", message: "no-cycles.enabled must be a boolean" } },
    { config: { "no-cycles": { severity: "fatal" } }, expected: { code: "invalid_option", message: "no-cycles.severity must be error, warning, or info" } },
  ];

  for (const { config, expected } of cases) {
    const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
      method: "POST", headers: { "content-type": "application/json" },
      body: JSON.stringify({ code: "flowchart TD\nA --> B", config }),
    }), env);
    assert.equal(response.status, 400);
    assert.deepEqual(await response.json(), { error: expected });
  }

  const legacyConfig = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "flowchart TD\nA --> B", config: { "schema-version": "v1", rules: { "no-cycles": { enabled: false } } } }),
  }), env);
  assert.equal(legacyConfig.status, 200);
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
  assert.deepEqual(result.issues, []);
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

test("rejects a flowchart relation without a destination node", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: "graph TD\n  A -->" }),
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

test("MCP diagram discovery reflects all Worker lint families", async () => {
  const response = await worker.fetch(new Request("https://example.test/mcp", {
    method: "POST", headers: { authorization: "Bearer test-key", "content-type": "application/json", accept: "application/json, text/event-stream", host: "example.test" },
    body: JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "list_diagram_types", arguments: {} } }),
  }), env);
  assert.equal(response.status, 200);
  const wire = await response.text();
  const body = JSON.parse(wire.match(/^data: (.+)$/m)?.[1] ?? "{}") as { result: { structuredContent: { "lint-supported": string[] } } };
  assert.deepEqual(body.result.structuredContent["lint-supported"], ["flowchart", "sequence", "class", "er", "state"]);
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

test("rejects oversized analysis requests before parsing them", async () => {
  const oversizedCode = `flowchart TD\n${" ".repeat(1_048_576)}`;
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ code: oversizedCode }),
  }), env);

  assert.equal(response.status, 413);
  assert.deepEqual(await response.json(), {
    error: { code: "payload_too_large", message: "request body must not exceed 1 MiB" },
  });
});
