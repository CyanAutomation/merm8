import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { findBrokenLocalLinks } from "./check-readme-links.ts";

const withFixture = async (run: (root: string) => Promise<void>): Promise<void> => {
  const root = await mkdtemp(path.join(os.tmpdir(), "merm8-readme-links-"));
  try {
    await mkdir(path.join(root, "docs"));
    await writeFile(path.join(root, "docs", "guide.md"), "# Guide\n");
    await run(root);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
};

test("accepts existing relative Markdown targets", async () => {
  await withFixture(async root => {
    const broken = await findBrokenLocalLinks("[Guide](docs/guide.md#guide)", root);
    assert.deepEqual(broken, []);
  });
});

test("reports missing local targets with their link text and destination", async () => {
  await withFixture(async root => {
    const broken = await findBrokenLocalLinks("[FAQ](docs/missing.md)", root);
    assert.equal(broken.length, 1);
    assert.equal(broken[0].destination, "docs/missing.md");
    assert.equal(broken[0].text, "FAQ");
  });
});

test("ignores external URLs and fragment-only links", async () => {
  await withFixture(async root => {
    const markdown = "[site](https://example.com) [email](mailto:help@example.com) [top](#guide)";
    assert.deepEqual(await findBrokenLocalLinks(markdown, root), []);
  });
});
