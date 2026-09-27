import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const workflows = ["kaseki-dry.yaml", "kaseki-docs.yaml"];

function readJobs(workflowName) {
  const source = readFileSync(
    path.join(repositoryRoot, ".github", "workflows", workflowName),
    "utf8",
  );
  const jobsMatch = source.match(/^jobs:\s*\n([\s\S]*)$/m);
  assert.ok(jobsMatch, `${workflowName} must define jobs`);

  const jobsSource = jobsMatch[1];
  const jobHeaders = [...jobsSource.matchAll(/^  ([A-Za-z0-9_-]+):\s*$/gm)];
  assert.ok(
    jobHeaders.length > 0,
    `${workflowName} must contain at least one job`,
  );

  return jobHeaders.map((header, index) => {
    const start = header.index ?? 0;
    const end = jobHeaders[index + 1]?.index ?? jobsSource.length;
    return { name: header[1], source: jobsSource.slice(start, end) };
  });
}

for (const workflowName of workflows) {
  test(`${workflowName} only runs jobs dispatched from main`, () => {
    const jobs = readJobs(workflowName);
    for (const job of jobs) {
      assert.match(
        job.source,
        /^    if:\s*github\.ref == 'refs\/heads\/main'\s*$/m,
        `${workflowName} job ${job.name} must be guarded by the main ref`,
      );
    }
  });
}

for (const workflowName of workflows) {
  test(`${workflowName} reads Kaseki configuration from the environment-bound jobs`, () => {
    const jobs = readJobs(workflowName);
    for (const job of jobs) {
      assert.match(
        job.source,
        /^    environment: kaseki-agent\s*$/m,
        `${workflowName} job ${job.name} must select the Kaseki environment`,
      );
      assert.match(
        job.source,
        /^      KASEKI_BASE_URL: \$\{\{ vars\.KASEKI_BASE_URL \}\}\s*$/m,
        `${workflowName} job ${job.name} must read the environment-scoped base URL`,
      );
      assert.match(
        job.source,
        /^      KASEKI_ALLOWED_HOSTS: \$\{\{ vars\.KASEKI_ALLOWED_HOSTS \}\}\s*$/m,
        `${workflowName} job ${job.name} must read the environment-scoped host allowlist`,
      );
    }
  });
}
