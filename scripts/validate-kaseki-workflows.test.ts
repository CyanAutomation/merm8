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
  "kaseki-dry.yaml": "Submit DRY sweep to Soyuz",
  "kaseki-docs.yaml": "Submit documentation sweep to Soyuz",
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

function runSubmission(
  workflowName: string,
  httpStatus: number,
  response: Record<string, unknown>,
): {
  status: number | null;
  stdout: string;
  stderr: string;
  output: string;
  summary: string;
  curlArgs: string;
  payload: Record<string, unknown>;
} {
  const run = readStep(workflowName, submitStepNames[workflowName]).run;
  assert.ok(run, "submission step must define a shell script");

  const directory = mkdtempSync(path.join(os.tmpdir(), "soyuz-submit-"));
  try {
    const binDirectory = path.join(directory, "bin");
    mkdirSync(binDirectory);
    const githubOutput = path.join(directory, "github-output");
    writeFileSync(githubOutput, "");
    const githubStepSummary = path.join(directory, "github-step-summary");
    writeFileSync(githubStepSummary, "");
    const curlArgsFile = path.join(directory, "curl-args");
    const payloadFile = path.join(directory, "payload.json");

    writeExecutable(
      path.join(binDirectory, "curl"),
      [
        "#!/usr/bin/env bash",
        "set -euo pipefail",
        'args=("$@")',
        'output_file=""',
        'payload=""',
        'while (($#)); do',
        '  case "$1" in',
        '    --output) output_file="$2"; shift 2 ;;',
        '    --data) payload="$2"; shift 2 ;;',
        '    *) shift ;;',
        '  esac',
        "done",
        'printf \'%s\\n\' "${args[@]}" > "$MOCK_CURL_ARGS_FILE"',
        'printf \'%s\' "$payload" > "$MOCK_CURL_PAYLOAD_FILE"',
        'printf \'%s\' "$MOCK_CURL_RESPONSE" > "$output_file"',
        'printf \'%s\' "$MOCK_HTTP_STATUS"',
        'if (( MOCK_HTTP_STATUS >= 400 )); then exit 22; fi',
        "",
      ].join("\n"),
    );

    const result = spawnSync("bash", ["-c", run], {
      cwd: repositoryRoot,
      encoding: "utf8",
      timeout: 5_000,
      env: {
        ...process.env,
        PATH: `${binDirectory}:${process.env.PATH ?? ""}`,
        SOYUZ_BASE_URL: "https://soyuz.scheimann.workers.dev",
        SOYUZ_SUBMISSION_API_TOKEN: "test-soyuz-submission-token",
        REPO_URL: "https://github.com/CyanAutomation/merm8",
        REF: "main",
        TASK_DESCRIPTION: "Test task description",
        ALLOWLIST: "src/**/*.ts",
        VALIDATION_COMMAND: "npm test",
        MOCK_HTTP_STATUS: String(httpStatus),
        MOCK_CURL_RESPONSE: JSON.stringify(response),
        MOCK_CURL_ARGS_FILE: curlArgsFile,
        MOCK_CURL_PAYLOAD_FILE: payloadFile,
        GITHUB_OUTPUT: githubOutput,
        GITHUB_STEP_SUMMARY: githubStepSummary,
      },
    });

    return {
      status: result.status,
      stdout: result.stdout,
      stderr: result.stderr,
      output: readFileSync(githubOutput, "utf8"),
      summary: readFileSync(githubStepSummary, "utf8"),
      curlArgs: readFileSync(curlArgsFile, "utf8"),
      payload: JSON.parse(readFileSync(payloadFile, "utf8")) as Record<string, unknown>,
    };
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
}

// @spec: SOYUZ-001: Submission jobs run only on main and use the protected Soyuz environment.
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

  test(`${workflowName} reads the Soyuz URL and submission credential from the soyuz environment`, () => {
    for (const [jobName, job] of readJobs(workflowName)) {
      assert.equal(
        job.environment,
        "soyuz",
        `${workflowName} job ${jobName} must select the Soyuz environment`,
      );
      assert.equal(
        job.env?.SOYUZ_BASE_URL,
        "${{ vars.SOYUZ_BASE_URL }}",
        `${workflowName} job ${jobName} must read the environment-scoped URL`,
      );
      assert.equal(job.env?.KASEKI_BASE_URL, undefined);
      assert.equal(job.env?.KASEKI_ALLOWED_HOSTS, undefined);

      const submit = job.steps?.find((step) => step.name === submitStepNames[workflowName]);
      assert.equal(
        submit?.env?.SOYUZ_SUBMISSION_API_TOKEN,
        "${{ secrets.SOYUZ_SUBMISSION_API_TOKEN }}",
        `${workflowName} job ${jobName} must use the scoped Soyuz submission credential`,
      );
      assert.equal(submit?.env?.KASEKI_SUBMISSION_API_TOKEN, undefined);
      assert.equal(submit?.env?.CLIENT_API_TOKEN, undefined);
      assert.ok(
        job.steps?.filter((step) => step !== submit)
          .every((step) =>
            step.env?.SOYUZ_SUBMISSION_API_TOKEN === undefined &&
            step.env?.KASEKI_SUBMISSION_API_TOKEN === undefined
          ),
        "the submission credential must only be exposed to the submit step",
      );
    }
  });
}

// @spec: SOYUZ-002: Requests preserve bounded PR behavior and use a UUID idempotency key.
for (const workflowName of workflows) {
  test(`${workflowName} submits to Soyuz using the configured base URL`, () => {
    const submitStep = readStep(workflowName, submitStepNames[workflowName]);
    const run = submitStep.run ?? "";

    assert.match(run, /--request POST "\$SOYUZ_BASE_URL\/v1\/runs"/);
    assert.match(run, /Authorization: Bearer \$SOYUZ_SUBMISSION_API_TOKEN/);
    assert.match(run, /Idempotency-Key: \$idempotency_key/);
    assert.match(run, /\bidempotencyKey: \$idempotencyKey/);
    assert.match(run, /taskMode:\s*"patch",\s*publishMode:\s*"pr"/);
    assert.match(run, /publishMode:\s*"pr",\s*timeoutSeconds:\s*10800,\s*maxDiffBytes:\s*102400/);
    assert.match(run, /changedFilesAllowlist:/);
    assert.match(run, /validationCommands:/);
    assert.doesNotMatch(run, /\/api\/v1\/runs|\/status/);
  });

  test(`${workflowName} checks out the triggering commit for validation`, () => {
    const checkout = readJobs(workflowName)
      .flatMap(([, job]) => job.steps ?? [])
      .find((step) => step.uses?.startsWith("actions/checkout@"));
    assert.ok(checkout, `${workflowName} must check out the repository`);
    assert.equal(checkout.with?.ref, "${{ github.sha }}");
    assert.equal(checkout.with?.["persist-credentials"], false);
  });

  test(`${workflowName} accepts only Soyuz fresh-submit and idempotent-replay responses`, () => {
    const run = readStep(workflowName, submitStepNames[workflowName]).run ?? "";
    assert.match(run, /200\|202|202\|200/);
    assert.match(run, /\.data\.id/);
    assert.match(run, /\.data\.displayId/);
    assert.match(run, /\.data\.status/);
    assert.match(run, /scripts\/validate-soyuz-run-id\.ts/);
    assert.match(run, /submission_status=accepted/);
  });

  test(`${workflowName} keeps the Soyuz token out of other steps and non-main triggers`, () => {
    const workflow = readWorkflow(workflowName) as Workflow & {
      on?: Record<string, unknown>;
    };
    const steps = Object.values(workflow.jobs ?? {}).flatMap((job) => job.steps ?? []);
    const sources = steps.map((step) => step.run ?? "").join("\n");
    assert.doesNotMatch(sources, /\/ready|gateway-test|verify-controller-health/);
    assert.doesNotMatch(
      sources,
      /KASEKI_SUBMISSION_API_TOKEN|KASEKI_API_TOKEN|CLIENT_API_TOKEN/,
    );
    assert.equal(workflow.on?.pull_request, undefined);
    assert.equal(workflow.on?.push, undefined);
  });
}

// @spec: SOYUZ-003: Workflows succeed on Soyuz admission and never wait for execution completion.
for (const workflowName of workflows) {
  test(`${workflowName} reports accepted submission without polling for completion`, () => {
    const steps = readJobs(workflowName).flatMap(([, job]) => job.steps ?? []);
    assert.equal(
      steps.some((step) => step.name?.startsWith("Wait for")),
      false,
      "workflow must not poll run status",
    );

    const summaryStep = steps.find((step) => step.name === "Publish Soyuz submission details");
    assert.ok(summaryStep, "workflow must publish the admitted run ID and status");
    assert.equal(summaryStep.if, "always()");
    assert.equal(summaryStep.env?.RUN_ID, "${{ steps.submit.outputs.run_id || 'not submitted' }}");
    assert.equal(summaryStep.env?.DISPLAY_ID, "${{ steps.submit.outputs.display_id || 'not assigned' }}");
    assert.equal(summaryStep.env?.RUN_STATUS, "${{ steps.submit.outputs.run_status || 'not received' }}");
    assert.match(summaryStep.run ?? "", /\$SUBMISSION_STATUS/);
    assert.match(summaryStep.run ?? "", /Soyuz run/);
    assert.match(summaryStep.run ?? "", /admitted the task/i);
    assert.match(summaryStep.run ?? "", /does not confirm execution completion/i);
  });
}

test("Soyuz submit step accepts 202 and 200, and reuses its UUID idempotency key", () => {
  const response = {
    data: {
      id: "01944e2d-73a1-7cc4-b282-efce9adf00d2",
      displayId: "soyuz-213",
      status: "queued",
    },
    requestId: "c899e364-6a40-498e-9cf5-1c003bf012f4",
  };

  for (const httpStatus of [200, 202]) {
    const result = runSubmission("kaseki-dry.yaml", httpStatus, response);
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.output, /submission_status=accepted/);
    assert.match(result.output, /display_id=soyuz-213/);
    assert.match(result.output, /run_status=queued/);
    assert.doesNotMatch(result.output, /idempotency_key=/);
    assert.match(result.curlArgs, /https:\/\/soyuz\.scheimann\.workers\.dev\/v1\/runs/);
    assert.match(result.curlArgs, /Authorization: Bearer test-soyuz-submission-token/);
    const idempotencyHeader = result.curlArgs.match(/Idempotency-Key: ([0-9a-f-]{36})/i)?.[1];
    assert.ok(idempotencyHeader, "request must send the idempotency key header");
    assert.equal(result.payload.idempotencyKey, idempotencyHeader);
  }
});

test("Soyuz submit step fails on non-success HTTP responses", () => {
  for (const workflowName of workflows) {
    const result = runSubmission(workflowName, 503, {
      error: { code: "QUEUE_PUBLISH_UNCERTAIN", message: "retry shortly" },
    });
    assert.notEqual(result.status, 0);
    assert.doesNotMatch(result.output, /submission_status=accepted/);
    assert.match(result.summary, /Soyuz returned HTTP `503`/);
  }
});

test("Soyuz submit step rejects malformed response IDs or lifecycle values", () => {
  for (const data of [
    { id: "not-a-uuid", displayId: "soyuz-213", status: "queued" },
    { id: "01944e2d-73a1-7cc4-b282-efce9adf00d2", displayId: "soyuz-213", status: "unknown" },
  ]) {
    const result = runSubmission("kaseki-dry.yaml", 202, { data });
    assert.notEqual(result.status, 0);
    assert.doesNotMatch(result.output, /submission_status=accepted/);
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

test("kaseki-dry.yaml caps every Soyuz response body", () => {
  const source = readWorkflowSource("kaseki-dry.yaml");
  const responseFileWrites = source.match(/--output "\$response_file"/g) ?? [];
  const responseSizeCaps = source.match(/--max-filesize 1048576/g) ?? [];

  assert.ok(responseFileWrites.length > 0, "workflow must capture Soyuz responses");
  assert.equal(
    responseSizeCaps.length,
    responseFileWrites.length,
    "every response written to disk must have a 1 MiB size cap",
  );
});

test("both submission requests are restricted to HTTPS", () => {
  for (const workflowName of workflows) {
    const submitStep = readStep(workflowName, submitStepNames[workflowName]);
    assert.match(
      submitStep.run ?? "",
      /curl --proto '=https' --tlsv1\.2 --fail-with-body --silent --show-error/,
      `${workflowName} must restrict credential-bearing requests to HTTPS`,
    );
  }
});

test("all workflow shell scripts parse with Bash", () => {
  for (const workflowName of workflows) {
    for (const step of readJobs(workflowName).flatMap(([, job]) => job.steps ?? [])) {
      if (!step.run) continue;
      const result = spawnSync("bash", ["-n"], {
        encoding: "utf8",
        input: step.run,
      });
      assert.equal(
        result.status,
        0,
        `${workflowName} step "${step.name}" must have valid Bash syntax: ${result.stderr}`,
      );
    }
  }
});
