import test from "node:test";
import assert from "node:assert/strict";

import { analyzeMermaid } from "../src/engine/analyze.js";

test("analysis preserves flowchart findings, ordering, severity overrides, and metrics", () => {
  const result = analyzeMermaid(
    "flowchart TD\nA --> B\nA --> C\nA --> A\nSolo",
    {
      "max-fanout": { limit: 1, severity: "info" },
      "no-cycles": { severity: "warning" },
    },
  );

  assert.equal(result.valid, true);
  assert.deepEqual(result.issues.map(({ "rule-id": rule, severity, line }) => [rule, severity, line]), [
    ["max-fanout", "info", 2],
    ["no-cycles", "warning", 2],
    ["no-disconnected-nodes", "error", 5],
  ]);
  assert.deepEqual(result.metrics, {
    "node-count": 4,
    "edge-count": 3,
    "disconnected-node-count": 1,
    "duplicate-node-count": 0,
    "max-fanin": 1,
    "max-fanout": 3,
    "diagram-type": "flowchart",
    "issue-counts": {
      "by-severity": { info: 1, warning: 1, error: 1 },
      "by-rule": { "max-fanout": 1, "no-cycles": 1, "no-disconnected-nodes": 1 },
    },
  });
});

test("Worker analysis cache reports cold and warm responses with matching results", async () => {
  const worker = (await import("../worker/index.js")).default;
  const env = { ANALYZE_LIMITER: { limit: async () => ({ success: true }) } };
  const body = JSON.stringify({ code: "flowchart TD\nCacheAUnique --> CacheBUnique" });
  const request = () => new Request("https://example.test/v1/analyze", {
    method: "POST",
    headers: { "content-type": "application/json", "cf-connecting-ip": "203.0.113.80" },
    body,
  });
  const first = await worker.fetch(request(), env);
  const firstBody = await first.json();
  const second = await worker.fetch(request(), env);
  const secondBody = await second.json();
  assert.equal(first.headers.get("cache-status"), "merm8; fwd=uri-miss");
  assert.equal(second.headers.get("cache-status"), "merm8; hit");
  assert.deepEqual(secondBody, firstBody);

  const configured = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST",
    headers: { "content-type": "application/json", "cf-connecting-ip": "203.0.113.80" },
    body: JSON.stringify({ code: "flowchart TD\nCacheAUnique --> CacheBUnique\nCacheAUnique --> CacheCUnique", config: { "max-fanout": { limit: 1 } } }),
  }), env);
  assert.equal(configured.headers.get("cache-status"), "merm8; fwd=uri-miss");
});

test("analysis keeps diagram-specific rules for sequence, class, ER, and state diagrams", () => {
  const cases = [
    {
      code: "sequenceDiagram\nparticipant Alice\nAlice->>Bob: Hello\nBob->>Carol: Next",
      expected: [["no-undefined-actors", "error", 3], ["no-undefined-actors", "error", 4]],
    },
    {
      code: "classDiagram\nclass Animal\nclass Animal",
      expected: [["no-duplicate-classes", "error", 2]],
    },
    {
      code: "erDiagram\nCUSTOMER ||--o{ CUSTOMER : relates",
      expected: [["no-self-referential", "warning", 2]],
    },
    {
      code: "stateDiagram-v2\n[*] --> Ready\nReady --> Done\nLost --> End",
      expected: [["no-unreachable-state", "error", 4], ["no-unreachable-state", "error", 4]],
    },
  ] as const;

  for (const { code, expected } of cases) {
    const result = analyzeMermaid(code);
    assert.equal(result.valid, true, code);
    assert.deepEqual(
      result.issues.map(({ "rule-id": rule, severity, line }) => [rule, severity, line]),
      expected,
      code,
    );
  }
});

test("disabled rules do not change structural metrics", () => {
  const result = analyzeMermaid("flowchart TD\nA --> B\nSolo", {
    "no-disconnected-nodes": { enabled: false },
  });

  assert.deepEqual(result.issues, []);
  assert.equal(result.metrics?.["disconnected-node-count"], 1);
  assert.deepEqual(result.metrics?.["issue-counts"], { "by-severity": {}, "by-rule": {} });
});
