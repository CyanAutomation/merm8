import { extractNamedDefinitions } from "./ast-common.ts";
import type { ParserAST, RawRecord } from "./ast-types.ts";

const relationTypes: Partial<Record<number, string>> = {
  0: "aggregation",
  1: "extension",
  2: "composition",
  3: "dependency",
  4: "lollipop",
};

export function extractClassAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const relations = Array.isArray(db.relations) ? db.relations : [];
  appendClassRelations(ast, relations);
  appendClassNodes(ast, extractNamedDefinitions(sourceLines, /^class\s+(\w+)/i));
  return ast;
}

function appendClassRelations(ast: ParserAST, relations: RawRecord[]): void {
  for (const relation of relations) {
    const id1 = String(relation.id1 || "").trim();
    const id2 = String(relation.id2 || "").trim();
    const type1 = relation.relation?.type1;
    const type = typeof type1 === "number" ? relationTypes[type1] ?? "dependency" : "dependency";
    const inheritance = type === "extension";
    ast.edges.push({
      from: inheritance ? id2 : id1,
      to: inheritance ? id1 : id2,
      type,
    });
  }
}

function appendClassNodes(
  ast: ParserAST,
  definitions: ReturnType<typeof extractNamedDefinitions>,
): void {
  for (const definition of definitions) {
    ast.nodes.push({
      id: definition.id,
      label: "",
      line: definition.line,
      column: definition.column,
    });
  }
}
