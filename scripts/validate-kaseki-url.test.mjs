import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { validateKasekiBaseUrl } from "./validate-kaseki-url.mjs";

test("accepts any HTTPS Kaseki endpoint, including a custom host and path", () => {
  assert.equal(
    validateKasekiBaseUrl("https://controller.example.net:9443/kaseki"),
    "https://controller.example.net:9443/kaseki",
  );
  assert.equal(
    validateKasekiBaseUrl("https://192.0.2.44:8443"),
    "https://192.0.2.44:8443",
  );
});

test("rejects non-HTTPS endpoints", () => {
  assert.throws(() => validateKasekiBaseUrl("http://controller.example.net"));
});

test("rejects endpoints with embedded credentials", () => {
  assert.throws(() =>
    validateKasekiBaseUrl("https://user:secret@controller.example.net"),
  );
});

test("rejects malformed endpoints and query or fragment suffixes", () => {
  for (const endpoint of [
    "",
    "controller.example.net",
    "https://",
    "https://controller.example.net?token=secret",
    "https://controller.example.net?",
    "https://controller.example.net/#section",
    "https://controller.example.net/#",
  ]) {
    assert.throws(() => validateKasekiBaseUrl(endpoint), endpoint);
  }
});

test("normalizes the base URL through the GitHub Actions environment file", () => {
  const directory = mkdtempSync(path.join(tmpdir(), "kaseki-url-test-"));
  const githubEnv = path.join(directory, "github-env");
  const script = fileURLToPath(new URL("./validate-kaseki-url.mjs", import.meta.url));

  try {
    const result = spawnSync(process.execPath, [script], {
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_ENV: githubEnv,
        KASEKI_BASE_URL: "https://controller.example.org/kaseki///",
      },
    });

    assert.equal(result.status, 0, result.stderr);
    assert.equal(
      readFileSync(githubEnv, "utf8"),
      "KASEKI_BASE_URL=https://controller.example.org/kaseki\n",
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
