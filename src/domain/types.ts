export type DiagramType = "flowchart" | "sequence" | "class" | "er" | "state" | "unknown";
export type Severity = "error" | "warning" | "info";
export interface Node { id: string; label: string; line: number; column: number }
export interface Edge { from: string; to: string; label: string | null; line: number; column: number }
export interface Diagram {
  type: DiagramType;
  nodes: Node[];
  edges: Edge[];
  sourceNodeIds: string[];
  /** Targets of explicit `[*] --> state` transitions. */
  startNodeIds?: string[];
  /** Enriched parsed view for semantic review; deterministic rules continue to use nodes/edges above. */
  semantic?: { nodes: Node[]; edges: Edge[] };
}
export interface Issue { "rule-id": string; severity: Severity; message: string; line?: number; column?: number; fingerprint: string }
export type RuleConfig = Record<string, Record<string, unknown>>;
export interface AnalysisMetrics {
  "node-count": number;
  "edge-count": number;
  "disconnected-node-count": number;
  "duplicate-node-count": number;
  "max-fanin": number;
  "max-fanout": number;
  "diagram-type": DiagramType;
  "issue-counts": { "by-severity": Record<string, number>; "by-rule": Record<string, number> };
}
export interface Analysis { valid: boolean; "diagram-type"?: DiagramType; "lint-supported"?: boolean; issues: Issue[]; metrics?: AnalysisMetrics; error?: { code: string; message: string; line: number; column: number } }
