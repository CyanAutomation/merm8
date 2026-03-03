#!/usr/bin/env node
/**
 * parse.mjs - Mermaid parser subprocess for mermaid-lint
 *
 * Reads Mermaid diagram source from stdin, validates it using the official
 * Mermaid library, and writes a structured JSON result to stdout.
 *
 * Security constraints:
 *  - Stateless: no file system writes
 *  - Input via stdin only
 *  - Output via stdout only
 *  - Terminated by the Go parent process after timeout
 */

import { readFileSync } from 'fs';

// Set up a minimal DOM environment so that mermaid's DOMPurify dependency
// initialises correctly in Node.js (it requires a window/document object).
import { JSDOM } from 'jsdom';
const { window: _win } = new JSDOM('<!DOCTYPE html>');
global.window           = _win;
global.document         = _win.document;
global.Element          = _win.Element;
global.HTMLElement      = _win.HTMLElement;
global.DocumentFragment = _win.DocumentFragment;
global.NodeFilter       = _win.NodeFilter;
global.Node             = _win.Node;

// Read all stdin synchronously (Go sends the full payload before reading output)
const input = readFileSync('/dev/stdin', 'utf8').trim();

if (!input) {
  writeResult({ valid: false, error: { message: 'empty input', line: 0, column: 0 } });
  process.exit(0);
}

try {
  const mermaid = (await import('mermaid/dist/mermaid.core.mjs')).default;
  mermaid.initialize({ startOnLoad: false });

  const { parse, detectType, mermaidAPI } = mermaid;

  // detectType throws for completely unrecognised diagram types
  let diagramType;
  try {
    diagramType = detectType(input, { suppressErrors: false });
  } catch (typeErr) {
    const base = String(typeErr?.message || typeErr);
    const hint = 'Hint: start the diagram with a Mermaid type keyword like "flowchart", "graph", "sequenceDiagram", "classDiagram", "stateDiagram", or "erDiagram".';
    writeResult({
      valid: false,
      error: { message: `${base}. ${hint}`, line: 0, column: 0 },
    });
    process.exit(0);
  }

  // parse() resolves on success and rejects with a ParseError on failure
  try {
    await parse(input);
  } catch (parseErr) {
    const msg = parseErr?.message || String(parseErr);
    const line = parseErr?.hash?.loc?.first_line ?? 0;
    const col  = parseErr?.hash?.loc?.first_column ?? 0;
    writeResult({ valid: false, error: { message: msg, line, column: col } });
    process.exit(0);
  }

  // Extract the structural AST after successful parse
  let ast;
  try {
    const normalizedType = normalizeDiagramType(diagramType);
    ast = await extractAST(mermaidAPI, input, normalizedType);
  } catch (err) {
    writeResult({
      valid: false,
      error: { message: 'AST extraction failed in parser runtime: ' + String(err?.message || err), line: 0, column: 0 },
    });
    process.exit(0);
  }
  writeResult({ valid: true, diagram_type: normalizeDiagramType(diagramType), ast });
} catch (err) {
  writeResult({
    valid: false,
    error: { message: 'internal parser error: ' + String(err?.message || err), line: 0, column: 0 },
  });
  process.exit(1);
}

// ---------------------------------------------------------------------------
// AST extraction helpers
// ---------------------------------------------------------------------------

/**
 * Extracts a simplified AST by calling getDiagramFromText.
 * Falls back to deriving nodes from edges when vertices are unavailable.
 */
async function extractAST(mermaidAPI, source, diagramType) {
  const ast = {
    type: diagramType,
    direction: 'TD',
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
  } catch (_) {
    // getDiagramFromText can fail in parser runtime under Node.js.
  }

  if (!db) {
    if (diagramType !== 'flowchart') {
      return ast;
    }
    throw new Error('AST extraction failed in parser runtime');
  }

  if (diagramType !== 'flowchart') {
    return ast;
  }

  // Direction
  ast.direction = db.direction ?? 'TD';

  // Edges (reliably populated even without a DOM)
  const rawEdges = Array.isArray(db.edges) ? db.edges : [];
  for (const e of rawEdges) {
    const from = String(e.start ?? e.from ?? '');
    const to = String(e.end ?? e.to ?? '');
    const edgeLoc = findEdgeLocation(sourceLines, from, to);
    ast.edges.push({
      from,
      to,
      type: String(e.type  ?? 'arrow'),
      ...(edgeLoc || {}),
    });
  }

  // Vertices (may be empty when labels use DOMPurify; fall back to edge IDs)
  const rawVertices = db.vertices ?? {};
  const explicitNodes = Object.entries(rawVertices);
  if (explicitNodes.length > 0) {
    for (const [id, v] of explicitNodes) {
      const nodeLoc = findNodeLocation(sourceLines, id);
      ast.nodes.push({ id, label: extractLabel(v), ...(nodeLoc || {}) });
    }
  } else {
    // Derive unique node IDs from edge endpoints
    const seen = new Set();
    for (const e of ast.edges) {
      if (e.from && !seen.has(e.from)) {
        seen.add(e.from);
        const nodeLoc = findNodeLocation(sourceLines, e.from);
        ast.nodes.push({ id: e.from, label: '', ...(nodeLoc || {}) });
      }
      if (e.to && !seen.has(e.to)) {
        seen.add(e.to);
        const nodeLoc = findNodeLocation(sourceLines, e.to);
        ast.nodes.push({ id: e.to, label: '', ...(nodeLoc || {}) });
      }
    }
  }

  // Subgraphs
  const rawSubs = Array.isArray(db.subGraphs) ? db.subGraphs : [];
  for (const s of rawSubs) {
    ast.subgraphs.push({
      id:    String(s.id    ?? s.title ?? ''),
      label: String(s.title ?? s.id    ?? ''),
      nodes: Array.isArray(s.nodes) ? s.nodes.map(String) : [],
    });
  }

  return ast;
}




function findNodeLocation(lines, id) {
  const escaped = escapeRegExp(id);
  const patterns = [
    new RegExp(`(^|\\s)${escaped}(?=\\s*[\[({])`),
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

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const arrowIndex = line.search(/[-.=xo]+>/);
    if (arrowIndex < 0) {
      continue;
    }

    const fromIndex = line.indexOf(from);
    if (fromIndex < 0 || fromIndex > arrowIndex) {
      continue;
    }

    const toIndex = line.indexOf(to, arrowIndex);
    if (toIndex < 0) {
      continue;
    }

    return { line: i + 1, column: fromIndex + 1 };
  }

  return null;
}


function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function normalizeDiagramType(detectedType) {
  const raw = String(detectedType || '').toLowerCase();
  if (raw.startsWith('flowchart') || raw === 'graph') return 'flowchart';
  if (raw.startsWith('sequence')) return 'sequence';
  if (raw.startsWith('class')) return 'class';
  if (raw === 'er' || raw.startsWith('erd')) return 'er';
  if (raw.startsWith('state')) return 'state';
  return 'unknown';
}

function extractSuppressions(source) {
  const suppressions = [];
  const lines = source.split(/\r?\n/);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();

    const disableNextLineMatch = line.match(/^%%\s*merm8-(?:disable|ignore)-next-line\s+(all|[a-z0-9-]+)\s*$/i);
    if (disableNextLineMatch) {
      const rule = disableNextLineMatch[1].toLowerCase();
      suppressions.push({
        ruleId: rule,
        scope: 'next-line',
        line: i + 1,
        targetLine: i + 2,
      });
      continue;
    }

    const disableMatch = line.match(/^%%\s*merm8-(?:disable|ignore)\s+(all|[a-z0-9-]+)\s*$/i);
    if (disableMatch) {
      suppressions.push({
        ruleId: disableMatch[1].toLowerCase(),
        scope: 'file',
        line: i + 1,
        targetLine: i + 1,
      });
    }
  }

  return suppressions;
}

function extractLabel(vertex) {
  if (!vertex) return '';
  if (typeof vertex.text === 'string') return vertex.text;
  if (typeof vertex.label === 'string') return vertex.label;
  if (vertex.text && typeof vertex.text.label === 'string') return vertex.text.label;
  return '';
}

function writeResult(obj) {
  process.stdout.write(JSON.stringify(obj) + '\n');
}
