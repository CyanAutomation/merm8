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

export function analyzeDiagram(diagram: Diagram, config: RuleConfig = {}): Analysis {
  const issues: Issue[] = [];
  if (diagram.type === "flowchart") {
    const outgoing = new Map<string, number>(); const connected = new Set<string>();
    for (const edge of diagram.edges) { outgoing.set(edge.from, (outgoing.get(edge.from) ?? 0) + 1); connected.add(edge.from); connected.add(edge.to); }
    if (enabled("no-cycles", config)) for (const id of cyclic(diagram)) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("no-cycles", "error", `cycle detected involving node: ${id}`, node?.line, node?.column, config)); }
    if (enabled("max-fanout", config)) { const limit = typeof config["max-fanout"]?.limit === "number" ? config["max-fanout"].limit : 5; for (const [id, count] of outgoing) if (count > limit) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("max-fanout", "warning", `node "${id}" has fanout ${count} (limit ${limit})`, node?.line, node?.column, config)); } }
    if (enabled("no-disconnected-nodes", config)) for (const node of diagram.nodes) if (!connected.has(node.id)) issues.push(issue("no-disconnected-nodes", "error", `node is disconnected: ${node.id}`, node.line, node.column, config));
    if (enabled("no-duplicate-node-ids", config)) { const counts = new Map<string, number>(); for (const id of diagram.sourceNodeIds) counts.set(id, (counts.get(id) ?? 0) + 1); for (const [id, count] of counts) if (count > 1) { const node = diagram.nodes.find(n => n.id === id); issues.push(issue("no-duplicate-node-ids", "error", `duplicate node ID: ${id}`, node?.line, node?.column, config)); } }
  }
  if (diagram.type === "sequence" && enabled("no-undefined-actors", config)) {
    const declared = new Set(diagram.sourceNodeIds);
    const reported = new Set<string>();
    for (const edge of diagram.edges) for (const id of [edge.from, edge.to]) {
      if (!declared.has(id) && !reported.has(id)) {
        reported.add(id);
        const node = diagram.nodes.find(candidate => candidate.id === id);
        issues.push(issue("no-undefined-actors", "error", `undefined actor reference: ${id}`, node?.line ?? edge.line, node?.column ?? edge.column, config));
      }
    }
  }
  if (diagram.type === "class" && enabled("no-duplicate-classes", config)) {
    const counts = new Map<string, number>();
    for (const id of diagram.sourceNodeIds) counts.set(id, (counts.get(id) ?? 0) + 1);
    for (const [id, count] of counts) if (count > 1) {
      const node = diagram.nodes.find(candidate => candidate.id === id);
      issues.push(issue("no-duplicate-classes", "error", `duplicate class declaration: ${id}`, node?.line, node?.column, config));
    }
  }
  if (diagram.type === "er" && enabled("no-self-referential", config)) {
    for (const edge of diagram.edges) if (edge.from === edge.to) {
      issues.push(issue("no-self-referential", "warning", `entity has a self-referential relationship: ${edge.from}`, edge.line, edge.column, config));
    }
  }
  if (diagram.type === "state" && enabled("no-unreachable-state", config)) {
    const starts = diagram.startNodeIds?.length
      ? new Set(diagram.startNodeIds)
      : new Set(diagram.nodes.map(node => node.id).filter(id => !diagram.edges.some(edge => edge.to === id)));
    const outgoing = new Map<string, string[]>();
    for (const edge of diagram.edges) outgoing.set(edge.from, [...(outgoing.get(edge.from) ?? []), edge.to]);
    const reachable = new Set<string>();
    const pending = [...starts];
    while (pending.length) {
      const id = pending.pop()!;
      if (reachable.has(id)) continue;
      reachable.add(id);
      pending.push(...(outgoing.get(id) ?? []));
    }
    for (const node of diagram.nodes) if (!reachable.has(node.id)) {
      issues.push(issue("no-unreachable-state", "error", `state is unreachable from the initial state: ${node.id}`, node.line, node.column, config));
    }
  }
  const sortedIssues = issues.sort((a, b) => (a.line ?? 0) - (b.line ?? 0) || a["rule-id"].localeCompare(b["rule-id"]));
  const fanout = new Map<string, number>(); const fanin = new Map<string, number>(); const connected = new Set<string>();
  for (const edge of diagram.edges) {
    fanout.set(edge.from, (fanout.get(edge.from) ?? 0) + 1);
    fanin.set(edge.to, (fanin.get(edge.to) ?? 0) + 1);
    connected.add(edge.from); connected.add(edge.to);
  }
  const max = (counts: Map<string, number>) => {
    let maximum = 0;
    for (const count of counts.values()) if (count > maximum) maximum = count;
    return maximum;
  };
  const duplicateIds = new Set<string>(); const declarations = new Map<string, number>();
  for (const id of diagram.sourceNodeIds) { declarations.set(id, (declarations.get(id) ?? 0) + 1); if ((declarations.get(id) ?? 0) > 1) duplicateIds.add(id); }
  const bySeverity: Record<string, number> = {}; const byRule: Record<string, number> = {};
  for (const item of sortedIssues) { bySeverity[item.severity] = (bySeverity[item.severity] ?? 0) + 1; byRule[item["rule-id"]] = (byRule[item["rule-id"]] ?? 0) + 1; }
  return {
    valid: true,
    "diagram-type": diagram.type,
    "lint-supported": diagram.type !== "unknown",
    issues: sortedIssues,
    metrics: {
      "node-count": diagram.nodes.length,
      "edge-count": diagram.edges.length,
      "disconnected-node-count": diagram.nodes.filter(node => !connected.has(node.id)).length,
      "duplicate-node-count": duplicateIds.size,
      "max-fanin": max(fanin),
      "max-fanout": max(fanout),
      "diagram-type": diagram.type,
      "issue-counts": { "by-severity": bySeverity, "by-rule": byRule },
    },
  };
}

export function analyzeMermaid(code: string, config: RuleConfig = {}): Analysis {
  const parsed = parseMermaid(code);
  if (!parsed.diagram) return { valid: false, issues: [], error: parsed.error };
  return analyzeDiagram(parsed.diagram, config);
}

export const supportedRules = ["max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids", "no-undefined-actors", "no-duplicate-classes", "no-self-referential", "no-unreachable-state"] as const;
