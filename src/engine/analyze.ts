import { parseMermaid } from "../parser/parse.js";
import type { Analysis, Diagram, Issue, RuleConfig, Severity } from "../domain/types.js";

const severity = (id: string, fallback: Severity, config: RuleConfig): Severity => {
  const value = config[id]?.severity;
  return value === "error" || value === "warning" || value === "info" ? value : fallback;
};
const enabled = (id: string, config: RuleConfig) => config[id]?.enabled !== false;
const fingerprint = (value: string) => {
  let hash = 0x811c9dc5;
  for (let index = 0; index < value.length; index += 1) { hash ^= value.charCodeAt(index); hash = Math.imul(hash, 0x01000193); }
  return (hash >>> 0).toString(16).padStart(8, "0");
};
const issue = (id: string, fallback: Severity, message: string, line: number | undefined, column: number | undefined, config: RuleConfig): Issue => ({
  "rule-id": id, severity: severity(id, fallback, config), message, ...(line ? { line } : {}), ...(column ? { column } : {}),
  fingerprint: fingerprint(`${id}|${message}|${line ?? 0}|${column ?? 0}`)
});

function cyclic(diagram: Diagram): string[] {
  const edges = new Map<string, string[]>(); for (const edge of diagram.edges) edges.set(edge.from, [...(edges.get(edge.from) ?? []), edge.to]);
  const visiting = new Set<string>(), done = new Set<string>(), found = new Set<string>();
  const visit = (node: string) => { if (visiting.has(node)) { found.add(node); return; } if (done.has(node)) return; visiting.add(node); for (const next of edges.get(node) ?? []) visit(next); visiting.delete(node); done.add(node); };
  for (const node of diagram.nodes) visit(node.id); return [...found];
}

export function analyzeMermaid(code: string, config: RuleConfig = {}): Analysis {
  const parsed = parseMermaid(code); if (!parsed.diagram) return { valid: false, issues: [], error: parsed.error };
  const diagram = parsed.diagram; const issues: Issue[] = [];
  if (diagram.type === "flowchart") {
    const outgoing = new Map<string, number>(); const connected = new Set<string>();
    for (const edge of diagram.edges) { outgoing.set(edge.from, (outgoing.get(edge.from) ?? 0) + 1); connected.add(edge.from); connected.add(edge.to); }
    if (enabled("no-cycles", config)) for (const id of cyclic(diagram)) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("no-cycles", "error", `cycle detected involving node: ${id}`, node?.line, node?.column, config)); }
    if (enabled("max-fanout", config)) { const limit = typeof config["max-fanout"]?.limit === "number" ? config["max-fanout"].limit : 5; for (const [id, count] of outgoing) if (count > limit) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("max-fanout", "warning", `node "${id}" has fanout ${count} (limit ${limit})`, node?.line, node?.column, config)); } }
    if (enabled("no-disconnected-nodes", config)) for (const node of diagram.nodes) if (!connected.has(node.id)) issues.push(issue("no-disconnected-nodes", "error", `node is disconnected: ${node.id}`, node.line, node.column, config));
    if (enabled("no-duplicate-node-ids", config)) { const counts = new Map<string, number>(); for (const id of diagram.sourceNodeIds) counts.set(id, (counts.get(id) ?? 0) + 1); for (const [id, count] of counts) if (count > 1) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("no-duplicate-node-ids", "error", `duplicate node ID: ${id}`, node?.line, node?.column, config)); } }
  }
  return { valid: true, "diagram-type": diagram.type, issues: issues.sort((a, b) => (a.line ?? 0) - (b.line ?? 0) || a["rule-id"].localeCompare(b["rule-id"])) };
}

export const supportedRules = ["max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids"] as const;
