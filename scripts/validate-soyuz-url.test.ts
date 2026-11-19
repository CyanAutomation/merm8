import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { validateSoyuzBaseUrl } from "./validate-soyuz-url.ts";

const soyuzUrl = "https://soyuz.scheimann.workers.dev";

test("accepts the configured Soyuz origin and normalizes a trailing slash", () => {
  assert.equal(validateSoyuzBaseUrl(soyuzUrl), soyuzUrl);
  assert.equal(validateSoyuzBaseUrl(`${soyuzUrl}/`), soyuzUrl);
  assert.equal(validateSoyuzBaseUrl(`${soyuzUrl}:443`), soyuzUrl);
});

test("rejects other hosts and URL prefixes", () => {
  for (const endpoint of [
    "https://soyuz.scheimann.workers.dev.attacker.example",
    "https://attacker.scheimann.workers.dev",
    "https://workers.dev",
    `${soyuzUrl}/api`,
    `${soyuzUrl}:8443`,
  ]) {
    assert.throws(() => validateSoyuzBaseUrl(endpoint), endpoint);
  }
});

test("rejects non-HTTPS endpoints", () => {
  assert.throws(() => validateSoyuzBaseUrl("http://soyuz.scheimann.workers.dev"));
});

test("rejects endpoints with embedded credentials", () => {
  assert.throws(() =>
    validateSoyuzBaseUrl("https://user:secret@soyuz.scheimann.workers.dev"),
  );
});

test("rejects malformed endpoints and query or fragment suffixes", () => {
  for (const endpoint of [
    "",
    "soyuz.scheimann.workers.dev",
    "https://",
    `${soyuzUrl}?token=secret`,
    `${soyuzUrl}?`,
    `${soyuzUrl}/#section`,
    `${soyuzUrl}/#`,
  ]) {
    assert.throws(() => validateSoyuzBaseUrl(endpoint), endpoint);
  }
});

test("writes the normalized Soyuz URL through the GitHub Actions environment file", () => {
  const directory = mkdtempSync(path.join(tmpdir(), "soyuz-url-test-"));
  const githubEnv = path.join(directory, "github-env");
  const script = fileURLToPath(
    new URL("./validate-soyuz-url.ts", import.meta.url),
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
          SOYUZ_BASE_URL: `${soyuzUrl}/`,
        },
      },
    );

    assert.equal(result.status, 0, result.stderr);
    assert.equal(readFileSync(githubEnv, "utf8"), `SOYUZ_BASE_URL=${soyuzUrl}\n`);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
