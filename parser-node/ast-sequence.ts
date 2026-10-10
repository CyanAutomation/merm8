import type { ParserAST, RawRecord, SourceLocation } from "./ast-types.ts";

export function extractSequenceAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const state = db?.state?.records;
  if (!state) return ast;

  // Extract explicitly defined participants from source (preserve duplicates for detection)
  const participantDefinitions: (SourceLocation & { id: string })[] = [];
  for (let i = 0; i < sourceLines.length; i++) {
    const trimmed = sourceLines[i].trim();
    // Match "participant X" or "participant X as Y" - capture full name (may include spaces)
    const participantMatch = trimmed.match(/^participant\s+(.+?)(?:\s+as\s+.+)?$/i);
    if (participantMatch) {
      participantDefinitions.push({
        id: participantMatch[1].trim(),
        line: i + 1,
        column: trimmed.indexOf(participantMatch[1]) + 1,
      });
    }
  }

  // Extract messages as edges
  const messages = Array.isArray(state.messages) ? state.messages : [];
  const allActors = new Set<string>();

  for (const msg of messages) {
    const from = String(msg.from || "").trim();
    const to = String(msg.to || "").trim();
    if (from) allActors.add(from);
    if (to) allActors.add(to);

    ast.edges.push({
      from,
      to,
      type: msg.type === 1 ? "dotted" : "solid",
      label: String(msg.message || ""),
    });
  }

  // Add all participant definitions as nodes (including duplicates for detection)
  for (const def of participantDefinitions) {
    ast.nodes.push({
      id: def.id,
      label: "",
      line: def.line,
      column: def.column,
    });
  }

  return ast;
}
