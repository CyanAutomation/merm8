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
  concurrency?: {
    group?: string;
    "cancel-in-progress"?: boolean | string;
  };
  on?: {
    pull_request?: {
      paths?: string[];
    };
  };
  jobs?: Record<
    string,
    {
      if?: string;
      needs?: string | string[];
      outputs?: Record<string, string>;
      steps?: Array<{
        id?: string;
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

type PackageManifest = {
  engines?: Record<string, string>;
  devDependencies?: Record<string, string>;
};

type NpmLock = {
  packages?: Record<string, { version?: string; integrity?: string }>;
};

type ZizmorConfig = {
  rules?: Record<string, { ignore?: string[] }>;
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

test("CI captures coverage and runs lockfile-installed Fallow for pull requests", () => {
  const workflow = readWorkflow(".github/workflows/ci.yml");
  const steps = workflow.jobs?.test?.steps ?? [];
  const checkout = steps.find((step) => step.uses?.startsWith("actions/checkout@"));
  const installStep = steps.find((step) => step.name === "Install Node dependencies");
  const testStep = steps.find((step) => step.name === "Type-check and test Worker");
  const auditStep = steps.find((step) => step.name === "Audit changed TypeScript code");

  assert.equal(checkout?.with?.["fetch-depth"], 0);
  assert.ok(installStep, "workflow must install locked Node dependencies");
  assert.ok(auditStep, "workflow must audit changed TypeScript code");
  assert.match(installStep?.run ?? "", /npm ci/);
  assert.equal(testStep?.env?.NODE_V8_COVERAGE, "${{ runner.temp }}/merm8-v8coverage");
  assert.equal(auditStep?.if, "github.event_name == 'pull_request'");
  assert.equal(auditStep?.run, "./node_modules/.bin/fallow audit --coverage \"$COVERAGE_DIR\"");
  assert.ok(
    steps.indexOf(installStep) < steps.indexOf(auditStep),
    "Fallow must run after the locked Node dependencies are installed",
  );
  assert.equal(auditStep?.env?.COVERAGE_DIR, "${{ runner.temp }}/merm8-v8coverage");

  const manifest = JSON.parse(readRepositoryFile("package.json")) as PackageManifest;
  const lockfile = JSON.parse(readRepositoryFile("package-lock.json")) as NpmLock;
  assert.equal(manifest.devDependencies?.fallow, "3.33.1");
  assert.equal(lockfile.packages?.["node_modules/fallow"]?.version, "3.33.1");
  assert.match(lockfile.packages?.["node_modules/fallow"]?.integrity ?? "", /^sha512-/);
});

test("README external link workflow installs dependencies before checking links", () => {
  const workflow = readWorkflow(".github/workflows/readme-links.yml");
  const steps = workflow.jobs?.["external-links"]?.steps ?? [];
  const installStep = steps.find((step) => /\bnpm ci\b/.test(step.run ?? ""));
  const checkStep = steps.find((step) =>
    (step.run ?? "").includes("npm run check:readme-links:external"),
  );

  assert.ok(installStep, "workflow must install the locked npm dependencies");
  assert.ok(checkStep, "workflow must run the external README link checker");
  assert.ok(
    steps.indexOf(installStep) < steps.indexOf(checkStep),
    "link checking must run after npm dependencies are installed",
  );
});

test("Zizmor suppression tracks the reusable workflow reference line", () => {
  const ciLines = readRepositoryFile(".github/workflows/ci.yml").split("\n");
  const reusableWorkflowLine = ciLines.findIndex((line) =>
    line.includes("uses: ./.github/workflows/deploy-cloudflare.yml"),
  ) + 1;
  const config = parseYaml(
    readRepositoryFile(".github/zizmor.yml"),
  ) as ZizmorConfig;

  assert.ok(reusableWorkflowLine > 0, "CI must call the deploy workflow");
  assert.ok(
    config.rules?.["self-repository"]?.ignore?.includes(
      `ci.yml:${reusableWorkflowLine}`,
    ),
    "the self-repository suppression must stay on the reusable workflow reference",
  );
});

test("production deployment is limited to Worker bundle changes", () => {
  const workflow = readWorkflow(".github/workflows/ci.yml");
  const detector = workflow.jobs?.["worker-changes"];
  const deploy = workflow.jobs?.["deploy-production"];
  const detectorStep = detector?.steps?.find((step) => step.id === "detect");

  assert.ok(detector, "CI must detect changes that affect the Worker bundle");
  assert.ok(deploy, "CI must define production deployment");
  assert.equal(
    detector?.if,
    "github.event_name == 'push' && github.ref == 'refs/heads/main'",
  );
  assert.equal(
    detector?.outputs?.changed,
    "${{ steps.detect.outputs.changed }}",
  );
  assert.ok(detectorStep?.run, "Worker change detection must run a diff check");
  assert.match(detectorStep?.run ?? "", /git cat-file -e/);
  assert.match(detectorStep?.run ?? "", /git diff --quiet "\$BEFORE_SHA" "\$GITHUB_SHA"/);
  assert.match(detectorStep?.run ?? "", /echo "changed=true"/);
  for (const path of [
    "worker/",
    "src/",
    "package.json",
    "package-lock.json",
    ".npmrc",
    "wrangler.toml",
    "tsconfig.json",
  ]) {
    assert.ok(
      detectorStep.run.includes(path),
      `Worker change detection must include ${path}`,
    );
  }
  assert.ok(
    Array.isArray(deploy?.needs) && deploy.needs.includes("worker-changes"),
    "deployment must depend on Worker change detection",
  );
  assert.match(
    deploy?.if ?? "",
    /needs\.worker-changes\.outputs\.changed == 'true'/,
    "deployment must be skipped when Worker inputs are unchanged",
  );
});

test("main push CI runs are not coalesced before deployment", () => {
  const workflow = readWorkflow(".github/workflows/ci.yml");

  assert.match(
    workflow.concurrency?.group ?? "",
    /github\.event\.pull_request\.number \|\| github\.sha/,
    "main pushes must have per-commit concurrency groups",
  );
  assert.equal(
    workflow.concurrency?.["cancel-in-progress"],
    "${{ github.event_name == 'pull_request' }}",
    "only stale pull request runs should be cancelled",
  );
});

test("changelog describes the main-only manual deployment gate", () => {
  const changelog = readRepositoryFile("CHANGELOG.md");

  assert.match(
    changelog,
    /Production deployments now run only when Worker bundle inputs change on `main`;\s+manual deployments remain limited to a confirmed `main` commit with successful CI\./,
  );
});

test("the repository declares and uses Node.js 24", () => {
  const nodeWorkflows = [
    ".github/workflows/ci.yml",
    ".github/workflows/deploy-cloudflare.yml",
    ".github/workflows/integration-contract-tests.yml",
    ".github/workflows/kaseki-docs.yaml",
    ".github/workflows/kaseki-dry.yaml",
    ".github/workflows/race-tests.yml",
    ".github/workflows/release.yml",
  ];

  for (const filePath of nodeWorkflows) {
    const workflow = readWorkflow(filePath);
    const setupSteps = Object.values(workflow.jobs ?? {})
      .flatMap((job) => job.steps ?? [])
      .filter((step) => step.uses?.startsWith("actions/setup-node@"));

    assert.ok(setupSteps.length > 0, `${filePath} must set up Node.js`);
    for (const step of setupSteps) {
      const nodeVersion = step.with?.["node-version"];
      assert.notEqual(
        nodeVersion,
        undefined,
        `${filePath} must specify the Node.js version`,
      );
      assert.equal(
        String(nodeVersion),
        "24",
        `${filePath} must use Node.js 24`,
      );
    }
  }

  const rootManifest = JSON.parse(readRepositoryFile("package.json")) as PackageManifest;
  const parserManifest = JSON.parse(
    readRepositoryFile("parser-node/package.json"),
  ) as PackageManifest;

  assert.equal(rootManifest.engines?.node, ">=24.0.0");
  assert.equal(parserManifest.engines?.node, ">=24.0.0");
  assert.match(rootManifest.devDependencies?.["@types/node"] ?? "", /^\^24\./);
  assert.equal(readRepositoryFile(".nvmrc").trim(), "24");
});
