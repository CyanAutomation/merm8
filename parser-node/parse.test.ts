import test from "node:test";
import assert from "node:assert/strict";

import { withWorkerTimeout } from "./parse.ts";

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
