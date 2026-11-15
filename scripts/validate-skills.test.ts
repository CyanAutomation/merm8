import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { parse as parseYaml } from "yaml";

const repositoryRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "..",
);
const skillsRoot = path.join(repositoryRoot, ".github", "skills");
const skillsIndexPath = path.join(skillsRoot, "README.md");
const packageJson = JSON.parse(
  readFileSync(path.join(repositoryRoot, "package.json"), "utf8"),
) as { scripts?: Record<string, string> };
const makefile = readFileSync(path.join(repositoryRoot, "Makefile"), "utf8");
const requiredSections = [
  "## Inputs / Outputs / Non-goals",
  "## Trigger conditions",
  "## Mandatory rules",
  "## Validation checklist",
  "## Expected output format",
  "## Failure/stop conditions",
];

const skillDirectories = readdirSync(skillsRoot, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name)
  .sort();

// @spec: SKILLS-001: Each repository skill has valid metadata and required guidance sections.
test("repository skills have valid metadata and required sections", () => {
  assert.ok(skillDirectories.length > 0, "at least one skill must be present");

  for (const directory of skillDirectories) {
    const skillPath = path.join(skillsRoot, directory, "SKILL.md");
    assert.ok(existsSync(skillPath), `${directory} must contain SKILL.md`);
    const contents = readFileSync(skillPath, "utf8");
    const frontMatter = contents.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);

    assert.ok(frontMatter, `${directory}/SKILL.md must begin with YAML front matter`);

    const metadata = parseYaml(frontMatter[1]) as {
      name?: unknown;
      description?: unknown;
    };
    const name = typeof metadata.name === "string" ? metadata.name : "";
    const description =
      typeof metadata.description === "string" ? metadata.description : "";
    assert.equal(metadata.name, directory, `${directory}/SKILL.md name must match its directory`);
    assert.equal(typeof metadata.description, "string", `${directory}/SKILL.md needs a description`);
    assert.ok(
      description.trim().length >= 20,
      `${directory}/SKILL.md description should explain purpose and use`,
    );
    assert.ok(name.length > 0, `${directory}/SKILL.md needs a non-empty name`);

    for (const section of requiredSections) {
      assert.ok(contents.includes(section), `${directory}/SKILL.md needs ${section}`);
    }
  }
});

// @spec: SKILLS-002: Declared skill inputs exist and referenced project commands remain available.
test("skill inputs and documented project commands resolve", () => {
  const makeTargets = new Set(
    [...makefile.matchAll(/^([A-Za-z0-9][A-Za-z0-9_-]*):/gm)].map((match) => match[1]),
  );

  for (const directory of skillDirectories) {
    const skillPath = path.join(skillsRoot, directory, "SKILL.md");
    const contents = readFileSync(skillPath, "utf8");
    const inputsLine = contents.match(/^- Inputs:.*$/m)?.[0] ?? "";
    const inputPaths = [...inputsLine.matchAll(/`([^`]+)`/g)]
      .map((match) => match[1])
      .filter((reference) => /[./]/.test(reference));

    for (const reference of inputPaths) {
      assert.ok(
        existsSync(path.resolve(repositoryRoot, reference)),
        `${directory}/SKILL.md input path does not exist: ${reference}`,
      );
    }

    const inlineCode = [...contents.matchAll(/`([^`]+)`/g)].map((match) => match[1]);
    for (const reference of inlineCode) {
      const npmScript = reference.match(/^npm run ([A-Za-z0-9:_-]+)/)?.[1];
      if (npmScript) {
        assert.ok(
          packageJson.scripts?.[npmScript],
          `${directory}/SKILL.md references missing npm script: ${npmScript}`,
        );
      }

      const makeTarget = reference.match(/^make ([A-Za-z0-9_-]+)$/)?.[1];
      if (makeTarget) {
        assert.ok(
          makeTargets.has(makeTarget),
          `${directory}/SKILL.md references missing Make target: ${makeTarget}`,
        );
      }
    }
  }
});

// @spec: SKILLS-003: The skills index links every available skill.
test("skills index links each skill", () => {
  assert.ok(existsSync(skillsIndexPath), ".github/skills/README.md must exist");
  const index = readFileSync(skillsIndexPath, "utf8");

  for (const directory of skillDirectories) {
    assert.ok(
      index.includes(`./${directory}/SKILL.md`),
      `skills index must link ${directory}/SKILL.md`,
    );
  }
});
