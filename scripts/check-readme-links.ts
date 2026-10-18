import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const markdownLinkPattern = /\[((?:\\.|[^\]])*)\]\((<[^>]+>|[^)\s]+)(?:\s+[^)]*)?\)/g;
type BrokenLink = { text: string; destination: string; line: number };

const isExternal = (destination: string): boolean =>
  /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(destination);

export async function findBrokenLocalLinks(
  markdown: string,
  baseDirectory: string,
  sourceFile = "README.md",
): Promise<BrokenLink[]> {
  const broken: BrokenLink[] = [];
  const sourceDirectory = path.dirname(path.resolve(baseDirectory, sourceFile));

  for (const match of markdown.matchAll(markdownLinkPattern)) {
    const text = match[1].replace(/\\([\\\]])/g, "$1");
    const destination = match[2].replace(/^<|>$/g, "");
    if (!destination || isExternal(destination)) continue;

    const localPath = destination.split(/[?#]/, 1)[0];
    if (!localPath) continue;

    let decodedPath;
    try {
      decodedPath = decodeURIComponent(localPath);
    } catch {
      decodedPath = localPath;
    }

    const target = path.resolve(sourceDirectory, decodedPath);
    let exists = false;
    try {
      exists = (await stat(target)).isFile();
    } catch {
      // Missing or inaccessible local link target.
    }

    if (!exists) {
      const line = markdown.slice(0, match.index ?? 0).split("\n").length;
      broken.push({ text, destination, line });
    }
  }

  return broken;
}

async function main() {
  const readmePath = fileURLToPath(new URL("../README.md", import.meta.url));
  const markdown = await readFile(readmePath, "utf8");
  const broken = await findBrokenLocalLinks(markdown, path.dirname(readmePath));
  if (broken.length) {
    for (const link of broken) {
      process.stderr.write(`README.md:${link.line}: [${link.text}] points to missing local file ${link.destination}\n`);
    }
    process.exitCode = 1;
    return;
  }
  process.stdout.write("README.md local link targets are valid.\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
