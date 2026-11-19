import assert from "node:assert/strict";
import test from "node:test";

import { validateSoyuzRunId } from "./validate-soyuz-run-id.ts";

test("accepts canonical Soyuz UUIDv7 run IDs", () => {
  const runId = "01944e2d-73a1-7cc4-b282-efce9adf00d2";
  assert.equal(validateSoyuzRunId(runId), runId);
  assert.equal(
    validateSoyuzRunId("01944E2D-73A1-7CC4-B282-EFCE9ADF00D2"),
    "01944E2D-73A1-7CC4-B282-EFCE9ADF00D2",
  );
});

test("rejects non-v7, malformed, and output-injecting run IDs", () => {
  for (const runId of [
    "",
    "01944e2d-73a1-4cc4-b282-efce9adf00d2",
    "01944e2d-73a1-7cc4-7282-efce9adf00d2",
    "run-123",
    "01944e2d73a17cc4b282efce9adf00d2",
    "01944e2d-73a1-7cc4-b282-efce9adf00d2\nINJECTED=value",
    "../other-run",
  ]) {
    assert.throws(() => validateSoyuzRunId(runId), runId);
  }
});

test("rejects non-string run IDs", () => {
  for (const runId of [undefined, null, 42, {}, []]) {
    assert.throws(() => validateSoyuzRunId(runId));
  }
});
