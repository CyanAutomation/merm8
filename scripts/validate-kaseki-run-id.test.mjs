import assert from "node:assert/strict";
import test from "node:test";

import { validateKasekiRunId } from "./validate-kaseki-run-id.mjs";

test("accepts run IDs composed of safe URL and output characters", () => {
  for (const runId of ["1", "run_abc-123", "A".repeat(128)]) {
    assert.equal(validateKasekiRunId(runId), runId);
  }
});

test("rejects empty, overlong, and output-injecting run IDs", () => {
  for (const runId of [
    "",
    "A".repeat(129),
    "run id",
    "run=other",
    "run\nINJECTED=value",
    "../other-run",
  ]) {
    assert.throws(() => validateKasekiRunId(runId), runId);
  }
});

test("rejects non-string run IDs", () => {
  for (const runId of [undefined, null, 42, {}, []]) {
    assert.throws(() => validateKasekiRunId(runId));
  }
});
