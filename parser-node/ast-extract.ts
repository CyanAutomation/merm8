import { extractClassAST } from "./ast-class.ts";
import { extractERAST } from "./ast-er.ts";
import { extractFlowchartAST } from "./ast-flowchart.ts";
import { extractSequenceAST } from "./ast-sequence.ts";
import { extractStateAST } from "./ast-state.ts";
import { extractSuppressions } from "./ast-common.ts";
import type { DiagramType, MermaidAPI, ParserAST, RawRecord } from "./ast-types.ts";

export async function extractAST(
  mermaidAPI: MermaidAPI,
  source: string,
  diagramType: DiagramType,
): Promise<ParserAST> {
  const ast: ParserAST = {
    type: diagramType,
    direction: "TD",
    nodes: [],
    edges: [],
    subgraphs: [],
    suppressions: extractSuppressions(source),
  };
  const sourceLines = source.split(/\r?\n/);
  const db = await diagramDatabase(mermaidAPI, source);
  if (!db) {
    if (diagramType !== "flowchart") return ast;
    throw new Error("AST extraction failed in parser runtime");
  }
  if (diagramType === "sequence") return extractSequenceAST(ast, db, sourceLines);
  if (diagramType === "class") return extractClassAST(ast, db, sourceLines);
  if (diagramType === "er") return extractERAST(ast, db, sourceLines);
  if (diagramType === "state") return extractStateAST(ast, db, sourceLines);
  if (diagramType !== "flowchart") return ast;
  return extractFlowchartAST(ast, db, sourceLines);
}

async function diagramDatabase(mermaidAPI: MermaidAPI, source: string): Promise<RawRecord | null> {
  try {
    const diagram = await mermaidAPI.getDiagramFromText(source);
    return diagram?.db ?? null;
  } catch (_) {}
  return null;
}
