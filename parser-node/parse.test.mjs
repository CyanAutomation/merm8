import test from "node:test";
import assert from "node:assert/strict";

import { withWorkerTimeout } from "./parse.mjs";

function createTimerHarness() {
  let nextId = 0;
  const scheduled = new Map();
  const cleared = [];

  return {
    timer: {
      setTimeout(fn, _timeoutMs) {
        const id = ++nextId;
        scheduled.set(id, fn);
        return id;
      },
      clearTimeout(id) {
        cleared.push(id);
        scheduled.delete(id);
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

test("withWorkerTimeout clears timeout when wrapped promise resolves", async () => {
  const harness = createTimerHarness();

  const result = await withWorkerTimeout(Promise.resolve("ok"), 25, harness.timer);

  assert.equal(result, "ok");
  assert.deepEqual(harness.getCleared(), [1]);
});

test("withWorkerTimeout clears timeout when wrapped promise rejects", async () => {
  const harness = createTimerHarness();
  const expected = new Error("boom");

  await assert.rejects(
    withWorkerTimeout(Promise.reject(expected), 25, harness.timer),
    expected,
  );
  assert.deepEqual(harness.getCleared(), [1]);
});

test("withWorkerTimeout keeps WORKER_TIMEOUT error code for timeout failures", async () => {
  const harness = createTimerHarness();
  let timeoutErr;

  const pending = withWorkerTimeout(new Promise(() => {}), 25, harness.timer).catch(
    (err) => {
      timeoutErr = err;
      throw err;
    },
  );

  harness.triggerTimeout(1);

  await assert.rejects(pending, (err) => {
    assert.equal(err?.code, "WORKER_TIMEOUT");
    assert.equal(err?.message, "worker parse timeout");
    return true;
  });
  assert.equal(timeoutErr?.code, "WORKER_TIMEOUT");
  assert.deepEqual(harness.getCleared(), []);
});
