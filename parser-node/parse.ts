#!/usr/bin/env node
/**
 * parse.ts - Mermaid parser subprocess for mermaid-lint
 *
 * Reads Mermaid diagram source from stdin and writes structured JSON result(s)
 * to stdout. Supports one-shot mode and long-lived worker mode.
 */

import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import readline from "readline";
import parserPkg from "./package.json" with { type: "json" };
import { extractAST } from "./ast-extract.ts";
import type { DiagramType, MermaidRuntime, ParseResult, ParserAST, RawRecord } from "./ast-types.ts";

// Set up a minimal DOM environment so that mermaid's DOMPurify dependency
// initialises correctly in Node.js (it requires a window/document object).
import { JSDOM } from "jsdom";

type Mistake =
  | "graphviz"
  | "yaml-frontmatter"
  | "tabs"
  | "wrong-arrow-graphviz"
  | "wrong-arrow-single";
type TimerHandle = ReturnType<typeof setTimeout> | number;
type WorkerTimer = {
  setTimeout(callback: () => void, timeoutMs: number): TimerHandle;
  clearTimeout(handle: TimerHandle): void;
};
type WorkerTimeoutError = Error & { code: "WORKER_TIMEOUT" };

function asRecord(value: unknown): RawRecord | null {
  return typeof value === "object" && value !== null
    ? (value as RawRecord)
    : null;
}

function errorMessage(error: unknown): string {
  const message = asRecord(error)?.message;
  return message ? String(message) : String(error);
}

function errorCode(error: unknown): string {
  return String(asRecord(error)?.code || "");
}

const { window: _win } = new JSDOM("<!DOCTYPE html>");
const runtimeGlobals = globalThis as unknown as Record<string, unknown>;
runtimeGlobals.window = _win;
runtimeGlobals.document = _win.document;
runtimeGlobals.Element = _win.Element;
runtimeGlobals.HTMLElement = _win.HTMLElement;
runtimeGlobals.DocumentFragment = _win.DocumentFragment;
runtimeGlobals.NodeFilter = _win.NodeFilter;
runtimeGlobals.Node = _win.Node;

const versionInfoMode = process.argv.includes("--version-info");
const workerMode = process.argv.includes("--worker");

let mermaidRuntime: MermaidRuntime | null = null;

async function loadMermaid(): Promise<MermaidRuntime> {
  if (!mermaidRuntime) {
    mermaidRuntime = (await import("mermaid/dist/mermaid.core.mjs")).default as MermaidRuntime;
    mermaidRuntime.initialize({ startOnLoad: false });
  }
  return mermaidRuntime;
}

async function main(): Promise<void> {
  if (versionInfoMode) {
    try {
      const mermaid = await loadMermaid();
      const mermaidRuntimeVersion = String(mermaid?.version || "").trim();
      const mermaidDependencyVersion = String(
        parserPkg?.dependencies?.mermaid || "",
      ).trim();
      writeResult({
        parser_version: String(parserPkg?.version || "").trim(),
        mermaid_version: mermaidRuntimeVersion || mermaidDependencyVersion,
      });
      process.exit(0);
    } catch (err) {
      const mermaidDependencyVersion = String(
        parserPkg?.dependencies?.mermaid || "",
      ).trim();
      writeResult({
        parser_version: String(parserPkg?.version || "").trim(),
        mermaid_version: mermaidDependencyVersion,
        error: "internal parser error: " + errorMessage(err),
      });
      process.exit(1);
    }
  }

  if (workerMode) {
    await runWorkerMode();
    process.exit(0);
  }

  const input = readFileSync("/dev/stdin", "utf8");
  const singleResult = await parseSource(input);
  writeResult(singleResult);
  if (
    !singleResult.valid &&
    singleResult.error &&
    (singleResult.error.message.startsWith("internal parser error:") ||
      singleResult.error.message.startsWith("parser_memory_limit:"))
  ) {
    process.exit(1);
  }
}

async function runWorkerMode(): Promise<void> {
  await loadMermaid();

  const rl = readline.createInterface({
    input: process.stdin,
    crlfDelay: Infinity,
    terminal: false,
  });

  for await (const line of rl) {
    const trimmed = String(line || "").trim();
    if (!trimmed) {
      continue;
    }

    let envelope: RawRecord;
    try {
      envelope = JSON.parse(trimmed) as RawRecord;
    } catch (err) {
      writeResult({
        id: "",
        error: "invalid worker request: " + errorMessage(err),
      });
      continue;
    }

    const id = String(envelope?.id || "").trim();
    if (!id) {
      writeResult({
        id: "",
        error: "invalid worker request: missing id",
      });
      continue;
    }

    const timeoutMs = normalizeWorkerTimeoutMs(envelope?.timeout_ms);

    try {
      const result = await withWorkerTimeout(
        parseSource(String(envelope?.code || "")),
        timeoutMs,
      );
      writeResult({ id, result });
    } catch (err) {
      if (isWorkerTimeoutError(err)) {
        writeResult({
          id,
          error: "parser_timeout: exceeded " + String(timeoutMs) + "ms",
        });
        continue;
      }
      writeResult({
        id,
        error: "internal parser error: " + errorMessage(err),
      });
    }
  }
}

function normalizeWorkerTimeoutMs(rawTimeoutMs: unknown): number {
  const parsed = Number.parseInt(String(rawTimeoutMs ?? ""), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }
  return Math.min(parsed, 24 * 60 * 60 * 1000);
}

export function withWorkerTimeout<T>(
  promise: Promise<T>,
  timeoutMs: number,
  timer: WorkerTimer = globalThis,
): Promise<T> {
  if (!timeoutMs || timeoutMs <= 0) {
    return promise;
  }

  return new Promise((resolve, reject) => {
    const timerId = timer.setTimeout(() => {
      const err = new Error("worker parse timeout") as WorkerTimeoutError;
      err.code = "WORKER_TIMEOUT";
      reject(err);
    }, timeoutMs);

    if (typeof timerId === "object") timerId.unref?.();

    promise.then(
      (value) => {
        timer.clearTimeout(timerId);
        resolve(value);
      },
      (err) => {
        timer.clearTimeout(timerId);
        reject(err);
      },
    );
  });
}

function isWorkerTimeoutError(err: unknown): err is WorkerTimeoutError {
  return errorCode(err) === "WORKER_TIMEOUT";
}

async function parseSource(input: string): Promise<ParseResult> {
  if (!String(input || "").trim()) return parseError("empty input");
  try {
    const mermaid = await loadMermaid();
    return await parseWithRuntime(mermaid, input);
  } catch (err) {
    return runtimeFailure(err);
  }
}

function parseError(message: string, line = 0, column = 0): ParseResult {
  return { valid: false, error: { message, line, column } };
}

async function parseWithRuntime(mermaid: MermaidRuntime, input: string): Promise<ParseResult> {
  const detected = detectDiagramType(mermaid, input);
  if (detected.error) return detected.error;
  const syntaxError = await validateSyntax(mermaid, input);
  if (syntaxError) return syntaxError;

  const diagramType = normalizeDiagramType(detected.diagramType);
  try {
    const ast = await extractAST(mermaid.mermaidAPI, input, diagramType);
    return { valid: true, diagram_type: diagramType, ast };
  } catch (err) {
    return parseError("AST extraction failed in parser runtime: " + errorMessage(err));
  }
}

function detectDiagramType(
  mermaid: MermaidRuntime,
  input: string,
): { diagramType?: string; error?: ParseResult } {
  try {
    return { diagramType: mermaid.detectType(input, { suppressErrors: false }) };
  } catch (err) {
    const base = errorMessage(err);
    const hint = buildErrorHint(base, detectCommonMistakes(input));
    return { error: parseError(`${base}. ${hint}`) };
  }
}

async function validateSyntax(mermaid: MermaidRuntime, input: string): Promise<ParseResult | null> {
  try {
    await mermaid.parse(input);
    return null;
  } catch (err) {
    const failure = asRecord(err);
    const location = asRecord(asRecord(failure?.hash)?.loc);
    const message = failure?.message ? String(failure.message) : String(err);
    const line = Number(location?.first_line ?? 0);
    const column = Number(location?.first_column ?? 0);
    const enhancedMessage = buildParseErrorMessage(message, detectCommonMistakes(input));
    return parseError(enhancedMessage, line, column);
  }
}

function runtimeFailure(err: unknown): ParseResult {
  const message = errorMessage(err);
  const lowerMessage = message.toLowerCase();
  if (lowerMessage.includes("heap") || lowerMessage.includes("memory") || lowerMessage.includes("out of memory")) {
    return parseError("parser_memory_limit: Parser memory exhausted; diagram too large for configured limit");
  }
  return parseError("internal parser error: " + message);
}

// ---------------------------------------------------------------------------
// Error detection and hint generation
// ---------------------------------------------------------------------------

function detectCommonMistakes(input: string): Mistake[] {
  const firstLine = input.split("\n")[0].trim();
  const mistakes: Mistake[] = [];

  // Detect Graphviz syntax
  if (
    firstLine.startsWith("digraph") ||
    firstLine.startsWith("rankdir") ||
    firstLine === "{"
  ) {
    mistakes.push("graphviz");
  }

  // Detect YAML frontmatter
  if (firstLine.startsWith("---")) {
    mistakes.push("yaml-frontmatter");
  }

  // Detect tabs instead of spaces
  if (input.includes("\t")) {
    mistakes.push("tabs");
  }

  // Detect wrong arrow styles
  const graphvizArrow = input.includes(" -> ") && !input.includes("-->");
  const singleArrow =
    input.includes("->") && !input.includes("-->") && !input.includes(" -> ");
  if (graphvizArrow) {
    mistakes.push("wrong-arrow-graphviz");
  } else if (singleArrow && input.toLowerCase().includes("flowchart")) {
    mistakes.push("wrong-arrow-single");
  }

  return mistakes;
}

function buildErrorHint(baseMessage: string, mistakes: Mistake[]): string {
  const hints = [];

  if (mistakes.includes("graphviz")) {
    hints.push(
      'This looks like Graphviz syntax. Mermaid uses "flowchart TD" or "graph TD", not "digraph".',
    );
  }
  if (mistakes.includes("yaml-frontmatter")) {
    hints.push(
      'Remove the "---" YAML frontmatter line; Mermaid code should start directly with the diagram type.',
    );
  }
  if (mistakes.includes("tabs")) {
    hints.push("Replace tabs with spaces (2-4 spaces per indentation level).");
  }
  if (mistakes.includes("wrong-arrow-graphviz")) {
    hints.push('Use "-->" for connections in Mermaid flowcharts, not "->".');
  }
  if (mistakes.includes("wrong-arrow-single")) {
    hints.push('Use "-->" for flowchart connections, not "->".');
  }

  if (hints.length > 0) {
    return hints.join(" ");
  }

  return 'Hint: start the diagram with a Mermaid type keyword like "flowchart", "graph", "sequenceDiagram", "classDiagram", "stateDiagram", or "erDiagram".';
}

function buildParseErrorMessage(originalMsg: string, mistakes: Mistake[]): string {
  const hints = [];

  if (mistakes.includes("tabs")) {
    hints.push("[Hint: Replace tabs with spaces]");
  }
  if (
    mistakes.includes("wrong-arrow-graphviz") ||
    mistakes.includes("wrong-arrow-single")
  ) {
    hints.push('[Hint: Use "-->" for connections, not "->"]');
  }
  if (mistakes.includes("graphviz")) {
    hints.push(
      "[Hint: This looks like Graphviz syntax; use Mermaid syntax instead]",
    );
  }

  if (hints.length > 0) {
    return originalMsg + " " + hints.join(" ");
  }

  return originalMsg;
}

// ---------------------------------------------------------------------------
// AST extraction helpers
// ---------------------------------------------------------------------------

function normalizeDiagramType(detectedType: unknown): DiagramType {
  const raw = String(detectedType || "").toLowerCase();
  if (raw.startsWith("flowchart") || raw === "graph") return "flowchart";
  if (raw.startsWith("sequence")) return "sequence";
  if (raw.startsWith("class")) return "class";
  if (raw === "er" || raw.startsWith("erd")) return "er";
  if (raw.startsWith("state")) return "state";
  return "unknown";
}

function writeResult(obj: unknown): void {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await main();
}
