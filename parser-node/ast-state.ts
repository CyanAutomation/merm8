import { findNodeLocation } from "./ast-common.ts";
import type { ParserAST, RawRecord } from "./ast-types.ts";

export function extractStateAST(
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
