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
  const seen = new Set<string>();
  const add = (id: string, line: number, column: number) => {
    const normalized = id.trim().replace(/^\[\*\]$/, "*");
    if (!normalized || normalized === "*") return;
    sourceNodeIds.push(normalized);
    if (!seen.has(normalized)) { seen.add(normalized); nodes.push({ id: normalized, label: normalized, line, column }); }
  };
  source.split(/\r?\n/).forEach((line, i) => {
    const number = i + 1; const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("%%") || /^(flowchart|graph|sequenceDiagram|classDiagram|erDiagram|stateDiagram)/i.test(trimmed)) return;
    const relation = type === "sequence" ? /^\s*([\w.-]+).*?(?:--?>|-->>|->>)\s*([\w.-]+)/ : /^\s*([^\s\[{(<-]+).*?(?:-->|---|--|\.\.>|==>)\s*([^\s\[{(<:]+)/;
    const match = line.match(relation);
    if (match) { const from = match[1].trim(); const to = match[2].trim(); add(from, number, line.indexOf(from) + 1); add(to, number, line.lastIndexOf(to) + 1); edges.push({ from, to, line: number, column: line.indexOf(from) + 1 }); return; }
    if (type === "flowchart") {
      const standalone = line.match(/^\s*([A-Za-z0-9_.-]+)\s*(?:\[|\(|\{|$)/);
      if (standalone) add(standalone[1], number, line.indexOf(standalone[1]) + 1);
    }
  });
  return { diagram: { type, nodes, edges, sourceNodeIds } };
}
