import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { parse as parseYaml } from "yaml";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);

function readRepositoryFile(filePath: string): string {
  return readFileSync(path.join(repositoryRoot, filePath), "utf8");
}

type Workflow = {
  on?: {
    pull_request?: {
      paths?: string[];
    };
  };
  jobs?: Record<
    string,
    {
      steps?: Array<{
        name?: string;
        run?: string;
        if?: string;
        env?: Record<string, string>;
        uses?: string;
        with?: Record<string, unknown>;
      }>;
    }
  >;
};

type DependabotConfig = {
  version?: number;
  updates?: Array<{
    "package-ecosystem"?: string;
    directory?: string;
    schedule?: { interval?: string };
  }>;
};

function readWorkflow(filePath: string): Workflow {
  return parseYaml(readRepositoryFile(filePath)) as Workflow;
}

test("integration workflow runs Go integration-tagged suites", () => {
  const workflow = readWorkflow(
    ".github/workflows/integration-contract-tests.yml",
  );
  const step = workflow.jobs?.["contract-integration"]?.steps?.find(
    (candidate) => candidate.name === "Integration-tagged Go suites",
  );
  assert.ok(step?.run, "workflow must run the integration-tagged suites");

  const args = step.run.trim().split(/\s+/);
  assert.equal(args[0], "go");
  assert.equal(args[1], "test");
  assert.ok(args.includes("-tags=integration"));
  assert.ok(args.includes("./cmd/server"));
  assert.ok(args.includes("./internal/api"));
  assert.ok(args.includes("-count=1"));
});

test("race tests run on pull requests that change Go or parser code", () => {
  const workflow = readWorkflow(".github/workflows/race-tests.yml");
  const paths = workflow.on?.pull_request?.paths;
  assert.ok(Array.isArray(paths), "race workflow must declare pull request paths");

  for (const requiredPath of [
    "**/*.go",
    "go.mod",
    "go.sum",
    "parser-node/**",
  ]) {
    assert.ok(
      paths.includes(requiredPath),
      `race workflow must run for changes matching ${requiredPath}`,
    );
  }
});

test("Dependabot updates Go and both npm dependency roots weekly", () => {
  const config = parseYaml(
    readRepositoryFile(".github/dependabot.yml"),
  ) as DependabotConfig;
  const relevantUpdates = config.updates?.filter(
    (update) =>
      update["package-ecosystem"] === "gomod" ||
      update["package-ecosystem"] === "npm",
  );
  assert.equal(config.version, 2);
  assert.deepEqual(
    relevantUpdates
      ?.map((update) => [update["package-ecosystem"], update.directory])
      .sort((left, right) =>
        `${left[0]}:${left[1]}`.localeCompare(`${right[0]}:${right[1]}`),
      ),
    [
      ["gomod", "/"],
      ["npm", "/"],
      ["npm", "/parser-node"],
    ],
  );
  for (const update of relevantUpdates ?? []) {
    assert.equal(update.schedule?.interval, "weekly");
  }
});

test("CI captures runtime coverage and gates only new Fallow findings on pull requests", () => {
  const workflow = readWorkflow(".github/workflows/ci.yml");
  const steps = workflow.jobs?.test?.steps ?? [];
  const checkout = steps.find((step) => step.uses?.startsWith("actions/checkout@"));
  const testStep = steps.find((step) => step.name === "Type-check and test Worker");
  const auditStep = steps.find((step) => step.name === "Audit changed TypeScript code");

  assert.equal(checkout?.with?.["fetch-depth"], 0);
  assert.equal(testStep?.env?.NODE_V8_COVERAGE, "${{ runner.temp }}/merm8-v8coverage");
  assert.equal(auditStep?.if, "github.event_name == 'pull_request'");
  assert.match(auditStep?.run ?? "", /fallow@3\.33\.1 audit --coverage/);
  assert.equal(auditStep?.env?.COVERAGE_DIR, "${{ runner.temp }}/merm8-v8coverage");
});
