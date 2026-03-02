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
  try {
    detectType(input, { suppressErrors: false });
  } catch (typeErr) {
    writeResult({
      valid: false,
      error: { message: String(typeErr.message || typeErr), line: 0, column: 0 },
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
  const ast = await extractAST(mermaidAPI, input);
  writeResult({ valid: true, ast });
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
 * Falls back to deriving nodes from edges if vertex extraction fails
 * (e.g. due to missing DOM/DOMPurify in Node.js environment).
 */
async function extractAST(mermaidAPI, source) {
  const ast = {
    direction: 'TD',
    nodes: [],
    edges: [],
    subgraphs: [],
  };

  let db = null;
  try {
    const diagram = await mermaidAPI.getDiagramFromText(source);
    db = diagram?.db ?? null;
  } catch (_) {
    // getDiagramFromText can fail when node labels trigger DOMPurify in a
    // non-browser environment. We still return a best-effort AST derived
    // from the edge list extracted before the failure.
  }

  if (db) {
    // Direction
    ast.direction = db.direction ?? 'TD';

    // Edges (reliably populated even without a DOM)
    const rawEdges = Array.isArray(db.edges) ? db.edges : [];
    for (const e of rawEdges) {
      ast.edges.push({
        from: String(e.start ?? e.from ?? ''),
        to:   String(e.end   ?? e.to   ?? ''),
        type: String(e.type  ?? 'arrow'),
      });
    }

    // Vertices (may be empty when labels use DOMPurify; fall back to edge IDs)
    const rawVertices = db.vertices ?? {};
    const explicitNodes = Object.entries(rawVertices);
    if (explicitNodes.length > 0) {
      for (const [id, v] of explicitNodes) {
        ast.nodes.push({ id, label: extractLabel(v) });
      }
    } else {
      // Derive unique node IDs from edge endpoints
      const seen = new Set();
      for (const e of ast.edges) {
        if (e.from && !seen.has(e.from)) { seen.add(e.from); ast.nodes.push({ id: e.from, label: '' }); }
        if (e.to   && !seen.has(e.to))   { seen.add(e.to);   ast.nodes.push({ id: e.to,   label: '' }); }
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
  }

  return ast;
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
