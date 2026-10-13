import { extractNamedDefinitions } from "./ast-common.ts";
import type { ParserAST, RawRecord, SourceLocation } from "./ast-types.ts";

export function extractSequenceAST(
  ast: ParserAST,
  db: RawRecord,
  sourceLines: string[],
): ParserAST {
  const state = db?.state?.records;
  if (!state) return ast;

  const definitions = extractNamedDefinitions(
    sourceLines,
    /^participant\s+(.+?)(?:\s+as\s+.+)?$/i,
  );
  const actors = appendSequenceMessages(ast, Array.isArray(state.messages) ? state.messages : []);
  appendParticipantNodes(ast, definitions);
  appendImplicitActors(ast, actors, definitions);
  return ast;
}

function appendSequenceMessages(ast: ParserAST, messages: RawRecord[]): Set<string> {
  const actors = new Set<string>();
  for (const message of messages) {
    const from = String(message.from || "").trim();
    const to = String(message.to || "").trim();
    if (from) actors.add(from);
    if (to) actors.add(to);

    ast.edges.push({
      from,
      to,
      type: message.type === 1 ? "dotted" : "solid",
      label: String(message.message || ""),
    });
  }
  return actors;
}

function appendParticipantNodes(
  ast: ParserAST,
  definitions: (SourceLocation & { id: string })[],
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

function appendImplicitActors(
  ast: ParserAST,
  actors: Set<string>,
  definitions: (SourceLocation & { id: string })[],
): void {
  const declaredActors = new Set(definitions.map(({ id }) => id));
  for (const actor of actors) {
    if (!declaredActors.has(actor)) ast.nodes.push({ id: actor, label: "" });
  }
}
