import type { Diagram, DiagramType } from "../domain/types.js";
import { buildDiagram, createState, type ParserError } from "./context.js";
import { parseLine } from "./line-parser.js";

type ParseResult = { diagram?: Diagram; error?: ParserError };

const typeFor = (source: string): DiagramType => {
  const first = source.split(/\r?\n/).find(line => line.trim() && !line.trim().startsWith("%%"))?.trim().toLowerCase() ?? "";
  if (/^(flowchart|graph)\b/.test(first)) return "flowchart";
  if (/^sequencediagram\b/.test(first)) return "sequence";
  if (/^classdiagram\b/.test(first)) return "class";
  if (/^erdiagram\b/.test(first)) return "er";
  if (/^statediagram(?:-v2)?\b/.test(first)) return "state";
  return "unknown";
};

/** A Worker-safe structural parser. It deliberately uses no DOM, Node runtime, or subprocess. */
export function parseMermaid(source: string): ParseResult {
  if (!source.trim()) return { error: { code: "empty_input", message: "empty input", line: 0, column: 0 } };
  const type = typeFor(source);
  if (type === "unknown") return { error: { code: "syntax_error", message: "Unsupported or unrecognised Mermaid diagram type", line: 1, column: 1 } };
  const state = createState(type);
  for (const [index, line] of source.split(/\r?\n/).entries()) {
    const error = parseLine(state, line, index + 1);
    if (error) return { error };
  }
  return { diagram: buildDiagram(state) };
}
