import type { ParserAST, RawRecord, SourceLocation } from "./ast-types.ts";

export function extractClassAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const relations = Array.isArray(db.relations) ? db.relations : [];
  const classes = db.classes || {};

  // Extract class definitions from source (preserve duplicates for detection)
  const classDefinitions: (SourceLocation & { id: string })[] = [];
  for (let i = 0; i < sourceLines.length; i++) {
    const trimmed = sourceLines[i].trim();
    // Match "class ClassName" or "class ClassName {"
    const classMatch = trimmed.match(/^class\s+(\w+)/i);
    if (classMatch) {
      classDefinitions.push({
        id: classMatch[1].trim(),
        line: i + 1,
        column: trimmed.indexOf(classMatch[1]) + 1,
      });
    }
  }

  // Extract relations as edges
  for (const rel of relations) {
    const id1 = String(rel.id1 || "").trim();
    const id2 = String(rel.id2 || "").trim();

    // Map relation types
    let relType = "dependency";
    const type1 = rel.relation?.type1;
    if (type1 === 0) relType = "aggregation";
    else if (type1 === 1) relType = "extension";
    else if (type1 === 2) relType = "composition";
    else if (type1 === 3) relType = "dependency";
    else if (type1 === 4) relType = "lollipop";

    // For inheritance (extension), the edge direction is from child to parent
    // In Mermaid: "Base <|-- Derived" means Derived extends Base
    // So edge should be from Derived (id2) to Base (id1)
    if (relType === "extension") {
      ast.edges.push({
        from: id2,
        to: id1,
        type: relType,
      });
    } else {
      ast.edges.push({
        from: id1,
        to: id2,
        type: relType,
      });
    }
  }

  // Add all class definitions as nodes (including duplicates for detection)
  for (const def of classDefinitions) {
    ast.nodes.push({
      id: def.id,
      label: "",
      line: def.line,
      column: def.column,
    });
  }

  return ast;
}
