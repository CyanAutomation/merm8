import type { Diagram, Issue, RuleConfig, Severity } from "../domain/types.js";
import { declarationCounts } from "./common.js";

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
  const edges = new Map<string, string[]>();
  for (const edge of diagram.edges) edges.set(edge.from, [...(edges.get(edge.from) ?? []), edge.to]);
  const visiting = new Set<string>(), done = new Set<string>(), found = new Set<string>();
  const visit = (node: string) => {
    if (visiting.has(node)) { found.add(node); return; }
    if (done.has(node)) return;
    visiting.add(node);
    for (const next of edges.get(node) ?? []) visit(next);
    visiting.delete(node);
    done.add(node);
  };
  for (const node of diagram.nodes) visit(node.id);
  return [...found];
}

function nodeFor(diagram: Diagram, id: string) {
  return diagram.nodes.find(node => node.id === id);
}

function flowchartCycleIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-cycles", config)) return [];
  return cyclic(diagram).map(id => {
    const node = nodeFor(diagram, id);
    return issue("no-cycles", "error", `cycle detected involving node: ${id}`, node?.line, node?.column, config);
  });
}

function flowchartFanoutIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("max-fanout", config)) return [];
  const limit = typeof config["max-fanout"]?.limit === "number" ? config["max-fanout"].limit : 5;
  const outgoing = new Map<string, number>();
  for (const edge of diagram.edges) outgoing.set(edge.from, (outgoing.get(edge.from) ?? 0) + 1);
  const findings: Issue[] = [];
  for (const [id, count] of outgoing) {
    if (count <= limit) continue;
    const node = nodeFor(diagram, id);
    findings.push(issue("max-fanout", "warning", `node "${id}" has fanout ${count} (limit ${limit})`, node?.line, node?.column, config));
  }
  return findings;
}

function disconnectedFlowchartIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-disconnected-nodes", config)) return [];
  const connected = new Set(diagram.edges.flatMap(edge => [edge.from, edge.to]));
  return diagram.nodes
    .filter(node => !connected.has(node.id))
    .map(node => issue("no-disconnected-nodes", "error", `node is disconnected: ${node.id}`, node.line, node.column, config));
}

function duplicateDeclarationIssues(
  diagram: Diagram,
  config: RuleConfig,
  ruleId: string,
  message: (id: string) => string,
): Issue[] {
  return [...declarationCounts(diagram.sourceNodeIds)]
    .filter(([, count]) => count > 1)
    .map(([id]) => {
      const node = nodeFor(diagram, id);
      return issue(ruleId, "error", message(id), node?.line, node?.column, config);
    });
}

function flowchartIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  return [
    ...flowchartCycleIssues(diagram, config),
    ...flowchartFanoutIssues(diagram, config),
    ...disconnectedFlowchartIssues(diagram, config),
    ...(enabled("no-duplicate-node-ids", config)
      ? duplicateDeclarationIssues(diagram, config, "no-duplicate-node-ids", id => `duplicate node ID: ${id}`)
      : []),
  ];
}

function undefinedActorIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-undefined-actors", config)) return [];
  const declared = new Set(diagram.sourceNodeIds);
  const reported = new Set<string>();
  const findings: Issue[] = [];
  for (const edge of diagram.edges) {
    for (const id of [edge.from, edge.to]) {
      if (declared.has(id) || reported.has(id)) continue;
      reported.add(id);
      const node = nodeFor(diagram, id);
      findings.push(issue("no-undefined-actors", "error", `undefined actor reference: ${id}`, node?.line ?? edge.line, node?.column ?? edge.column, config));
    }
  }
  return findings;
}

function duplicateClassIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-duplicate-classes", config)) return [];
  return duplicateDeclarationIssues(diagram, config, "no-duplicate-classes", id => `duplicate class declaration: ${id}`);
}

function selfReferentialIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-self-referential", config)) return [];
  return diagram.edges
    .filter(edge => edge.from === edge.to)
    .map(edge => issue("no-self-referential", "warning", `entity has a self-referential relationship: ${edge.from}`, edge.line, edge.column, config));
}

function unreachableStateIssues(diagram: Diagram, config: RuleConfig): Issue[] {
  if (!enabled("no-unreachable-state", config)) return [];
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
  return diagram.nodes
    .filter(node => !reachable.has(node.id))
    .map(node => issue("no-unreachable-state", "error", `state is unreachable from the initial state: ${node.id}`, node.line, node.column, config));
}

export function evaluateRules(diagram: Diagram, config: RuleConfig): Issue[] {
  switch (diagram.type) {
    case "flowchart": return flowchartIssues(diagram, config);
    case "sequence": return undefinedActorIssues(diagram, config);
    case "class": return duplicateClassIssues(diagram, config);
    case "er": return selfReferentialIssues(diagram, config);
    case "state": return unreachableStateIssues(diagram, config);
    case "unknown": return [];
  }
}
