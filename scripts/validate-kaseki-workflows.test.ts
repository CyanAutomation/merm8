import assert from "node:assert/strict";
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { parse as parseYaml } from "yaml";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const workflows = ["kaseki-dry.yaml", "kaseki-docs.yaml"];
const submitStepNames: Record<string, string> = {
  "kaseki-dry.yaml": "Submit DRY sweep",
  "kaseki-docs.yaml": "Submit documentation sweep",
};

type WorkflowStep = {
  name?: string;
  uses?: string;
  if?: string;
  run?: string;
  with?: Record<string, unknown>;
  env?: Record<string, string>;
};

type WorkflowJob = {
  if?: string;
  environment?: string;
  env?: Record<string, string>;
  steps?: WorkflowStep[];
};

type Workflow = { jobs?: Record<string, WorkflowJob> };

function readWorkflowSource(workflowName: string): string {
  return readFileSync(
    path.join(repositoryRoot, ".github", "workflows", workflowName),
    "utf8",
  );
}

function readWorkflow(workflowName: string): Workflow {
  return parseYaml(readWorkflowSource(workflowName)) as Workflow;
}

function readJobs(workflowName: string): [string, WorkflowJob][] {
  const jobs = Object.entries(readWorkflow(workflowName).jobs ?? {});
  assert.ok(jobs.length > 0, `${workflowName} must contain at least one job`);
  return jobs;
}

function readStep(workflowName: string, stepName: string): WorkflowStep {
  for (const [, job] of readJobs(workflowName)) {
    const step = job.steps?.find((candidate) => candidate.name === stepName);
    if (step) return step;
  }
  assert.fail(`${workflowName} must define the "${stepName}" step`);
}

function writeExecutable(filePath: string, contents: string): void {
  writeFileSync(filePath, contents);
  chmodSync(filePath, 0o755);
}

// @spec: KASEKI-001: Kaseki jobs run only on main and bind configuration to the protected environment.
for (const workflowName of workflows) {
  test(`${workflowName} only runs jobs dispatched from main`, () => {
    for (const [jobName, job] of readJobs(workflowName)) {
      assert.equal(
        job.if,
        "github.ref == 'refs/heads/main'",
        `${workflowName} job ${jobName} must be guarded by the main ref`,
      );
    }
  });
}

// @spec: KASEKI-001: Every Kaseki job reads its controller settings from the environment.
for (const workflowName of workflows) {
  test(`${workflowName} reads Kaseki configuration from the environment-bound jobs`, () => {
    for (const [jobName, job] of readJobs(workflowName)) {
      assert.equal(
        job.environment,
        "kaseki-agent",
        `${workflowName} job ${jobName} must select the Kaseki environment`,
      );
      assert.equal(
        job.env?.KASEKI_BASE_URL,
        "${{ vars.KASEKI_BASE_URL }}",
        `${workflowName} job ${jobName} must read the environment-scoped base URL`,
      );
      assert.equal(
        job.env?.KASEKI_ALLOWED_HOSTS,
        "${{ vars.KASEKI_ALLOWED_HOSTS }}",
        `${workflowName} job ${jobName} must read the environment-scoped host allowlist`,
      );
    }
  });
}

// @spec: KASEKI-002: Kaseki submissions use the triggering commit, bounded PRs, main, and idempotency keys.
for (const workflowName of workflows) {
  test(`${workflowName} submits Kaseki runs against the main branch`, () => {
    const job = readJobs(workflowName).map(([, value]) => value).find((value) =>
      value.steps?.some((step) => step.name === submitStepNames[workflowName]),
    );
    assert.ok(job, `${workflowName} must define its submit job`);
    assert.equal(job.env?.REF, "main");

    const submitStep = readStep(workflowName, submitStepNames[workflowName]);
    assert.match(submitStep.run ?? "", /--arg ref "\$REF"/);
    assert.match(submitStep.run ?? "", /\bref:\s*\$ref\b/);
  });

  test(`${workflowName} checks out the triggering commit for validation`, () => {
    const checkout = readJobs(workflowName)
      .flatMap(([, job]) => job.steps ?? [])
      .find((step) => step.uses?.startsWith("actions/checkout@"));
    assert.ok(checkout, `${workflowName} must check out the repository`);
    assert.equal(checkout.with?.ref, "${{ github.sha }}");
    assert.equal(checkout.with?.["persist-credentials"], false);
  });

  test(`${workflowName} submits bounded normal pull requests`, () => {
    const run = readStep(workflowName, submitStepNames[workflowName]).run ?? "";
    assert.match(run, /taskMode:\s*"patch",\s*publishMode:\s*"pr"/);
    assert.match(run, /publishMode:\s*"pr",\s*timeoutSeconds:\s*10800,\s*maxDiffBytes:\s*102400/);
  });

  test(`${workflowName} makes submission retries idempotent`, () => {
    const run = readStep(workflowName, submitStepNames[workflowName]).run ?? "";
    assert.match(run, /idempotency_key=/);
    assert.match(run, /idempotencyKey:\s*\$idempotencyKey/);
  });

  test(`${workflowName} only reports successful Kaseki completion for exit code zero`, () => {
    const run = readStep(workflowName, "Wait for Kaseki completion").run ?? "";
    const completedBranch = run.indexOf("completed)");
    const nonZeroGuard = run.indexOf('if [[ "$exit_code" != 0 ]]', completedBranch);
    const successStatus = run.indexOf('echo "status=completed"', nonZeroGuard);

    assert.ok(completedBranch >= 0, "wait step must handle completed runs");
    assert.ok(nonZeroGuard > completedBranch, "completed runs must validate exitCode");
    assert.ok(
      successStatus > nonZeroGuard,
      "the workflow must only publish completed after the zero exit-code guard",
    );
    assert.match(run, /failed\|cancelled/);
  });

  test(`${workflowName} always publishes the run ID and final status`, () => {
    const summaryStep = readStep(workflowName, "Publish run details");
    assert.equal(summaryStep.if, "always()");
    assert.equal(summaryStep.env?.RUN_ID, "${{ steps.submit.outputs.run_id || 'not submitted' }}");
    assert.equal(summaryStep.env?.FINAL_STATUS, "${{ steps.wait.outputs.status || 'not completed' }}");
    assert.match(summaryStep.run ?? "", /\| Kaseki run \| .*?\$RUN_ID/);
    assert.match(summaryStep.run ?? "", /\| Final status \| .*?\$FINAL_STATUS/);
  });
}

// @spec: KASEKI-003: Transient status failures retry; only a zero exit code is successful.
test("kaseki-dry.yaml retries status polling after a transient controller failure", () => {
  const run = readStep("kaseki-dry.yaml", "Wait for Kaseki completion").run;
  assert.ok(run, "wait step must define a shell script");

  const directory = mkdtempSync(path.join(os.tmpdir(), "kaseki-poll-retry-"));
  const binDirectory = path.join(directory, "bin");
  mkdirSync(binDirectory);
  const curlCountFile = path.join(directory, "curl-count");
  const sleepLog = path.join(directory, "sleep-log");
  const githubOutput = path.join(directory, "github-output");

  writeExecutable(
    path.join(binDirectory, "curl"),
    [
      "#!/bin/sh",
      "count=0",
      'if [ -f "$KASEKI_CURL_COUNT_FILE" ]; then count="$(cat "$KASEKI_CURL_COUNT_FILE")"; fi',
      "count=$((count + 1))",
      'printf \'%s\' "$count" > "$KASEKI_CURL_COUNT_FILE"',
      '[ "$count" -eq 1 ] && exit 22',
      "exit 0",
      "",
    ].join("\n"),
  );
  writeExecutable(
    path.join(binDirectory, "jq"),
    [
      "#!/bin/sh",
      'count="$(cat "$KASEKI_CURL_COUNT_FILE")"',
      'case "$2" in',
      "  '.status | strings')",
      '    [ "$count" -ge 2 ] || exit 1',
      "    printf 'completed\\n'",
      "    ;;",
      "  '.exitCode // 0 | numbers') printf '0\\n' ;;",
      "  *) exit 1 ;;",
      "esac",
      "",
    ].join("\n"),
  );
  writeExecutable(
    path.join(binDirectory, "sleep"),
    ["#!/bin/sh", 'printf \'%s\\n\' "$*" >> "$KASEKI_SLEEP_LOG"', ""].join("\n"),
  );

  try {
    const result = spawnSync("bash", ["-c", run], {
      encoding: "utf8",
      timeout: 5_000,
      env: {
        ...process.env,
        PATH: `${binDirectory}:${process.env.PATH ?? ""}`,
        RUN_ID: "run-123",
        KASEKI_API_TOKEN: "test-token",
        KASEKI_BASE_URL: "https://controller.example.test",
        KASEKI_CURL_COUNT_FILE: curlCountFile,
        KASEKI_SLEEP_LOG: sleepLog,
        GITHUB_OUTPUT: githubOutput,
      },
    });

    assert.equal(result.status, 0, result.stderr);
    assert.equal(readFileSync(curlCountFile, "utf8"), "2");
    assert.equal(readFileSync(sleepLog, "utf8"), "60\n");
    assert.match(readFileSync(githubOutput, "utf8"), /status=completed/);
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});

test("kaseki-docs.yaml installs project dependencies before validation", () => {
  const command = readJobs("kaseki-docs.yaml")[0][1].env?.VALIDATION_COMMAND;
  assert.ok(command, "docs validation command must be explicit");
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

test("kaseki-dry.yaml caps every controller response body", () => {
  const source = readWorkflowSource("kaseki-dry.yaml");
  const responseFileWrites = source.match(/--output "\$response_file"/g) ?? [];
  const responseSizeCaps = source.match(/--max-filesize 1048576/g) ?? [];

  assert.ok(responseFileWrites.length > 0, "workflow must capture controller responses");
  assert.equal(
    responseSizeCaps.length,
    responseFileWrites.length,
    "every response written to disk must have a 1 MiB size cap",
  );
});

test("kaseki-dry.yaml restricts DRY sweep submission to HTTPS", () => {
  const submitStep = readStep("kaseki-dry.yaml", "Submit DRY sweep");

  assert.match(
    submitStep.run ?? "",
    /curl --proto '=https' --silent --show-error \\\n/,
    "the credential-bearing submission request must only use HTTPS",
  );
});
