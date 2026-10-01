import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { validateKasekiBaseUrl } from "./validate-kaseki-url.ts";

test("accepts an HTTPS Kaseki endpoint on an explicitly allowed host", () => {
  assert.equal(
    validateKasekiBaseUrl(
      "https://controller.example.net:9443/kaseki",
      "controller.example.net",
    ),
    "https://controller.example.net:9443/kaseki",
  );
  assert.equal(
    validateKasekiBaseUrl("https://192.0.2.44:8443", "192.0.2.44"),
    "https://192.0.2.44:8443",
  );
});

test("rejects missing and malformed Kaseki host allowlists", () => {
  assert.throws(() => validateKasekiBaseUrl("https://controller.example.net"));
  for (const allowedHosts of [
    "",
    "controller.example.net,",
    "https://controller.example.net",
    "*.example.net",
  ]) {
    assert.throws(
      () =>
        validateKasekiBaseUrl("https://controller.example.net", allowedHosts),
      allowedHosts,
    );
  }
});

test("rejects hosts outside the exact allowlist", () => {
  for (const endpoint of [
    "https://evil-controller.example.net",
    "https://controller.example.net.attacker.example",
    "https://attacker.example",
  ]) {
    assert.throws(
      () => validateKasekiBaseUrl(endpoint, "controller.example.net"),
      endpoint,
    );
  }
});

test("rejects non-HTTPS endpoints", () => {
  assert.throws(() =>
    validateKasekiBaseUrl(
      "http://controller.example.net",
      "controller.example.net",
    ),
  );
});

test("rejects endpoints with embedded credentials", () => {
  assert.throws(() =>
    validateKasekiBaseUrl(
      "https://user:secret@controller.example.net",
      "controller.example.net",
    ),
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
    assert.throws(
      () => validateKasekiBaseUrl(endpoint, "controller.example.net"),
      endpoint,
    );
  }
});

test("normalizes the base URL through the GitHub Actions environment file", () => {
  const directory = mkdtempSync(path.join(tmpdir(), "kaseki-url-test-"));
  const githubEnv = path.join(directory, "github-env");
  const script = fileURLToPath(
    new URL("./validate-kaseki-url.ts", import.meta.url),
  );

  try {
    const result = spawnSync(
      process.execPath,
      ["--experimental-strip-types", script],
      {
        encoding: "utf8",
        env: {
          ...process.env,
          GITHUB_ENV: githubEnv,
          KASEKI_BASE_URL: "https://controller.example.org/kaseki///",
          KASEKI_ALLOWED_HOSTS: "controller.example.org",
        },
      },
    );

    assert.equal(result.status, 0, result.stderr);
    assert.equal(
      readFileSync(githubEnv, "utf8"),
      "KASEKI_BASE_URL=https://controller.example.org/kaseki\n",
    );
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
