import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const markdownLinkPattern = /\[((?:\\.|[^\]])*)\]\((<[^>]+>|[^)\s]+)(?:\s+[^)]*)?\)/g;
type BrokenLink = { text: string; destination: string; line: number; reason: string };
type MarkdownLink = { text: string; destination: string; line: number };
type LinkFetcher = (input: string | URL, init?: RequestInit) => Promise<Response>;

const isExternal = (destination: string): boolean =>
  /^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(destination);

function markdownLinks(markdown: string): MarkdownLink[] {
  return [...markdown.matchAll(markdownLinkPattern)].map(match => ({
    text: match[1].replace(/\\([\\\]])/g, "$1"),
    destination: match[2].replace(/^<|>$/g, ""),
    line: markdown.slice(0, match.index ?? 0).split("\n").length,
  }));
}

function withoutFencedCode(markdown: string): string {
  const lines: string[] = [];
  let fenceCharacter = "";
  let fenceLength = 0;

  for (const line of markdown.split("\n")) {
    const fence = line.match(/^ {0,3}(`{3,}|~{3,})/);
    if (!fenceCharacter && fence) {
      fenceCharacter = fence[1][0];
      fenceLength = fence[1].length;
      continue;
    }
    if (fenceCharacter) {
      if (fence && fence[1][0] === fenceCharacter && fence[1].length >= fenceLength) {
        fenceCharacter = "";
        fenceLength = 0;
      }
      continue;
    }
    lines.push(line);
  }

  return lines.join("\n");
}

function headingText(rawHeading: string): string {
  return rawHeading
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/<[^>]+>/g, "")
    .replace(/\\([\\`*{}\[\]()#+\-.!_>])/g, "$1");
}

function githubHeadingSlug(rawHeading: string): string {
  return headingText(rawHeading)
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

function addHeadingAnchor(
  rawHeading: string,
  anchors: Set<string>,
  slugCounts: Map<string, number>,
): void {
  const baseSlug = githubHeadingSlug(rawHeading);
  if (!baseSlug) return;

  let suffix = slugCounts.get(baseSlug) ?? 0;
  let slug = baseSlug;
  while (anchors.has(slug)) {
    suffix += 1;
    slug = `${baseSlug}-${suffix}`;
  }
  slugCounts.set(baseSlug, suffix);
  anchors.add(slug);
}

function markdownHeadingAt(lines: string[], index: number): string | null {
  const line = lines[index];
  const atxHeading = line.match(/^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$/);
  if (atxHeading) return atxHeading[1];

  const setextUnderline = lines[index + 1]?.match(/^ {0,3}(?:=+|-+)\s*$/);
  return line.trim() && setextUnderline ? line.trim() : null;
}

function markdownAnchors(markdown: string): Set<string> {
  const source = withoutFencedCode(markdown);
  const lines = source.split("\n");
  const anchors = new Set<string>();
  const slugCounts = new Map<string, number>();

  for (let index = 0; index < lines.length; index += 1) {
    const heading = markdownHeadingAt(lines, index);
    if (heading) addHeadingAnchor(heading, anchors, slugCounts);
  }

  for (const match of source.matchAll(/\b(?:id|name)\s*=\s*["']([^"']+)["']/gi)) {
    anchors.add(match[1]);
  }

  return anchors;
}

function decode(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function localDestinationParts(destination: string): { rawPath: string; rawFragment: string } {
  const hashIndex = destination.indexOf("#");
  const pathAndQuery = hashIndex >= 0 ? destination.slice(0, hashIndex) : destination;
  const rawFragment = hashIndex >= 0 ? destination.slice(hashIndex + 1).split("?", 1)[0] : "";
  const queryIndex = pathAndQuery.indexOf("?");
  const rawPath = queryIndex >= 0 ? pathAndQuery.slice(0, queryIndex) : pathAndQuery;
  return { rawPath, rawFragment };
}

async function checkLocalLink(
  link: MarkdownLink,
  markdown: string,
  sourceFilePath: string,
): Promise<BrokenLink | null> {
  if (!link.destination || isExternal(link.destination)) return null;

  const { rawPath, rawFragment } = localDestinationParts(link.destination);
  if (!rawPath && !rawFragment) return null;

  const localPath = decode(rawPath);
  const target = localPath ? path.resolve(path.dirname(sourceFilePath), localPath) : sourceFilePath;
  try {
    await stat(target);
  } catch {
    return { ...link, reason: "missing-file" };
  }

  if (!rawFragment) return null;
  const targetMarkdown = target === sourceFilePath ? markdown : await markdownContent(target);
  if (!targetMarkdown) return null;

  const fragment = decode(rawFragment);
  return markdownAnchors(targetMarkdown).has(fragment)
    ? null
    : { ...link, reason: "missing-anchor" };
}

async function markdownContent(target: string): Promise<string | null> {
  try {
    const targetStat = await stat(target);
    if (targetStat.isDirectory()) {
      for (const indexName of ["README.md", "readme.md", "index.md", "README.markdown"]) {
        try {
          return await readFile(path.join(target, indexName), "utf8");
        } catch {
          // Try the next conventional Markdown index file.
        }
      }
      return null;
    }

    if (!/\.(?:md|markdown)$/i.test(target)) return null;
    return await readFile(target, "utf8");
  } catch {
    return null;
  }
}

export async function findBrokenLocalLinks(
  markdown: string,
  baseDirectory: string,
  sourceFile = "README.md",
): Promise<BrokenLink[]> {
  const broken: BrokenLink[] = [];
  const sourceFilePath = path.resolve(baseDirectory, sourceFile);

  for (const link of markdownLinks(markdown)) {
    const failure = await checkLocalLink(link, markdown, sourceFilePath);
    if (failure) broken.push(failure);
  }

  return broken;
}

export async function findBrokenExternalLinks(
  markdown: string,
  fetcher: LinkFetcher = fetch,
): Promise<BrokenLink[]> {
  const uniqueLinks = new Map<string, MarkdownLink>();
  for (const link of markdownLinks(markdown)) {
    if (/^https?:\/\//i.test(link.destination) && !uniqueLinks.has(link.destination)) {
      uniqueLinks.set(link.destination, link);
    }
  }

  const results = await Promise.all([...uniqueLinks].map(async ([destination, link]) => {
    let response: Response | undefined;
    try {
      response = await fetcher(destination, {
        method: "HEAD",
        redirect: "follow",
        signal: AbortSignal.timeout(10_000),
      });
      if (response.status === 405 || response.status === 501) {
        await response.body?.cancel().catch(() => undefined);
        response = await fetcher(destination, {
          method: "GET",
          headers: { Range: "bytes=0-0" },
          redirect: "follow",
          signal: AbortSignal.timeout(10_000),
        });
      }
      if (!response.ok) {
        return { ...link, reason: `HTTP ${response.status} ${response.statusText}`.trim() };
      }
      return null;
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      return { ...link, reason };
    } finally {
      await response?.body?.cancel().catch(() => undefined);
    }
  }));

  return results.filter((result): result is BrokenLink => result !== null);
}

async function main() {
  const readmePath = fileURLToPath(new URL("../README.md", import.meta.url));
  const markdown = await readFile(readmePath, "utf8");
  const broken = await findBrokenLocalLinks(markdown, path.dirname(readmePath));
  const checkExternal = process.argv.includes("--external");
  if (checkExternal) broken.push(...await findBrokenExternalLinks(markdown));

  if (broken.length) {
    for (const link of broken) {
      const description = link.reason === "missing-anchor"
        ? "points to a missing Markdown section"
        : link.reason === "missing-file"
          ? "points to a missing local file"
          : `could not be verified (${link.reason})`;
      process.stderr.write(`README.md:${link.line}: [${link.text}] ${description}: ${link.destination}\n`);
    }
    process.exitCode = 1;
    return;
  }
  process.stdout.write(checkExternal
    ? "README.md local paths, anchors, and external links are valid.\n"
    : "README.md local paths and anchors are valid.\n");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await main();
}
