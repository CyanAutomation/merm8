import assert from "node:assert/strict";
import test from "node:test";
import { analyzeDiagram } from "../src/engine/analyze.js";
import { parseMermaid } from "../src/parser/parse.js";
import { buildSemanticState, SemanticStateLimitError } from "../src/semantic/state.js";

test("builds bounded semantic state from parsed labels and deterministic analysis", () => {
  const code = [
    "flowchart TD",
    "A[Receive order] -->|Accepted| B{Valid payment?}",
    "B -->|Yes| C[Capture payment]",
    "B -->|No| D[Reject order]",
    "A --> E[Notify customer]",
    "A --> F[Write audit log]",
    "A --> G[Create receipt]",
    "A --> H[Send confirmation]",
    "A --> I[Archive order]",
  ].join("\n");
  const parsed = parseMermaid(code);
  assert.ok(parsed.diagram);
  const analysis = analyzeDiagram(parsed.diagram);
  const state = buildSemanticState(parsed.diagram, analysis);

  assert.deepEqual(state.diagram.nodes.slice(0, 4), [
    { id: "A", label: "Receive order" },
    { id: "B", label: "Valid payment?" },
    { id: "C", label: "Capture payment" },
    { id: "D", label: "Reject order" },
  ]);
  assert.deepEqual(state.diagram.edges.slice(0, 3), [
    { from: "A", to: "B", label: "Accepted" },
    { from: "B", to: "C", label: "Yes" },
    { from: "B", to: "D", label: "No" },
  ]);
  assert.equal(state["has-branching"], true);
  assert.equal(state.structural["issue-count"], 1);
  assert.deepEqual(state.structural.issues, [{ "rule-id": "max-fanout", severity: "warning" }]);
  assert.equal("source" in state, false);
  assert.equal("OPENROUTER_API_KEY" in state, false);
});

test("rejects semantic diagrams above the node and edge bounds", () => {
  const code = ["flowchart TD", ...Array.from({ length: 201 }, (_, index) => `N${index}[Node ${index}]`)].join("\n");
  const parsed = parseMermaid(code);
  const diagram = parsed.diagram;
  assert.ok(diagram);
  assert.throws(() => buildSemanticState(diagram, analyzeDiagram(diagram)), SemanticStateLimitError);
});

test("preserves interaction messages as edge labels for sequence diagrams", () => {
  const parsed = parseMermaid("sequenceDiagram\nAlice->>Bob: Approve purchase");
  const diagram = parsed.diagram;
  assert.ok(diagram);
  const semantic = diagram.semantic;
  assert.ok(semantic);
  assert.deepEqual(semantic.nodes.map(({ id, label }) => ({ id, label })), [
    { id: "Alice", label: "Alice" },
    { id: "Bob", label: "Bob" },
  ]);
  assert.deepEqual(semantic.edges.map(({ from, to, label }) => ({ from, to, label })), [
    { from: "Alice", to: "Bob", label: "Approve purchase" },
  ]);
});

test("preserves relationship labels and entity endpoints for ER diagrams", () => {
  const parsed = parseMermaid("erDiagram\nCUSTOMER ||--o{ ORDER : places");
  const semantic = parsed.diagram?.semantic;
  assert.ok(semantic);
  assert.deepEqual(semantic.nodes.map(({ id }) => id), ["CUSTOMER", "ORDER"]);
  assert.deepEqual(semantic.edges.map(({ from, to, label }) => ({ from, to, label })), [
    { from: "CUSTOMER", to: "ORDER", label: "places" },
  ]);
});
