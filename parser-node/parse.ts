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

// Set up a minimal DOM environment so that mermaid's DOMPurify dependency
// initialises correctly in Node.js (it requires a window/document object).
import { JSDOM } from "jsdom";

type DiagramType = "flowchart" | "sequence" | "class" | "er" | "state" | "unknown";
type Mistake =
  | "graphviz"
  | "yaml-frontmatter"
  | "tabs"
  | "wrong-arrow-graphviz"
  | "wrong-arrow-single";
type SourceLocation = { line?: number; column?: number };
type NodeAST = SourceLocation & { id: string; label: string };
type EdgeAST = SourceLocation & {
  from: string;
  to: string;
  type: string;
  label?: string;
};
type SubgraphAST = { id: string; label: string; nodes: string[] };
type Suppression = {
  ruleId: string;
  scope: "file" | "next-line";
  line: number;
  targetLine: number;
};
type ParserAST = {
  type: DiagramType;
  direction: string;
  nodes: NodeAST[];
  edges: EdgeAST[];
  subgraphs: SubgraphAST[];
  suppressions: Suppression[];
  startStates?: string[];
};
type ParseResult =
  | { valid: true; diagram_type: DiagramType; ast: ParserAST }
  | {
      valid: false;
      error: { message: string; line: number; column: number };
    };
type RawRecord = Record<string, any>;
type MermaidAPI = {
  getDiagramFromText(source: string): Promise<{ db?: RawRecord | null }>;
};
type MermaidRuntime = {
  version?: string;
  initialize(config: { startOnLoad: boolean }): void;
  detectType(source: string, options: { suppressErrors: boolean }): string;
  parse(source: string): Promise<unknown>;
  mermaidAPI: MermaidAPI;
};
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
  const trimmedInput = String(input || "").trim();

  if (!trimmedInput) {
    return {
      valid: false,
      error: { message: "empty input", line: 0, column: 0 },
    };
  }

  try {
    const mermaid = await loadMermaid();
    const { parse, detectType, mermaidAPI } = mermaid;

    let diagramType: string;
    try {
      diagramType = detectType(input, { suppressErrors: false });
    } catch (typeErr) {
      const base = errorMessage(typeErr);
      const mistakes = detectCommonMistakes(input);
      const hint = buildErrorHint(base, mistakes);
      return {
        valid: false,
        error: { message: `${base}. ${hint}`, line: 0, column: 0 },
      };
    }

    try {
      await parse(input);
    } catch (parseErr) {
      const parseFailure = asRecord(parseErr);
      const message = parseFailure?.message;
      const location = asRecord(asRecord(parseFailure?.hash)?.loc);
      const msg = message ? String(message) : String(parseErr);
      const line = Number(location?.first_line ?? 0);
      const col = Number(location?.first_column ?? 0);
      const mistakes = detectCommonMistakes(input);
      const enhancedMsg = buildParseErrorMessage(msg, mistakes);
      return {
        valid: false,
        error: { message: enhancedMsg, line, column: col },
      };
    }

    let ast;
    try {
      const normalizedType = normalizeDiagramType(diagramType);
      ast = await extractAST(mermaidAPI, input, normalizedType);
    } catch (err) {
      return {
        valid: false,
        error: {
          message:
            "AST extraction failed in parser runtime: " +
            errorMessage(err),
          line: 0,
          column: 0,
        },
      };
    }

    return {
      valid: true,
      diagram_type: normalizeDiagramType(diagramType),
      ast,
    };
  } catch (err) {
    const errorMsg = errorMessage(err).toLowerCase();
    const isMemoryError =
      errorMsg.includes("heap") ||
      errorMsg.includes("memory") ||
      errorMsg.includes("out of memory") ||
      errorMsg.includes("javascript heap out of memory");

    if (isMemoryError) {
      return {
        valid: false,
        error: {
          message:
            "parser_memory_limit: Parser memory exhausted; diagram too large for configured limit",
          line: 0,
          column: 0,
        },
      };
    }

    return {
      valid: false,
      error: {
        message: "internal parser error: " + errorMessage(err),
        line: 0,
        column: 0,
      },
    };
  }
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

async function extractAST(
  mermaidAPI: MermaidAPI,
  source: string,
  diagramType: DiagramType,
): Promise<ParserAST> {
  const ast = {
    type: diagramType,
    direction: "TD",
    nodes: [] as NodeAST[],
    edges: [] as EdgeAST[],
    subgraphs: [] as SubgraphAST[],
    suppressions: extractSuppressions(source),
  };

  const sourceLines = source.split(/\r?\n/);

  let db: RawRecord | null = null;
  try {
    const diagram = await mermaidAPI.getDiagramFromText(source);
    db = diagram?.db ?? null;
  } catch (_) {}

  if (!db) {
    if (diagramType !== "flowchart") {
      return ast;
    }
    throw new Error("AST extraction failed in parser runtime");
  }

  if (diagramType === "sequence") {
    return extractSequenceAST(ast, db, sourceLines);
  }

  if (diagramType === "class") {
    return extractClassAST(ast, db, sourceLines);
  }

  if (diagramType === "er") {
    return extractERAST(ast, db, sourceLines);
  }

  if (diagramType === "state") {
    return extractStateAST(ast, db, sourceLines);
  }

  if (diagramType !== "flowchart") {
    return ast;
  }

  ast.direction = normalizeFlowchartDirection(db.direction);

  const rawEdges = Array.isArray(db.edges) ? db.edges : [];
  for (const e of rawEdges) {
    const fromOriginal = String(e.start ?? e.from ?? "");
    const toOriginal = String(e.end ?? e.to ?? "");
    const from = normalizeNodeID(fromOriginal);
    const to = normalizeNodeID(toOriginal);
    const edgeLoc = findEdgeLocation(sourceLines, fromOriginal, toOriginal);
    ast.edges.push({
      from,
      to,
      type: String(e.type ?? "arrow"),
      ...(edgeLoc || {}),
    });
  }

  const rawVertices = db.vertices ?? {};
  const explicitNodes = Object.entries(rawVertices);
  
  // Build a set of all node IDs referenced in edges
  const nodeIDsInEdges = new Set<string>();
  for (const e of ast.edges) {
    if (e.from) nodeIDsInEdges.add(e.from);
    if (e.to) nodeIDsInEdges.add(e.to);
  }
  
  if (explicitNodes.length > 0) {
    const seen = new Set<string>();
    for (const [id, v] of explicitNodes) {
      const normalizedID = normalizeNodeID(id);
      
      // Only include nodes that are referenced in edges (filters out phantom nodes from labels)
      if (!nodeIDsInEdges.has(normalizedID)) {
        continue;
      }
      
      if (seen.has(normalizedID)) {
        continue;
      }
      seen.add(normalizedID);
      
      const nodeLoc = findNodeLocation(sourceLines, id);
      ast.nodes.push({
        id: normalizedID,
        label: extractLabel(v),
        ...(nodeLoc || {}),
      });
    }
    
    // Add any nodes from edges that weren't in rawVertices
    for (const nodeID of nodeIDsInEdges) {
      if (!seen.has(nodeID)) {
        seen.add(nodeID);
        const nodeLoc = findNodeLocation(sourceLines, nodeID);
        ast.nodes.push({ id: nodeID, label: "", ...(nodeLoc || {}) });
      }
    }
  } else {
    const seen = new Set<string>();
    for (const e of ast.edges) {
      if (e.from && !seen.has(e.from)) {
        seen.add(e.from);
        const nodeLoc = findNodeLocation(sourceLines, e.from);
        ast.nodes.push({ id: e.from, label: "", ...(nodeLoc || {}) });
      }
      if (e.to && !seen.has(e.to)) {
        seen.add(e.to);
        const nodeLoc = findNodeLocation(sourceLines, e.to);
        ast.nodes.push({ id: e.to, label: "", ...(nodeLoc || {}) });
      }
    }
  }

  const rawSubs = Array.isArray(db.subGraphs) ? db.subGraphs : [];
  for (const s of rawSubs) {
    ast.subgraphs.push({
      id: String(s.id ?? s.title ?? ""),
      label: String(s.title ?? s.id ?? ""),
      nodes: Array.isArray(s.nodes)
        ? s.nodes.map((n: unknown) => normalizeNodeID(n))
        : [],
    });
  }

  return ast;
}

function findNodeLocation(lines: string[], id: string): SourceLocation | null {
  const escaped = escapeRegExp(id);
  const patterns = [
    new RegExp(`(^|\\s)${escaped}(?=\\s*[\\[({])`),
    new RegExp(`(^|\\s)${escaped}(?=\\s*[-.=xo]+>)`),
    new RegExp(`(^|\\s)${escaped}(?=\\s*$)`),
  ];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    for (const pattern of patterns) {
      const m = line.match(pattern);
      if (m) {
        const start = (m.index ?? 0) + m[1].length;
        return { line: i + 1, column: start + 1 };
      }
    }
  }

  return null;
}

function findEdgeLocation(
  lines: string[],
  from: string,
  to: string,
): SourceLocation | null {
  if (!from || !to) return null;

  const escapedFrom = escapeRegExp(from);
  const escapedTo = escapeRegExp(to);
  const fromPattern = new RegExp(
    `(^|\\s)${escapedFrom}(?=\\s*(?:[\\[({]|[-.=xo]+>))`,
  );
  const toPattern = new RegExp(`(^|\\s)${escapedTo}(?=\\s*(?:[\\[({]|$))`);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const arrowIndex = line.search(/[-.=xo]+>/);
    if (arrowIndex < 0) {
      continue;
    }

    const fromMatch = line.match(fromPattern);
    if (!fromMatch) {
      continue;
    }

    const fromIndex = (fromMatch.index ?? 0) + fromMatch[1].length;
    if (fromIndex > arrowIndex) {
      continue;
    }

    const afterArrow = line.slice(arrowIndex);
    const toMatch = afterArrow.match(toPattern);
    if (!toMatch) {
      continue;
    }

    return { line: i + 1, column: fromIndex + 1 };
  }

  return null;
}

function escapeRegExp(value: string): string {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeFlowchartDirection(dir: unknown): string {
  const normalized = String(dir || "")
    .trim()
    .toUpperCase();
  if (normalized === "TB") return "TD";
  if (
    normalized === "TD" ||
    normalized === "LR" ||
    normalized === "RL" ||
    normalized === "BT"
  ) {
    return normalized;
  }
  return "TD";
}

function normalizeNodeID(id: unknown): string {
  // Preserve Mermaid's canonical node identity (case-sensitive), while trimming
  // incidental surrounding whitespace from parser/runtime values.
  return String(id).trim();
}

function extractSequenceAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const state = db?.state?.records;
  if (!state) return ast;

  // Extract explicitly defined participants from source (preserve duplicates for detection)
  const participantDefinitions: (SourceLocation & { id: string })[] = [];
  for (let i = 0; i < sourceLines.length; i++) {
    const trimmed = sourceLines[i].trim();
    // Match "participant X" or "participant X as Y" - capture full name (may include spaces)
    const participantMatch = trimmed.match(/^participant\s+(.+?)(?:\s+as\s+.+)?$/i);
    if (participantMatch) {
      participantDefinitions.push({
        id: participantMatch[1].trim(),
        line: i + 1,
        column: trimmed.indexOf(participantMatch[1]) + 1,
      });
    }
  }

  // Extract messages as edges
  const messages = Array.isArray(state.messages) ? state.messages : [];
  const allActors = new Set<string>();

  for (const msg of messages) {
    const from = String(msg.from || "").trim();
    const to = String(msg.to || "").trim();
    if (from) allActors.add(from);
    if (to) allActors.add(to);

    ast.edges.push({
      from,
      to,
      type: msg.type === 1 ? "dotted" : "solid",
      label: String(msg.message || ""),
    });
  }

  // Add all participant definitions as nodes (including duplicates for detection)
  for (const def of participantDefinitions) {
    ast.nodes.push({
      id: def.id,
      label: "",
      line: def.line,
      column: def.column,
    });
  }

  return ast;
}

function extractClassAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const relations = Array.isArray(db.relations) ? db.relations : [];
  const classes = db.classes || {};

  // Extract class definitions from source (preserve duplicates for detection)
  const classDefinitions: (SourceLocation & { id: string })[] = [];
  for (let i = 0; i < sourceLines.length; i++) {
    const trimmed = sourceLines[i].trim();
    // Match "class ClassName" or "class ClassName {"
    const classMatch = trimmed.match(/^class\s+(\w+)/i);
    if (classMatch) {
      classDefinitions.push({
        id: classMatch[1].trim(),
        line: i + 1,
        column: trimmed.indexOf(classMatch[1]) + 1,
      });
    }
  }

  // Extract relations as edges
  for (const rel of relations) {
    const id1 = String(rel.id1 || "").trim();
    const id2 = String(rel.id2 || "").trim();

    // Map relation types
    let relType = "dependency";
    const type1 = rel.relation?.type1;
    if (type1 === 0) relType = "aggregation";
    else if (type1 === 1) relType = "extension";
    else if (type1 === 2) relType = "composition";
    else if (type1 === 3) relType = "dependency";
    else if (type1 === 4) relType = "lollipop";

    // For inheritance (extension), the edge direction is from child to parent
    // In Mermaid: "Base <|-- Derived" means Derived extends Base
    // So edge should be from Derived (id2) to Base (id1)
    if (relType === "extension") {
      ast.edges.push({
        from: id2,
        to: id1,
        type: relType,
      });
    } else {
      ast.edges.push({
        from: id1,
        to: id2,
        type: relType,
      });
    }
  }

  // Add all class definitions as nodes (including duplicates for detection)
  for (const def of classDefinitions) {
    ast.nodes.push({
      id: def.id,
      label: "",
      line: def.line,
      column: def.column,
    });
  }

  return ast;
}

function extractERAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const relationships = Array.isArray(db.relationships) ? db.relationships : [];

  // Extract entity names from relationships
  const entityNames = new Set<string>();
  for (const rel of relationships) {
    // Entity names are prefixed with "entity-" and suffixed with "-N"
    const entityA = String(rel.entityA || "").replace(/^entity-/, "").replace(/-\d+$/, "");
    const entityB = String(rel.entityB || "").replace(/^entity-/, "").replace(/-\d+$/, "");
    
    if (entityA) entityNames.add(entityA);
    if (entityB) entityNames.add(entityB);

    // Map relationship types
    let relType = "identifying";
    if (rel.relSpec?.relType === "NON_IDENTIFYING") {
      relType = "non-identifying";
    }

    ast.edges.push({
      from: entityA,
      to: entityB,
      type: relType,
      label: String(rel.roleA || rel.roleB || ""),
    });
  }

  // Add entities as nodes
  for (const entityName of entityNames) {
    const loc = findNodeLocation(sourceLines, entityName);
    ast.nodes.push({
      id: entityName,
      label: "",
      ...(loc || {}),
    });
  }

  return ast;
}

function extractStateAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const nodes = Array.isArray(db.nodes) ? db.nodes : [];
  const edges = Array.isArray(db.edges) ? db.edges : [];

  // Track start state transitions for reachability
  const startTransitions: string[] = [];

  // Extract states as nodes (skip start/end markers)
  for (const node of nodes) {
    const id = String(node.id || "").trim();
    if (id === "root_start" || id === "root_end") continue;
    
    const loc = findNodeLocation(sourceLines, id);
    ast.nodes.push({
      id,
      label: String(node.label || ""),
      ...(loc || {}),
    });
  }

  // Extract transitions as edges
  for (const edge of edges) {
    const from = String(edge.start || "").trim();
    const to = String(edge.end || "").trim();
    
    // Track start state transitions
    if (from === "root_start" && to !== "root_end") {
      startTransitions.push(to);
      continue;
    }
    
    // Skip edges from/to end markers
    if (from === "root_end" || to === "root_start" || to === "root_end") continue;

    ast.edges.push({
      from,
      to,
      type: "transition",
      label: String(edge.label || ""),
    });
  }

  // Store start state info in a special field for reachability analysis
  if (startTransitions.length > 0) {
    ast.startStates = startTransitions;
  }

  return ast;
}

function normalizeDiagramType(detectedType: unknown): DiagramType {
  const raw = String(detectedType || "").toLowerCase();
  if (raw.startsWith("flowchart") || raw === "graph") return "flowchart";
  if (raw.startsWith("sequence")) return "sequence";
  if (raw.startsWith("class")) return "class";
  if (raw === "er" || raw.startsWith("erd")) return "er";
  if (raw.startsWith("state")) return "state";
  return "unknown";
}

function extractSuppressions(source: string): Suppression[] {
  const suppressions: Suppression[] = [];
  const lines = source.split(/\r?\n/);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();

    const disableNextLineMatch = line.match(
      /^%%\s*merm8-(?:disable|ignore)-next-line\s+(all|[a-z0-9-]+)\s*$/i,
    );
    if (disableNextLineMatch) {
      const rule = disableNextLineMatch[1].toLowerCase();
      suppressions.push({
        ruleId: rule,
        scope: "next-line",
        line: i + 1,
        targetLine: i + 2,
      });
      continue;
    }

    const disableMatch = line.match(
      /^%%\s*merm8-(?:disable|ignore)\s+(all|[a-z0-9-]+)\s*$/i,
    );
    if (disableMatch) {
      suppressions.push({
        ruleId: disableMatch[1].toLowerCase(),
        scope: "file",
        line: i + 1,
        targetLine: i + 1,
      });
    }
  }

  return suppressions;
}

function extractLabel(vertex: unknown): string {
  if (!vertex) return "";
  const record = asRecord(vertex);
  if (!record) return "";
  if (typeof record.text === "string") return record.text;
  if (typeof record.label === "string") return record.label;
  const text = asRecord(record.text);
  if (typeof text?.label === "string") return text.label;
  return "";
}

function writeResult(obj: unknown): void {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await main();
}
