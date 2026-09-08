export type DiagramType = "flowchart" | "sequence" | "class" | "er" | "state" | "unknown";
export type Severity = "error" | "warning" | "info";
export interface Node { id: string; label: string; line: number; column: number }
export interface Edge { from: string; to: string; line: number; column: number }
export interface Diagram { type: DiagramType; nodes: Node[]; edges: Edge[]; sourceNodeIds: string[] }
export interface Issue { "rule-id": string; severity: Severity; message: string; line?: number; column?: number; fingerprint: string }
export type RuleConfig = Record<string, Record<string, unknown>>;
export interface Analysis { valid: boolean; "diagram-type"?: DiagramType; issues: Issue[]; error?: { code: string; message: string; line: number; column: number } }
