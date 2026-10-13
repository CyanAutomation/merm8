import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

import { extractAST } from "./ast-extract.ts";
import type { DiagramType, MermaidAPI, RawRecord } from "./ast-types.ts";
import { withWorkerTimeout } from "./parse.ts";

const parserScript = fileURLToPath(new URL("./parse.ts", import.meta.url));

function runParser(args: string[], input: string) {
  return spawnSync(process.execPath, ["--experimental-strip-types", parserScript, ...args], {
    cwd: dirname(parserScript),
    encoding: "utf8",
    input,
    timeout: 15_000,
  });
}

function parseThroughCli(source: string) {
  const result = runParser(
    ["--worker"],
    `${JSON.stringify({ id: "test", code: source, timeout_ms: 10_000 })}\n`,
  );
  assert.equal(result.status, 0, result.stderr);
  const envelope = JSON.parse(result.stdout) as { result: {
    valid: boolean;
    diagram_type?: string;
    error?: { message: string; line: number; column: number };
    ast?: {
      direction: string;
      nodes: Array<{ id: string; line?: number; column?: number }>;
      edges: Array<{ from: string; to: string; type: string; line?: number; column?: number }>;
      suppressions: Array<{ ruleId: string; scope: string; line: number; targetLine: number }>;
      startStates?: string[];
    };
  } };
  return envelope.result;
}

function controlledMermaid(db: RawRecord | null): MermaidAPI {
  return { getDiagramFromText: async () => ({ db }) };
}

function createTimerHarness() {
  let nextId = 0;
  const scheduled = new Map<number, () => void>();
  const cleared: number[] = [];

  return {
    timer: {
      setTimeout(fn: () => void, _timeoutMs: number) {
        const id = ++nextId;
        scheduled.set(id, fn);
        return id;
      },
      clearTimeout(id: ReturnType<typeof setTimeout> | number) {
        const numericId = typeof id === "number" ? id : Number(id);
        cleared.push(numericId);
        scheduled.delete(numericId);
      },
    },
    getCleared() {
      return [...cleared];
    },
    triggerTimeout(id = 1) {
      const fn = scheduled.get(id);
      if (!fn) {
        throw new Error("no timeout scheduled for id " + String(id));
      }
      fn();
    },
  };
}

// @spec: PARSER-NODE-001: Settled worker requests clear their timeout timer.
test("withWorkerTimeout clears its timer when the wrapped promise settles", async () => {
  const resolvedHarness = createTimerHarness();
  const result = await withWorkerTimeout(
    Promise.resolve("ok"),
    25,
    resolvedHarness.timer,
  );
  assert.equal(result, "ok");
  assert.deepEqual(resolvedHarness.getCleared(), [1]);

  const rejectedHarness = createTimerHarness();
  const expected = new Error("boom");
  await assert.rejects(
    withWorkerTimeout(Promise.reject(expected), 25, rejectedHarness.timer),
    expected,
  );
  assert.deepEqual(rejectedHarness.getCleared(), [1]);
});

test("withWorkerTimeout keeps WORKER_TIMEOUT error code for timeout failures", async () => {
  const harness = createTimerHarness();
  let timeoutErr: (Error & { code?: string }) | undefined;

  const pending = withWorkerTimeout(
    new Promise(() => {}),
    25,
    harness.timer,
  ).catch((err: unknown) => {
    timeoutErr = err as Error & { code?: string };
    throw err;
  });

  harness.triggerTimeout(1);

  await assert.rejects(pending, (err: unknown) => {
    const timeoutError = err as Error & { code?: string };
    assert.equal(timeoutError.code, "WORKER_TIMEOUT");
    assert.equal(timeoutError.message, "worker parse timeout");
    return true;
  });
  assert.equal(timeoutErr?.code, "WORKER_TIMEOUT");
  assert.deepEqual(harness.getCleared(), []);
});

test("parser CLI extracts flowchart nodes, edge locations, direction, and suppressions", () => {
  const result = parseThroughCli("flowchart LR\n%% merm8-disable no-cycles\nA[Start] --> B[Finish]");

  assert.equal(result.valid, true);
  assert.equal(result.diagram_type, "flowchart");
  assert.equal(result.ast?.direction, "LR");
  assert.deepEqual(result.ast?.nodes.map(({ id, line, column }) => [id, line, column]), [
    ["A", 3, 1],
    ["B", 3, 14],
  ]);
  assert.deepEqual(result.ast?.edges.map(({ from, to, line, column }) => [from, to, line, column]), [
    ["A", "B", 3, 1],
  ]);
  assert.deepEqual(result.ast?.suppressions, [
    { ruleId: "no-cycles", scope: "file", line: 2, targetLine: 2 },
  ]);
});

test("parser CLI extracts sequence participants and messages", () => {
  const result = parseThroughCli("sequenceDiagram\nparticipant Alice\nparticipant Bob\nAlice->>Bob: Hello");

  assert.equal(result.valid, true);
  assert.equal(result.diagram_type, "sequence");
  assert.deepEqual(result.ast?.nodes.map(({ id }) => id), ["Alice", "Bob"]);
  assert.deepEqual(result.ast?.edges.map(({ from, to }) => [from, to]), [["Alice", "Bob"]]);
});

test("parser CLI preserves class inheritance direction", () => {
  const result = parseThroughCli("classDiagram\nclass Animal\nclass Dog\nAnimal <|-- Dog");

  assert.equal(result.valid, true);
  assert.equal(result.diagram_type, "class");
  assert.deepEqual(result.ast?.nodes.map(({ id }) => id), ["Animal", "Dog"]);
  assert.deepEqual(result.ast?.edges.map(({ from, to, type }) => [from, to, type]), [
    ["Dog", "Animal", "extension"],
  ]);
});

test("parser CLI extracts ER relationships and state start transitions", () => {
  const er = parseThroughCli("erDiagram\nCUSTOMER ||--o{ ORDER : places");
  assert.equal(er.valid, true);
  assert.equal(er.diagram_type, "er");
  assert.deepEqual(er.ast?.nodes.map(({ id }) => id), ["CUSTOMER", "ORDER"]);
  assert.deepEqual(er.ast?.edges.map(({ from, to }) => [from, to]), [["CUSTOMER", "ORDER"]]);

  const state = parseThroughCli("stateDiagram-v2\n[*] --> Ready\nReady --> Done");
  assert.equal(state.valid, true);
  assert.equal(state.diagram_type, "state");
  assert.deepEqual(state.ast?.startStates, ["Ready"]);
  assert.deepEqual(state.ast?.edges.map(({ from, to }) => [from, to]), [["Ready", "Done"]]);
});

test("AST extractors map all supported diagram databases into stable nodes and edges", async () => {
  const cases: Array<{ type: DiagramType; source: string; db: RawRecord }> = [
    {
      type: "flowchart",
      source: "flowchart LR\n%% merm8-disable no-cycles\nA[Start] --> B[Finish]",
      db: {
        direction: "LR",
        edges: [{ start: "A", end: "B", type: "arrow" }],
        vertices: { A: { text: "Start" }, B: { text: "Finish" } },
        subGraphs: [{ id: "group", title: "Group", nodes: ["A", "B"] }],
      },
    },
    {
      type: "sequence",
      source: "sequenceDiagram\nparticipant Alice\nparticipant Bob\nAlice->>Bob: Hello",
      db: { state: { records: { messages: [{ from: "Alice", to: "Bob", type: 1, message: "Hello" }] } } },
    },
    {
      type: "class",
      source: "classDiagram\nclass Animal\nclass Dog\nAnimal <|-- Dog",
      db: { classes: {}, relations: [{ id1: "Animal", id2: "Dog", relation: { type1: 1 } }] },
    },
    {
      type: "er",
      source: "erDiagram\nCUSTOMER ||--o{ ORDER : places",
      db: {
        relationships: [{ entityA: "entity-CUSTOMER-1", entityB: "entity-ORDER-2", relSpec: { relType: "NON_IDENTIFYING" }, roleA: "places" }],
      },
    },
    {
      type: "state",
      source: "stateDiagram-v2\n[*] --> Ready\nReady --> Done\nDone --> [*]",
      db: {
        nodes: [{ id: "root_start" }, { id: "root_end" }, { id: "Ready", label: "Ready" }, { id: "Done", label: "Done" }],
        edges: [
          { start: "root_start", end: "Ready" },
          { start: "Ready", end: "Done" },
          { start: "Done", end: "root_end" },
        ],
      },
    },
  ];

  const results = await Promise.all(cases.map(({ type, source, db }) => extractAST(controlledMermaid(db), source, type)));
  const [flowchart, sequence, classDiagram, er, state] = results;

  assert.equal(flowchart.direction, "LR");
  assert.deepEqual(flowchart.nodes.map(({ id, label }) => [id, label]), [["A", "Start"], ["B", "Finish"]]);
  assert.deepEqual(flowchart.subgraphs, [{ id: "group", label: "Group", nodes: ["A", "B"] }]);
  assert.deepEqual(flowchart.suppressions, [{ ruleId: "no-cycles", scope: "file", line: 2, targetLine: 2 }]);

  assert.deepEqual(sequence.edges.map(({ from, to, type, label }) => [from, to, type, label]), [["Alice", "Bob", "dotted", "Hello"]]);
  assert.deepEqual(classDiagram.edges.map(({ from, to, type }) => [from, to, type]), [["Dog", "Animal", "extension"]]);
  assert.deepEqual(er.edges.map(({ from, to, type, label }) => [from, to, type, label]), [["CUSTOMER", "ORDER", "non-identifying", "places"]]);
  assert.deepEqual(state.startStates, ["Ready"]);
  assert.deepEqual(state.edges.map(({ from, to }) => [from, to]), [["Ready", "Done"]]);
  assert.deepEqual(state.nodes.map(({ id }) => id), ["Ready", "Done"]);
});

test("parser CLI keeps syntax-error details and targeted hints", () => {
  const result = parseThroughCli("flowchart TD\nA -> B");

  assert.equal(result.valid, false);
  assert.equal(result.ast, undefined);
  assert.equal(result.error?.line, 2);
  assert.match(result.error?.message ?? "", /Use "-->" for connections, not "->"/);
});

test("parser CLI returns targeted hints for Graphviz syntax and tabs", () => {
  const result = parseThroughCli("digraph G {\n\tA -> B\n}");

  assert.equal(result.valid, false);
  assert.match(result.error?.message ?? "", /This looks like Graphviz syntax/);
  assert.match(result.error?.message ?? "", /Replace tabs with spaces/);
  assert.match(result.error?.message ?? "", /Use "-->" for connections in Mermaid flowcharts/);
});

test("parser CLI supports one-shot and version-info modes", () => {
  const oneShot = runParser([], "flowchart TD\nA --> B\n");
  assert.equal(oneShot.status, 0, oneShot.stderr);
  const parsed = JSON.parse(oneShot.stdout) as { valid: boolean; diagram_type?: string };
  assert.equal(parsed.valid, true);
  assert.equal(parsed.diagram_type, "flowchart");

  const version = runParser(["--version-info"], "");
  assert.equal(version.status, 0, version.stderr);
  const versionInfo = JSON.parse(version.stdout) as { parser_version?: string; mermaid_version?: string };
  assert.ok(versionInfo.parser_version);
  assert.ok(versionInfo.mermaid_version);
});

test("flowchart extraction adds edge nodes when Mermaid omits vertex records", async () => {
  const result = await extractAST(
    controlledMermaid({
      direction: "LR",
      edges: [
        { start: "A", end: "B", type: "arrow" },
        { start: "B", end: "C", type: "arrow" },
      ],
    }),
    "flowchart LR\nA --> B\nB --> C",
    "flowchart",
  );

  assert.deepEqual(result.nodes.map(({ id }) => id), ["A", "B", "C"]);
});

test("sequence extraction includes actors mentioned only in messages", async () => {
  const result = await extractAST(
    controlledMermaid({
      state: { records: { messages: [{ from: "Alice", to: "Bob", type: 1, message: "Hello" }] } },
    }),
    "sequenceDiagram\nAlice->>Bob: Hello",
    "sequence",
  );

  assert.deepEqual(result.nodes.map(({ id, label }) => [id, label]), [
    ["Alice", ""],
    ["Bob", ""],
  ]);
});

test("class extraction maps every Mermaid relation type and preserves inheritance direction", async () => {
  const relations = [
    { type1: 0, type: "aggregation" },
    { type1: 1, type: "extension" },
    { type1: 2, type: "composition" },
    { type1: 3, type: "dependency" },
    { type1: 4, type: "lollipop" },
    { type1: 99, type: "dependency" },
  ];
  const result = await extractAST(
    controlledMermaid({
      relations: relations.map(({ type1 }, index) => ({
        id1: `Base${index}`,
        id2: `Child${index}`,
        relation: { type1 },
      })),
      classes: {},
    }),
    "classDiagram",
    "class",
  );

  assert.deepEqual(result.edges.map(({ from, to, type }) => [from, to, type]), [
    ["Base0", "Child0", "aggregation"],
    ["Child1", "Base1", "extension"],
    ["Base2", "Child2", "composition"],
    ["Base3", "Child3", "dependency"],
    ["Base4", "Child4", "lollipop"],
    ["Base5", "Child5", "dependency"],
  ]);
});

test("state extraction filters synthetic transitions and records valid starts", async () => {
  const result = await extractAST(
    controlledMermaid({
      nodes: [
        { id: "root_start" },
        { id: "root_end" },
        { id: "Ready", label: "Ready" },
        { id: "Done", label: "Done" },
      ],
      edges: [
        { start: "root_start", end: "root_end" },
        { start: "root_end", end: "Ready" },
        { start: "Ready", end: "root_start" },
        { start: "Ready", end: "root_end" },
        { start: "root_start", end: "Ready" },
        { start: "Ready", end: "Done", label: "finish" },
      ],
    }),
    "stateDiagram-v2\n[*] --> Ready\nReady --> Done",
    "state",
  );

  assert.deepEqual(result.startStates, ["Ready"]);
  assert.deepEqual(result.edges.map(({ from, to, label }) => [from, to, label]), [
    ["Ready", "Done", "finish"],
  ]);
});
