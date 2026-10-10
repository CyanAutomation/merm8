import type { Diagram, DiagramType } from "../domain/types.js";

export type ParserError = { code: string; message: string; line: number; column: number };
export type ParserState = {
  type: DiagramType;
  nodes: Diagram["nodes"];
  edges: Diagram["edges"];
  sourceNodeIds: string[];
  startNodeIds: string[];
  semanticNodes: Diagram["nodes"];
  semanticEdges: Diagram["edges"];
  seen: Set<string>;
  semanticSeen: Map<string, Diagram["nodes"][number]>;
};

export function createState(type: DiagramType): ParserState {
  return {
    type,
    nodes: [],
    edges: [],
    sourceNodeIds: [],
    startNodeIds: [],
    semanticNodes: [],
    semanticEdges: [],
    seen: new Set(),
    semanticSeen: new Map(),
  };
}

function normalizeId(id: string): string {
  return id.trim().replace(/^\[\*\]$/, "*");
}

export function addNode(state: ParserState, id: string, line: number, column: number, declaration = false): void {
  const normalized = normalizeId(id);
  if (!normalized || normalized === "*") return;
  if (declaration) state.sourceNodeIds.push(normalized);
  if (state.seen.has(normalized)) return;
  state.seen.add(normalized);
  state.nodes.push({ id: normalized, label: normalized, line, column });
}

export function addSemanticNode(state: ParserState, id: string, line: number, column: number, label = id): void {
  const normalized = normalizeId(id);
  if (!normalized || normalized === "*") return;
  const existing = state.semanticSeen.get(normalized);
  if (!existing) {
    const node = { id: normalized, label: label.trim() || normalized, line, column };
    state.semanticSeen.set(normalized, node);
    state.semanticNodes.push(node);
    return;
  }
  if (existing.label === normalized && label.trim() && label.trim() !== normalized) existing.label = label.trim();
}

export function extractLabel(line: string, id: string, column: number): string {
  const following = line.slice(column - 1 + id.length).trimStart();
  const shapes: Array<[string, string]> = [
    ["[[", "]]"], ["[(", ")]"], ["(((", ")))"], ["((", "))"], ["{{", "}}"],
    ["[/", "/]"], ["[\\", "\\]"], ["[", "]"], ["(", ")"], ["{", "}"], [">", "]"],
  ];
  const shape = shapes.find(([open]) => following.startsWith(open));
  if (!shape) return id;
  const [open, close] = shape;
  const end = following.indexOf(close, open.length);
  if (end < 0) return id;
  const raw = following.slice(open.length, end).trim();
  const quoted = raw.length >= 2 && ((raw.startsWith('"') && raw.endsWith('"')) || (raw.startsWith("'") && raw.endsWith("'")) || (raw.startsWith("`") && raw.endsWith("`")));
  const unquoted = quoted ? raw.slice(1, -1) : raw;
  return unquoted.replace(/<[^>]*>/g, " ").replace(/\\n/g, " ").replace(/\s+/g, " ").trim() || id;
}

export function isExplicitDeclaration(line: string, id: string, column: number): boolean {
  return /^\s*(?:\[|\(|\{)/.test(line.slice(column - 1 + id.length));
}

export function destinationColumn(line: string, fromColumn: number, from: string, type: Diagram["type"]): number {
  const sourceEnd = fromColumn - 1 + from.length;
  const tail = line.slice(sourceEnd);
  const arrow = (type === "sequence" ? /(?:--?>|-->>|->>)/ : /(?:-->|---|--|\.\.>|==>)/).exec(tail);
  if (!arrow) return line.indexOf(from, sourceEnd) + 1;
  let destination = sourceEnd + arrow.index + arrow[0].length;
  while (/\s/.test(line[destination] ?? "")) destination += 1;
  if (line[destination] === "|") {
    const labelEnd = line.indexOf("|", destination + 1);
    if (labelEnd >= 0) destination = labelEnd + 1;
    while (/\s/.test(line[destination] ?? "")) destination += 1;
  }
  return destination + 1;
}

export function skipLine(line: string): boolean {
  const trimmed = line.trim();
  return !trimmed || trimmed.startsWith("%%") || /^(flowchart|graph|sequenceDiagram|classDiagram|erDiagram|stateDiagram)/i.test(trimmed);
}

export function buildDiagram(state: ParserState): Diagram {
  return {
    type: state.type,
    nodes: state.nodes,
    edges: state.edges,
    sourceNodeIds: state.sourceNodeIds,
    ...(state.startNodeIds.length ? { startNodeIds: state.startNodeIds } : {}),
    semantic: { nodes: state.semanticNodes, edges: state.semanticEdges },
  };
}
