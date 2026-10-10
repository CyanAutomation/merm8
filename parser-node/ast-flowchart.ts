import { extractLabel, findEdgeLocation, findNodeLocation, normalizeFlowchartDirection, normalizeNodeID } from "./ast-common.ts";
import type { ParserAST, RawRecord } from "./ast-types.ts";

export function extractFlowchartAST(ast: ParserAST, db: RawRecord, sourceLines: string[]): ParserAST {
  ast.direction = normalizeFlowchartDirection(db.direction);
  addFlowchartEdges(ast, db, sourceLines);
  addFlowchartNodes(ast, db.vertices ?? {}, sourceLines);
  addFlowchartSubgraphs(ast, db.subGraphs);
  return ast;
}

function addFlowchartEdges(ast: ParserAST, db: RawRecord, sourceLines: string[]): void {
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
}

function addFlowchartNodes(ast: ParserAST, rawVertices: RawRecord, sourceLines: string[]): void {
  const edgeNodeIds = new Set<string>();
  for (const edge of ast.edges) {
    if (edge.from) edgeNodeIds.add(edge.from);
    if (edge.to) edgeNodeIds.add(edge.to);
  }
  const explicitNodes = Object.entries(rawVertices);
  if (explicitNodes.length > 0) {
    const seen = new Set<string>();
    for (const [id, v] of explicitNodes) {
      const normalizedID = normalizeNodeID(id);
      if (!edgeNodeIds.has(normalizedID) || seen.has(normalizedID)) continue;
      seen.add(normalizedID);
      const nodeLoc = findNodeLocation(sourceLines, id);
      ast.nodes.push({
        id: normalizedID,
        label: extractLabel(v),
        ...(nodeLoc || {}),
      });
    }
    for (const nodeID of edgeNodeIds) appendFlowchartNode(ast, nodeID, sourceLines, seen);
  } else {
    const seen = new Set<string>();
    for (const edge of ast.edges) {
      appendFlowchartNode(ast, edge.from, sourceLines, seen);
      appendFlowchartNode(ast, edge.to, sourceLines, seen);
    }
  }
}

function appendFlowchartNode(ast: ParserAST, id: string, sourceLines: string[], seen: Set<string>): void {
  if (!id || seen.has(id)) return;
  seen.add(id);
  const nodeLoc = findNodeLocation(sourceLines, id);
  ast.nodes.push({ id, label: "", ...(nodeLoc || {}) });
}

function addFlowchartSubgraphs(ast: ParserAST, subgraphs: unknown): void {
  const rawSubs = Array.isArray(subgraphs) ? subgraphs : [];
  for (const s of rawSubs) {
    ast.subgraphs.push({
      id: String(s.id ?? s.title ?? ""),
      label: String(s.title ?? s.id ?? ""),
      nodes: Array.isArray(s.nodes)
        ? s.nodes.map((n: unknown) => normalizeNodeID(n))
        : [],
    });
  }
}
