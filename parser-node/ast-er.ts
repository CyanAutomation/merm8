import { findNodeLocation } from "./ast-common.ts";
import type { ParserAST, RawRecord } from "./ast-types.ts";

export function extractERAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const relationships = Array.isArray(db.relationships) ? db.relationships : [];

  // Extract entity names from relationships
  const entityNames = new Set<string>();
  for (const rel of relationships) {
    // Entity names are prefixed with "entity-" and suffixed with "-N"
    const entityA = String(rel.entityA || "").replace(/^entity-/, "").replace(/-\d+$/, "");
    const entityB = String(rel.entityB || "").replace(/^entity-/, "").replace(/-\d+$/, "");

    if (entityA) entityNames.add(entityA);
    if (entityB) entityNames.add(entityB);

    // Map relationship types
    let relType = "identifying";
    if (rel.relSpec?.relType === "NON_IDENTIFYING") {
      relType = "non-identifying";
    }

    ast.edges.push({
      from: entityA,
      to: entityB,
      type: relType,
      label: String(rel.roleA || rel.roleB || ""),
    });
  }

  // Add entities as nodes
  for (const entityName of entityNames) {
    const loc = findNodeLocation(sourceLines, entityName);
    ast.nodes.push({
      id: entityName,
      label: "",
      ...(loc || {}),
    });
  }

  return ast;
}
