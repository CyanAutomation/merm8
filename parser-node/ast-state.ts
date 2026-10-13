import { findNodeLocation } from "./ast-common.ts";
import type { ParserAST, RawRecord } from "./ast-types.ts";

export function extractStateAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const nodes = Array.isArray(db.nodes) ? db.nodes : [];
  const edges = Array.isArray(db.edges) ? db.edges : [];
  appendStateNodes(ast, nodes, sourceLines);
  const startTransitions = appendStateEdges(ast, edges);
  if (startTransitions.length > 0) ast.startStates = startTransitions;
  return ast;
}

function appendStateNodes(ast: ParserAST, nodes: RawRecord[], sourceLines: string[]): void {
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
}

function appendStateEdges(ast: ParserAST, edges: RawRecord[]): string[] {
  const startTransitions: string[] = [];
  for (const edge of edges) {
    const from = String(edge.start || "").trim();
    const to = String(edge.end || "").trim();

    if (isStartTransition(from, to)) {
      startTransitions.push(to);
      continue;
    }

    if (isSyntheticTransition(from, to)) continue;

    ast.edges.push({
      from,
      to,
      type: "transition",
      label: String(edge.label || ""),
    });
  }
  return startTransitions;
}

function isStartTransition(from: string, to: string): boolean {
  return from === "root_start" && to !== "root_end";
}

function isSyntheticTransition(from: string, to: string): boolean {
  return from === "root_end" || to === "root_start" || to === "root_end";
}
