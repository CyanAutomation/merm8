import type { Analysis, Diagram } from "../domain/types.js";
import type { SemanticState } from "./types.js";

const MAX_NODES = 200;
const MAX_EDGES = 400;
const MAX_ISSUES = 100;
const MAX_LABEL_LENGTH = 512;
const MAX_STATE_BYTES = 64 * 1024;

export class SemanticStateLimitError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SemanticStateLimitError";
  }
}

function validateText(value: string, name: string): void {
  if (value.length > MAX_LABEL_LENGTH) {
    throw new SemanticStateLimitError(`${name} exceeds the ${MAX_LABEL_LENGTH}-character semantic review limit`);
  }
}

export function buildSemanticState(diagram: Diagram, analysis: Analysis): SemanticState {
  if (!analysis.valid || !analysis["diagram-type"]) {
    throw new Error("semantic state requires a successfully parsed diagram");
  }
  const semanticDiagram = diagram.semantic ?? diagram;
  if (semanticDiagram.nodes.length > MAX_NODES || semanticDiagram.edges.length > MAX_EDGES) {
    throw new SemanticStateLimitError(`semantic review supports at most ${MAX_NODES} nodes and ${MAX_EDGES} edges`);
  }

  for (const node of semanticDiagram.nodes) {
    validateText(node.id, "node ID");
    validateText(node.label, "node label");
  }
  for (const edge of semanticDiagram.edges) {
    validateText(edge.from, "edge source");
    validateText(edge.to, "edge destination");
    if (edge.label !== null) validateText(edge.label, "edge label");
  }

  const issueCount = analysis.issues.length;
  const issues = analysis.issues.slice(0, MAX_ISSUES).map(({ "rule-id": ruleId, severity }) => ({ "rule-id": ruleId, severity }));
  const outgoing = new Map<string, number>();
  for (const edge of semanticDiagram.edges) outgoing.set(edge.from, (outgoing.get(edge.from) ?? 0) + 1);

  const state: SemanticState = {
    diagram: {
      type: diagram.type,
      nodes: semanticDiagram.nodes.map(({ id, label }) => ({ id, label })),
      edges: semanticDiagram.edges.map(({ from, to, label }) => ({ from, to, label })),
    },
    structural: {
      valid: true,
      "issue-count": issueCount,
      issues,
      "issues-truncated": issueCount > issues.length,
    },
    "has-branching": [...outgoing.values()].some((count) => count > 1),
  };

  if (new TextEncoder().encode(JSON.stringify(state)).byteLength > MAX_STATE_BYTES) {
    throw new SemanticStateLimitError(`semantic state exceeds the ${MAX_STATE_BYTES}-byte limit`);
  }
  return state;
}
