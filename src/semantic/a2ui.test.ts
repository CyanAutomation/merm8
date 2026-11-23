import assert from "node:assert/strict";
import test from "node:test";
import type { Analysis } from "../domain/types.js";
import { buildSemanticReviewA2ui } from "./a2ui.js";
import type { SemanticReview } from "./types.js";

const review: SemanticReview = {
  purpose: { value: "process", confidence: 0.92 },
  "label-clarity": { value: true, probability: 0.9 },
  "branch-clarity": { value: false, probability: 0.3 },
  "abstraction-consistency": { value: true, probability: 0.8 },
  ambiguity: { value: true, probability: 0.85 },
  "review-priority": { value: "high", confidence: 0.91 },
};

function analysis(issues: Analysis["issues"] = []): Analysis {
  return {
    valid: true,
    "diagram-type": "flowchart",
    "lint-supported": true,
    issues,
    metrics: {
      "node-count": 3,
      "edge-count": 2,
      "disconnected-node-count": 0,
      "duplicate-node-count": 0,
      "max-fanin": 1,
      "max-fanout": 2,
      "diagram-type": "flowchart",
      "issue-counts": { "by-severity": {}, "by-rule": {} },
    },
  };
}

test("builds A2UI component order from JEV's approved presentation choice", () => {
  const [message] = buildSemanticReviewA2ui(analysis(), review, "checks-first", "detailed");
  assert.equal(message?.createSurface.surfaceId, "diagramReview");
  assert.deepEqual(
    message?.createSurface.components.find((component) => component.id === "root")?.children,
    ["issuesCard", "summaryCard", "semanticCard"],
  );
  assert.deepEqual(message?.createSurface.dataModel.presentation, {
    order: "checks-first",
    density: "detailed",
  });
});

test("uses clarity-first ordering and preserves each semantic judgment", () => {
  const [message] = buildSemanticReviewA2ui(analysis(), review, "clarity-first", "concise");
  const components = message?.createSurface.components ?? [];
  assert.deepEqual(
    components.find((component) => component.id === "root")?.children,
    ["semanticCard", "summaryCard", "issuesCard"],
  );
  const values = message?.createSurface.dataModel.semantic as Record<string, unknown>;
  assert.match(String(values?.summary), /Labels: Clear/);
  assert.match(String(values?.summary), /Branches: Needs clarification/);
  assert.match(String(values?.summary), /Ambiguity: Detected/);
});

test("caps issue detail and text length while retaining the deterministic total", () => {
  const oversizedIssues = Array.from({ length: 100 }, (_, index) => ({
    "rule-id": `rule-${index}`,
    severity: "warning" as const,
    message: `issue ${index} ${"x".repeat(600)}`,
    line: index + 1,
    fingerprint: `fingerprint-${index}`,
  }));
  const [message] = buildSemanticReviewA2ui(analysis(oversizedIssues), review, "checks-first", "detailed");
  const data = message?.createSurface.dataModel.issues as { summary: string; details: string };
  assert.match(data.summary, /100 deterministic issues/);
  assert.match(data.summary, /showing 8/);
  assert.ok(data.details.length < 2_500);
  assert.ok((message?.createSurface.components.length ?? 0) <= 64);
});
