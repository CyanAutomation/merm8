import type { Analysis, Diagram, Issue } from "../domain/types.js";
import { declarationCounts } from "./common.js";

function maximum(counts: Map<string, number>): number {
  let value = 0;
  for (const count of counts.values()) if (count > value) value = count;
  return value;
}

function issueCounts(issues: Issue[]): NonNullable<Analysis["metrics"]>["issue-counts"] {
  const bySeverity: Record<string, number> = {};
  const byRule: Record<string, number> = {};
  for (const item of issues) {
    bySeverity[item.severity] = (bySeverity[item.severity] ?? 0) + 1;
    byRule[item["rule-id"]] = (byRule[item["rule-id"]] ?? 0) + 1;
  }
  return { "by-severity": bySeverity, "by-rule": byRule };
}

export function buildMetrics(diagram: Diagram, issues: Issue[]): NonNullable<Analysis["metrics"]> {
  const fanout = new Map<string, number>();
  const fanin = new Map<string, number>();
  const connected = new Set<string>();
  for (const edge of diagram.edges) {
    fanout.set(edge.from, (fanout.get(edge.from) ?? 0) + 1);
    fanin.set(edge.to, (fanin.get(edge.to) ?? 0) + 1);
    connected.add(edge.from);
    connected.add(edge.to);
  }
  const duplicateNodeCount = [...declarationCounts(diagram.sourceNodeIds)].filter(([, count]) => count > 1).length;
  return {
    "node-count": diagram.nodes.length,
    "edge-count": diagram.edges.length,
    "disconnected-node-count": diagram.nodes.filter(node => !connected.has(node.id)).length,
    "duplicate-node-count": duplicateNodeCount,
    "max-fanin": maximum(fanin),
    "max-fanout": maximum(fanout),
    "diagram-type": diagram.type,
    "issue-counts": issueCounts(issues),
  };
}
