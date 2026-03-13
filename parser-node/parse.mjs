#!/usr/bin/env node
/**
 * parse.mjs - Mermaid parser subprocess for mermaid-lint
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

const { window: _win } = new JSDOM("<!DOCTYPE html>");
global.window = _win;
global.document = _win.document;
global.Element = _win.Element;
global.HTMLElement = _win.HTMLElement;
global.DocumentFragment = _win.DocumentFragment;
global.NodeFilter = _win.NodeFilter;
global.Node = _win.Node;

const versionInfoMode = process.argv.includes("--version-info");
const workerMode = process.argv.includes("--worker");

let mermaidRuntime = null;

async function loadMermaid() {
  if (!mermaidRuntime) {
    mermaidRuntime = (await import("mermaid/dist/mermaid.core.mjs")).default;
    mermaidRuntime.initialize({ startOnLoad: false });
  }
  return mermaidRuntime;
}

async function main() {
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
        error: "internal parser error: " + String(err?.message || err),
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
    String(singleResult?.error?.message || "").startsWith(
      "internal parser error:",
    ) ||
    String(singleResult?.error?.message || "").startsWith(
      "parser_memory_limit:",
    )
  ) {
    process.exit(1);
  }
}

async function runWorkerMode() {
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

    let envelope;
    try {
      envelope = JSON.parse(trimmed);
    } catch (err) {
      writeResult({
        id: "",
        error: "invalid worker request: " + String(err?.message || err),
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
        error: "internal parser error: " + String(err?.message || err),
      });
    }
  }
}

function normalizeWorkerTimeoutMs(rawTimeoutMs) {
  const parsed = Number.parseInt(String(rawTimeoutMs ?? ""), 10);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }
  return Math.min(parsed, 24 * 60 * 60 * 1000);
}

export function withWorkerTimeout(promise, timeoutMs, timer = globalThis) {
  if (!timeoutMs || timeoutMs <= 0) {
    return promise;
  }

  return new Promise((resolve, reject) => {
    const timerId = timer.setTimeout(() => {
      const err = new Error("worker parse timeout");
      err.code = "WORKER_TIMEOUT";
      reject(err);
    }, timeoutMs);

    timerId.unref?.();

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

function isWorkerTimeoutError(err) {
  return String(err?.code || "") === "WORKER_TIMEOUT";
}

async function parseSource(input) {
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

    let diagramType;
    try {
      diagramType = detectType(input, { suppressErrors: false });
    } catch (typeErr) {
      const base = String(typeErr?.message || typeErr);
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
      const msg = parseErr?.message || String(parseErr);
      const line = parseErr?.hash?.loc?.first_line ?? 0;
      const col = parseErr?.hash?.loc?.first_column ?? 0;
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
            String(err?.message || err),
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
    const errorMsg = String(err?.message || err).toLowerCase();
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
        message: "internal parser error: " + String(err?.message || err),
        line: 0,
        column: 0,
      },
    };
  }
}

// ---------------------------------------------------------------------------
// Error detection and hint generation
// ---------------------------------------------------------------------------

function detectCommonMistakes(input) {
  const firstLine = input.split("\n")[0].trim();
  const mistakes = [];

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

function buildErrorHint(baseMessage, mistakes) {
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

function buildParseErrorMessage(originalMsg, mistakes) {
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

async function extractAST(mermaidAPI, source, diagramType) {
  const ast = {
    type: diagramType,
    direction: "TD",
    nodes: [],
    edges: [],
    subgraphs: [],
    suppressions: extractSuppressions(source),
  };

  const sourceLines = source.split(/\r?\n/);

  let db = null;
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
  const nodeIDsInEdges = new Set();
  for (const e of ast.edges) {
    if (e.from) nodeIDsInEdges.add(e.from);
    if (e.to) nodeIDsInEdges.add(e.to);
  }
  
  if (explicitNodes.length > 0) {
    const seen = new Set();
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
    const seen = new Set();
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
        ? s.nodes.map((n) => normalizeNodeID(n))
        : [],
    });
  }

  return ast;
}

function findNodeLocation(lines, id) {
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
        const start = m.index + m[1].length;
        return { line: i + 1, column: start + 1 };
      }
    }
  }

  return null;
}

function findEdgeLocation(lines, from, to) {
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

    const fromIndex = fromMatch.index + fromMatch[1].length;
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

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeFlowchartDirection(dir) {
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

function normalizeNodeID(id) {
  // Preserve Mermaid's canonical node identity (case-sensitive), while trimming
  // incidental surrounding whitespace from parser/runtime values.
  return String(id).trim();
}

function extractSequenceAST(ast, db, sourceLines) {
  const state = db?.state?.records;
  if (!state) return ast;

  // Extract explicitly defined participants from source (preserve duplicates for detection)
  const participantDefinitions = [];
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
  const allActors = new Set();

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

function extractClassAST(ast, db, sourceLines) {
  const relations = Array.isArray(db.relations) ? db.relations : [];
  const classes = db.classes || {};

  // Extract class definitions from source (preserve duplicates for detection)
  const classDefinitions = [];
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

function normalizeDiagramType(detectedType) {
  const raw = String(detectedType || "").toLowerCase();
  if (raw.startsWith("flowchart") || raw === "graph") return "flowchart";
  if (raw.startsWith("sequence")) return "sequence";
  if (raw.startsWith("class")) return "class";
  if (raw === "er" || raw.startsWith("erd")) return "er";
  if (raw.startsWith("state")) return "state";
  return "unknown";
}

function extractSuppressions(source) {
  const suppressions = [];
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

function extractLabel(vertex) {
  if (!vertex) return "";
  if (typeof vertex.text === "string") return vertex.text;
  if (typeof vertex.label === "string") return vertex.label;
  if (vertex.text && typeof vertex.text.label === "string")
    return vertex.text.label;
  return "";
}

function writeResult(obj) {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  await main();
}
