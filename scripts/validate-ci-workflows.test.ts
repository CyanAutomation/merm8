import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);

function readRepositoryFile(filePath: string): string {
  return readFileSync(path.join(repositoryRoot, filePath), "utf8");
}

test("integration workflow runs Go integration-tagged suites", () => {
  const workflow = readRepositoryFile(
    ".github/workflows/integration-contract-tests.yml",
  );
  assert.match(
    workflow,
    /go test -tags=integration \.\/cmd\/server \.\/internal\/api -count=1/,
  );
});

test("race tests run on pull requests that change Go or parser code", () => {
  const workflow = readRepositoryFile(".github/workflows/race-tests.yml");
  const pullRequest = workflow.match(
    /^  pull_request:\n((?:    .*\n|      .*\n)+)/m,
  );
  assert.ok(pullRequest, "race workflow must declare pull_request paths");
  assert.match(pullRequest[1], /^      - "\*\*\/\*\.go"$/m);
  assert.match(pullRequest[1], /^      - "go\.mod"$/m);
  assert.match(pullRequest[1], /^      - "go\.sum"$/m);
  assert.match(pullRequest[1], /^      - "parser-node\/\*\*"$/m);
});

test("Dependabot updates Go and both npm dependency roots weekly", () => {
  const config = readRepositoryFile(".github/dependabot.yml");
  assert.match(
    config,
    /package-ecosystem: gomod\n    directory: \/\n    schedule:\n      interval: weekly/,
  );

  const npmDirectories = [
    ...config.matchAll(/package-ecosystem: npm\n    directory: (.+)$/gm),
  ].map((match) => match[1]);
  assert.deepEqual(npmDirectories, ["/", "/parser-node"]);
  assert.equal((config.match(/interval: weekly/g) ?? []).length, 4);
});
