export type DiagramType = "flowchart" | "sequence" | "class" | "er" | "state" | "unknown";
export type SourceLocation = { line?: number; column?: number };
export type NodeAST = SourceLocation & { id: string; label: string };
export type EdgeAST = SourceLocation & {
  from: string;
  to: string;
  type: string;
  label?: string;
};
export type SubgraphAST = { id: string; label: string; nodes: string[] };
export type Suppression = {
  ruleId: string;
  scope: "file" | "next-line";
  line: number;
  targetLine: number;
};
export type ParserAST = {
  type: DiagramType;
  direction: string;
  nodes: NodeAST[];
  edges: EdgeAST[];
  subgraphs: SubgraphAST[];
  suppressions: Suppression[];
  startStates?: string[];
};
export type ParseResult =
  | { valid: true; diagram_type: DiagramType; ast: ParserAST }
  | { valid: false; error: { message: string; line: number; column: number } };
export type RawRecord = Record<string, any>;
export type MermaidAPI = {
  getDiagramFromText(source: string): Promise<{ db?: RawRecord | null }>;
};
export type MermaidRuntime = {
  version?: string;
  initialize(config: { startOnLoad: boolean }): void;
  detectType(source: string, options: { suppressErrors: boolean }): string;
  parse(source: string): Promise<unknown>;
  mermaidAPI: MermaidAPI;
};
