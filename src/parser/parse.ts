import type { Diagram, DiagramType } from "../domain/types.js";

const typeFor = (source: string): DiagramType => {
  const first = source.split(/\r?\n/).find(line => line.trim() && !line.trim().startsWith("%%"))?.trim().toLowerCase() ?? "";
  if (/^(flowchart|graph)\b/.test(first)) return "flowchart";
  if (/^sequencediagram\b/.test(first)) return "sequence";
  if (/^classdiagram\b/.test(first)) return "class";
  if (/^erdiagram\b/.test(first)) return "er";
  if (/^statediagram(?:-v2)?\b/.test(first)) return "state";
  return "unknown";
};

/** A Worker-safe structural parser. It deliberately uses no DOM, Node runtime, or subprocess. */
export function parseMermaid(source: string): { diagram?: Diagram; error?: { code: string; message: string; line: number; column: number } } {
  if (!source.trim()) return { error: { code: "empty_input", message: "empty input", line: 0, column: 0 } };
  const type = typeFor(source);
  if (type === "unknown") return { error: { code: "syntax_error", message: "Unsupported or unrecognised Mermaid diagram type", line: 1, column: 1 } };
  const nodes: Diagram["nodes"] = []; const edges: Diagram["edges"] = []; const sourceNodeIds: string[] = [];
  const semanticNodes: Diagram["nodes"] = []; const semanticEdges: Diagram["edges"] = [];
  const seen = new Set<string>(); const semanticSeen = new Map<string, Diagram["nodes"][number]>();
  const add = (id: string, line: number, column: number, isDeclaration = false) => {
    const normalized = id.trim().replace(/^\[\*\]$/, "*");
    if (!normalized || normalized === "*") return;
    if (isDeclaration) sourceNodeIds.push(normalized);
    if (!seen.has(normalized)) { seen.add(normalized); nodes.push({ id: normalized, label: normalized, line, column }); }
  };
  const addSemantic = (id: string, line: number, column: number, label = id) => {
    const normalized = id.trim().replace(/^\[\*\]$/, "*");
    if (!normalized || normalized === "*") return;
    const existing = semanticSeen.get(normalized);
    if (!existing) {
      const node = { id: normalized, label: label.trim() || normalized, line, column };
      semanticSeen.set(normalized, node);
      semanticNodes.push(node);
    } else if (existing.label === normalized && label.trim() && label.trim() !== normalized) {
      existing.label = label.trim();
    }
  };
  const extractLabel = (line: string, id: string, column: number): string => {
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
    const unquoted = raw.length >= 2 && ((raw.startsWith('"') && raw.endsWith('"')) || (raw.startsWith("'") && raw.endsWith("'")) || (raw.startsWith("`") && raw.endsWith("`")))
      ? raw.slice(1, -1)
      : raw;
    return unquoted.replace(/<[^>]*>/g, " ").replace(/\\n/g, " ").replace(/\s+/g, " ").trim() || id;
  };
  const isExplicitDeclaration = (line: string, id: string, column: number) =>
    /^\s*(?:\[|\(|\{)/.test(line.slice(column - 1 + id.length));
  const destinationColumn = (line: string, fromColumn: number, from: string, diagramType: Diagram["type"]): number => {
    const sourceEnd = fromColumn - 1 + from.length;
    const tail = line.slice(sourceEnd);
    const arrow = (diagramType === "sequence" ? /(?:--?>|-->>|->>)/ : /(?:-->|---|--|\.\.>|==>)/).exec(tail);
    if (!arrow) return line.indexOf(from, sourceEnd) + 1;
    let destination = sourceEnd + arrow.index + arrow[0].length;
    while (/\s/.test(line[destination] ?? "")) destination += 1;
    if (line[destination] === "|") {
      const labelEnd = line.indexOf("|", destination + 1);
      if (labelEnd >= 0) destination = labelEnd + 1;
      while (/\s/.test(line[destination] ?? "")) destination += 1;
    }
    return destination + 1;
  };
  for (const [i, line] of source.split(/\r?\n/).entries()) {
    const number = i + 1; const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("%%") || /^(flowchart|graph|sequenceDiagram|classDiagram|erDiagram|stateDiagram)/i.test(trimmed)) continue;
    if (type === "flowchart" && /(?:-->|---|--|\.\.>|==>)\s*(?:-->|---|--|\.\.>|==>)/.test(line)) {
      return { error: { code: "syntax_error", message: "Malformed flowchart relation", line: number, column: 1 } };
    }
    if (type === "flowchart" && /(?:-->|---|--|\.\.>|==>)\s*$/.test(line)) {
      return { error: { code: "syntax_error", message: "Flowchart relation is missing a destination node", line: number, column: 1 } };
    }
    const relation = type === "sequence" ? /^\s*([\w.-]+).*?(?:--?>|-->>|->>)\s*([\w.-]+)/ : /^\s*([^\s\[{(<-]+).*?(?:-->|---|--|\.\.>|==>)\s*([^\s\[{(<:]+)/;
    const match = line.match(relation);
    if (match) {
      const from = match[1].trim();
      const to = match[2].trim();
      const fromCol = line.indexOf(from) + 1;
      const toCol = line.indexOf(to, fromCol);
      add(from, number, fromCol, isExplicitDeclaration(line, from, fromCol));
      add(to, number, toCol + 1, isExplicitDeclaration(line, to, toCol + 1));
      let semanticTo = to;
      let edgeLabel: string | null = null;
      let semanticToCol = destinationColumn(line, fromCol, from, type);
      if (type !== "sequence" && to.startsWith("|")) {
        const firstPipe = line.indexOf("|", toCol);
        const lastPipe = line.indexOf("|", firstPipe + 1);
        if (lastPipe > firstPipe) {
          edgeLabel = line.slice(firstPipe + 1, lastPipe).trim() || null;
          const target = line.slice(lastPipe + 1).match(/^\s*([^\s\[{(<:|]+)/)?.[1];
          if (target) {
            semanticTo = target;
            semanticToCol = destinationColumn(line, fromCol, from, type);
          }
        }
      }
      const colon = line.indexOf(":");
      const sequenceMessage = type === "sequence" && colon >= 0 ? line.slice(colon + 1).trim() : "";
      if (type === "sequence") edgeLabel = sequenceMessage || null;
      if (type === "er") {
        const tailStart = fromCol - 1 + from.length;
        const tail = line.slice(tailStart);
        const relationArrow = /(?:--|\.\.)/.exec(tail);
        if (relationArrow) {
          const targetOffset = tailStart + relationArrow.index + relationArrow[0].length;
          const target = line.slice(targetOffset).match(/^\s*(?:[|o{}]+\s*)?([A-Za-z0-9_.-]+)/)?.[1];
          if (target) {
            semanticTo = target;
            semanticToCol = line.indexOf(target, targetOffset) + 1;
          }
          const relationship = line.match(/:\s*([^:]+?)\s*$/)?.[1]?.trim();
          if (relationship) edgeLabel = relationship;
        }
      }
      addSemantic(from, number, fromCol, extractLabel(line, from, fromCol));
      addSemantic(semanticTo, number, semanticToCol, extractLabel(line, semanticTo, semanticToCol));
      edges.push({ from, to, label: null, line: number, column: fromCol });
      semanticEdges.push({ from, to: semanticTo, label: edgeLabel, line: number, column: fromCol });
      continue;
    }
    if (type === "flowchart") {
      const standalone = line.match(/^\s*([A-Za-z0-9_.-]+)\s*(?:\[|\(|\{|$)/);
      if (standalone) {
        const column = line.indexOf(standalone[1]) + 1;
        add(standalone[1], number, column, true);
        addSemantic(standalone[1], number, column, extractLabel(line, standalone[1], column));
      }
    }
  }
  return { diagram: { type, nodes, edges, sourceNodeIds, semantic: { nodes: semanticNodes, edges: semanticEdges } } };
}
