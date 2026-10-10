import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";

import { withWorkerTimeout } from "./parse.ts";

const parserScript = fileURLToPath(new URL("./parse.ts", import.meta.url));

function parseThroughCli(source: string) {
  const result = spawnSync(process.execPath, ["--experimental-strip-types", parserScript, "--worker"], {
    cwd: dirname(parserScript),
    encoding: "utf8",
    input: `${JSON.stringify({ id: "test", code: source, timeout_ms: 10_000 })}\n`,
    timeout: 15_000,
  });
  assert.equal(result.status, 0, result.stderr);
  const envelope = JSON.parse(result.stdout) as { result: {
    valid: boolean;
    diagram_type?: string;
    error?: { message: string; line: number; column: number };
    ast?: {
      direction: string;
      nodes: Array<{ id: string; line?: number; column?: number }>;
      edges: Array<{ from: string; to: string; line?: number; column?: number }>;
      suppressions: Array<{ ruleId: string; scope: string; line: number; targetLine: number }>;
    };
  } };
  return envelope.result;
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

test("parser CLI keeps syntax-error details and targeted hints", () => {
  const result = parseThroughCli("flowchart TD\nA -> B");

  assert.equal(result.valid, false);
  assert.equal(result.ast, undefined);
  assert.equal(result.error?.line, 2);
  assert.match(result.error?.message ?? "", /Use "-->" for connections, not "->"/);
});
