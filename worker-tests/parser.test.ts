import test from "node:test";
import assert from "node:assert/strict";

import { parseMermaid } from "../src/parser/parse.js";

test("flowchart parsing keeps source locations and semantic labels", () => {
  const result = parseMermaid("flowchart LR\nA[Start] --> B(\"Finish\")");

  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.nodes.map(({ id, label, line, column }) => [id, label, line, column]), [
    ["A", "A", 2, 1],
    ["B", "B", 2, 14],
  ]);
  assert.deepEqual(result.diagram.semantic, {
    nodes: [
      { id: "A", label: "Start", line: 2, column: 1 },
      { id: "B", label: "Finish", line: 2, column: 14 },
    ],
    edges: [{ from: "A", to: "B", label: null, line: 2, column: 1 }],
  });
});

test("sequence parsing distinguishes actor declarations from message references", () => {
  const result = parseMermaid("sequenceDiagram\nparticipant Alice\nAlice->>Bob: Hello\nBob->>Carol: Next");

  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.sourceNodeIds, ["Alice"]);
  assert.deepEqual(result.diagram.edges.map(({ from, to, label }) => [from, to, label]), [
    ["Alice", "Bob", null],
    ["Bob", "Carol", null],
  ]);
  assert.deepEqual(result.diagram.semantic?.edges.map(({ from, to, label }) => [from, to, label]), [
    ["Alice", "Bob", "Hello"],
    ["Bob", "Carol", "Next"],
  ]);
});

test("ER parsing retains normalized relationship targets and labels", () => {
  const result = parseMermaid("erDiagram\nCUSTOMER ||--o{ ORDER : places");

  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.edges.map(({ from, to }) => [from, to]), [["CUSTOMER", "ORDER"]]);
  assert.deepEqual(result.diagram.semantic?.edges.map(({ from, to, label }) => [from, to, label]), [
    ["CUSTOMER", "ORDER", "places"],
  ]);
});

test("state parsing keeps explicit starts for reachability analysis", () => {
  const result = parseMermaid("stateDiagram-v2\n[*] --> Ready\nReady --> Done\nLost --> End");

  assert.ok(result.diagram);
  assert.deepEqual(result.diagram.startNodeIds, ["Ready"]);
  assert.deepEqual(result.diagram.edges.map(({ from, to }) => [from, to]), [
    ["*", "Ready"],
    ["Ready", "Done"],
    ["Lost", "End"],
  ]);
});

test("flowchart syntax errors keep their line and error codes", () => {
  assert.deepEqual(parseMermaid("flowchart TD\nA --> --> B").error, {
    code: "syntax_error",
    message: "Malformed flowchart relation",
    line: 2,
    column: 1,
  });
  assert.deepEqual(parseMermaid("flowchart TD\nA -->").error, {
    code: "syntax_error",
    message: "Flowchart relation is missing a destination node",
    line: 2,
    column: 1,
  });
});
