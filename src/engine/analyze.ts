import { parseMermaid } from "../parser/parse.js";
import type { Analysis, Diagram, RuleConfig } from "../domain/types.js";
import { buildMetrics } from "./metrics.js";
import { evaluateRules } from "./rules.js";

export function analyzeDiagram(diagram: Diagram, config: RuleConfig = {}): Analysis {
  const issues = evaluateRules(diagram, config).sort((a, b) => (a.line ?? 0) - (b.line ?? 0) || a["rule-id"].localeCompare(b["rule-id"]));
  return {
    valid: true,
    "diagram-type": diagram.type,
    "lint-supported": diagram.type !== "unknown",
    issues,
    metrics: buildMetrics(diagram, issues),
  };
}

export function analyzeMermaid(code: string, config: RuleConfig = {}): Analysis {
  const parsed = parseMermaid(code);
  if (!parsed.diagram) return { valid: false, issues: [], error: parsed.error };
  return analyzeDiagram(parsed.diagram, config);
}

export const supportedRules = ["max-fanout", "no-cycles", "no-disconnected-nodes", "no-duplicate-node-ids", "no-undefined-actors", "no-duplicate-classes", "no-self-referential", "no-unreachable-state"] as const;
