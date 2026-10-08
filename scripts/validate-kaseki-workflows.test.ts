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
const submitStepNames: Record<string, string> = {
  "kaseki-dry.yaml": "Submit DRY sweep",
  "kaseki-docs.yaml": "Submit documentation sweep",
};

function readWorkflow(workflowName: string): string {
  return readFileSync(
    path.join(repositoryRoot, ".github", "workflows", workflowName),
    "utf8",
  );
}

function readJobs(workflowName: string): { name: string; source: string }[] {
  const source = readWorkflow(workflowName);
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

function readStep(workflowSource: string, stepName: string): string {
  const marker = `      - name: ${stepName}\n`;
  const start = workflowSource.indexOf(marker);
  assert.notEqual(start, -1, `workflow must define the "${stepName}" step`);

  const stepStart = start + marker.length;
  const nextStep = workflowSource.slice(stepStart).search(/^      - /m);
  return workflowSource.slice(
    stepStart,
    nextStep === -1 ? undefined : stepStart + nextStep,
  );
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

for (const workflowName of workflows) {
  test(`${workflowName} submits Kaseki runs against the main branch`, () => {
    const source = readWorkflow(workflowName);
    const submitStep = readStep(source, submitStepNames[workflowName]);

    assert.match(source, /^      REF: main\s*$/m);
    assert.match(submitStep, /--arg ref "\$REF"/);
    assert.match(submitStep, /^\s+ref: \$ref,\s*$/m);
  });

  test(`${workflowName} checks out the triggering commit for validation`, () => {
    const source = readWorkflow(workflowName);
    assert.match(
      source,
      /^      - uses: actions\/checkout@[^\n]+\n        with:\n          ref: \$\{\{ github\.sha \}\}\n/m,
    );
  });

  test(`${workflowName} submits bounded normal pull requests`, () => {
    const source = readWorkflow(workflowName);
    assert.match(source, /publishMode:\s*"pr"/);
    assert.doesNotMatch(source, /publishMode:\s*"draft_pr"/);
    assert.match(source, /maxDiffBytes:\s*102400/);
  });

  test(`${workflowName} makes submission retries idempotent`, () => {
    const source = readWorkflow(workflowName);
    assert.match(source, /idempotency_key=/);
    assert.match(source, /idempotencyKey:\s*\$idempotencyKey/);
  });

  test(`${workflowName} only reports successful Kaseki completion for exit code zero`, () => {
    const source = readWorkflow(workflowName);
    const waitStep = readStep(source, "Wait for Kaseki completion");
    assert.match(waitStep, /exitCode/);
    assert.match(waitStep, /failed\|cancelled/);
  });

  test(`${workflowName} always publishes the run ID and final status`, () => {
    const source = readWorkflow(workflowName);
    const summaryStep = readStep(source, "Publish run details");
    assert.match(summaryStep, /^        if: always\(\)\s*$/m);
    assert.match(summaryStep, /RUN_ID: \$\{\{ steps\.submit\.outputs\.run_id/);
    assert.match(summaryStep, /FINAL_STATUS: \$\{\{ steps\.wait\.outputs\.status/);
  });
}

test("kaseki-dry.yaml retries polling after temporary controller errors", () => {
  const waitStep = readStep(
    readWorkflow("kaseki-dry.yaml"),
    "Wait for Kaseki completion",
  );
  assert.match(waitStep, /if ! curl/);
  assert.match(waitStep, /polling will continue/);
});

test("kaseki-docs.yaml installs project dependencies before validation", () => {
  const source = readWorkflow("kaseki-docs.yaml");
  const validationCommand = source.match(
    /^      VALIDATION_COMMAND: >-\n((?:        .*\n)+)/m,
  );
  assert.ok(validationCommand, "docs validation command must be explicit");

  const command = validationCommand[1]
    .replace(/^        /gm, "")
    .replace(/\n/g, " ")
    .trim();
  const installRoot = command.indexOf("npm ci");
  const installParser = command.indexOf("npm --prefix parser-node ci");
  const runTests = command.indexOf("npm test");

  assert.ok(installRoot >= 0, "validation must install root dependencies");
  assert.ok(
    installParser > installRoot,
    "validation must install parser dependencies after root dependencies",
  );
  assert.ok(
    runTests > installParser,
    "validation must run tests after installing both dependency sets",
  );
});
