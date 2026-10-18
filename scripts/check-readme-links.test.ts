import assert from "node:assert/strict";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { findBrokenExternalLinks, findBrokenLocalLinks } from "./check-readme-links.ts";

const withFixture = async (run: (root: string) => Promise<void>): Promise<void> => {
  const root = await mkdtemp(path.join(os.tmpdir(), "merm8-readme-links-"));
  try {
    await mkdir(path.join(root, "docs"));
    await writeFile(path.join(root, "README.md"), "# Home\n\n## Setup\n");
    await writeFile(path.join(root, "docs", "guide.md"), "# Guide\n\n## Usage\n");
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

test("reports missing heading fragments in relative Markdown targets", async () => {
  await withFixture(async root => {
    const broken = await findBrokenLocalLinks("[Guide](docs/guide.md#missing-section)", root);
    assert.equal(broken.length, 1);
    assert.equal(broken[0].destination, "docs/guide.md#missing-section");
    assert.equal(broken[0].reason, "missing-anchor");
  });
});

test("validates fragment-only links against the README", async () => {
  await withFixture(async root => {
    const markdown = "# Home\n\n## Setup\n\n[Setup](#setup) [Missing](#missing)";
    const broken = await findBrokenLocalLinks(markdown, root);
    assert.equal(broken.length, 1);
    assert.equal(broken[0].destination, "#missing");
    assert.equal(broken[0].reason, "missing-anchor");
  });
});

test("recognizes GitHub duplicate-heading anchors", async () => {
  await withFixture(async root => {
    const markdown = "# Home\n\n## Setup\n\n## Setup\n";
    const broken = await findBrokenLocalLinks(`${markdown}\n[Second setup](#setup-1)`, root);
    assert.deepEqual(broken, []);
  });
});

test("accepts existing directory Markdown targets", async () => {
  await withFixture(async root => {
    const broken = await findBrokenLocalLinks("[Docs](docs/)", root);
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

test("ignores external URLs and accepts a valid fragment-only link", async () => {
  await withFixture(async root => {
    const markdown = "# Home\n\n[site](https://example.com) [email](mailto:help@example.com) [top](#home)";
    assert.deepEqual(await findBrokenLocalLinks(markdown, root), []);
  });
});

test("checks unique HTTP links and reports unavailable targets", async () => {
  const requests: string[] = [];
  const markdown = "[ok](https://example.test/ok) [missing](https://example.test/missing) [again](https://example.test/ok) [email](mailto:help@example.test)";
  const broken = await findBrokenExternalLinks(markdown, async (input, init) => {
    requests.push(`${init?.method}:${String(input)}`);
    return new Response(null, { status: String(input).endsWith("/missing") ? 404 : 200 });
  });

  assert.equal(requests.length, 2);
  assert.equal(broken.length, 1);
  assert.equal(broken[0].destination, "https://example.test/missing");
});

test("retries an external link with GET when HEAD is unsupported", async () => {
  const methods: string[] = [];
  const broken = await findBrokenExternalLinks("[site](https://example.test/site)", async (_input, init) => {
    methods.push(init?.method ?? "GET");
    return new Response(null, { status: init?.method === "HEAD" ? 405 : 200 });
  });

  assert.deepEqual(methods, ["HEAD", "GET"]);
  assert.deepEqual(broken, []);
});
