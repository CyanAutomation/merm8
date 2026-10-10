import type { DiagramType } from "../domain/types.js";
import {
  addNode,
  addSemanticNode,
  destinationColumn,
  extractLabel,
  isExplicitDeclaration,
  skipLine,
  type ParserError,
  type ParserState,
} from "./context.js";

type RelationSemantics = { target: string; targetColumn: number; label: string | null };

function parseSequenceDeclaration(state: ParserState, line: string, number: number): boolean {
  const participant = line.match(/^\s*(?:participant|actor)\s+([\w.-]+)(?:\s+as\s+([\w.-]+))?/i);
  if (!participant) return false;
  const id = participant[2] ?? participant[1];
  const column = line.indexOf(id) + 1;
  addNode(state, id, number, column, true);
  addSemanticNode(state, id, number, column, participant[1]);
  return true;
}

function parseClassDeclaration(state: ParserState, line: string, number: number): boolean {
  const declaration = line.match(/^\s*class\s+([\w.-]+)/i);
  if (!declaration) return false;
  const id = declaration[1];
  const column = line.indexOf(id) + 1;
  addNode(state, id, number, column, true);
  addSemanticNode(state, id, number, column);
  return true;
}

function parseStateStart(state: ParserState, line: string, number: number): boolean {
  const transition = line.match(/^\s*\[\*\]\s*-->\s*([\w.-]+)/);
  if (!transition) return false;
  const target = transition[1];
  const column = line.indexOf(target) + 1;
  const markerColumn = line.indexOf("[*]") + 1;
  addNode(state, target, number, column);
  addSemanticNode(state, target, number, column);
  state.startNodeIds.push(target);
  state.edges.push({ from: "*", to: target, label: null, line: number, column: markerColumn });
  state.semanticEdges.push({ from: "*", to: target, label: null, line: number, column: markerColumn });
  return true;
}

function parseSpecialDeclaration(state: ParserState, line: string, number: number): boolean {
  if (state.type === "sequence" && parseSequenceDeclaration(state, line, number)) return true;
  if (state.type === "class" && parseClassDeclaration(state, line, number)) return true;
  return state.type === "state" && parseStateStart(state, line, number);
}

function flowchartSyntaxError(line: string, number: number): ParserError | null {
  const relation = "(?:-->|---|--|\\.\\.>|==>)";
  if (new RegExp(`${relation}\\s*${relation}`).test(line)) {
    return { code: "syntax_error", message: "Malformed flowchart relation", line: number, column: 1 };
  }
  if (new RegExp(`${relation}\\s*$`).test(line)) {
    return { code: "syntax_error", message: "Flowchart relation is missing a destination node", line: number, column: 1 };
  }
  return null;
}

function relationMatch(type: DiagramType, line: string): RegExpMatchArray | null {
  const relation = type === "sequence"
    ? /^\s*([\w.-]+).*?(?:--?>|-->>|->>)\s*([\w.-]+)/
    : /^\s*([^\s\[{(<-]+).*?(?:-->|---|--|\.\.>|==>)\s*([^\s\[{(<:]+)/;
  return line.match(relation);
}

function flowchartSemantics(line: string, to: string, toColumn: number, from: string, fromColumn: number): RelationSemantics {
  let target = to;
  let targetColumn = destinationColumn(line, fromColumn, from, "flowchart");
  let label: string | null = null;
  if (to.startsWith("|")) {
    const firstPipe = line.indexOf("|", toColumn - 1);
    const lastPipe = line.indexOf("|", firstPipe + 1);
    if (lastPipe > firstPipe) {
      label = line.slice(firstPipe + 1, lastPipe).trim() || null;
      const candidate = line.slice(lastPipe + 1).match(/^\s*([^\s\[{(<:|]+)/)?.[1];
      if (candidate) {
        target = candidate;
        targetColumn = destinationColumn(line, fromColumn, from, "flowchart");
      }
    }
  }
  return { target, targetColumn, label };
}

function sequenceSemantics(line: string, to: string, from: string, fromColumn: number): RelationSemantics {
  const colon = line.indexOf(":");
  const label = colon >= 0 ? line.slice(colon + 1).trim() : "";
  return { target: to, targetColumn: destinationColumn(line, fromColumn, from, "sequence"), label: label || null };
}

function erSemantics(line: string, from: string, fromColumn: number, to: string): RelationSemantics {
  const tailStart = fromColumn - 1 + from.length;
  const tail = line.slice(tailStart);
  const relation = /(?:--|\.\.)/.exec(tail);
  let target = to;
  let targetColumn = destinationColumn(line, fromColumn, from, "er");
  if (relation) {
    const targetOffset = tailStart + relation.index + relation[0].length;
    const candidate = line.slice(targetOffset).match(/^\s*(?:[|o{}]+\s*)?([A-Za-z0-9_.-]+)/)?.[1];
    if (candidate) {
      target = candidate;
      targetColumn = line.indexOf(candidate, targetOffset) + 1;
    }
  }
  const label = line.match(/:\s*([^:]+?)\s*$/)?.[1]?.trim() ?? null;
  return { target, targetColumn, label };
}

function relationSemantics(state: ParserState, line: string, from: string, fromColumn: number, to: string, toColumn: number): RelationSemantics {
  if (state.type === "sequence") return sequenceSemantics(line, to, from, fromColumn);
  if (state.type === "er") return erSemantics(line, from, fromColumn, to);
  return flowchartSemantics(line, to, toColumn, from, fromColumn);
}

function parseRelation(state: ParserState, line: string, number: number): boolean {
  const match = relationMatch(state.type, line);
  if (!match) return false;
  const from = match[1].trim();
  const to = match[2].trim();
  const fromColumn = line.indexOf(from) + 1;
  const toColumn = line.indexOf(to, fromColumn) + 1;
  addNode(state, from, number, fromColumn, isExplicitDeclaration(line, from, fromColumn));
  addNode(state, to, number, toColumn, isExplicitDeclaration(line, to, toColumn));
  const semantics = relationSemantics(state, line, from, fromColumn, to, toColumn);
  addSemanticNode(state, from, number, fromColumn, extractLabel(line, from, fromColumn));
  addSemanticNode(state, semantics.target, number, semantics.targetColumn, extractLabel(line, semantics.target, semantics.targetColumn));
  state.edges.push({ from, to: state.type === "er" ? semantics.target : to, label: null, line: number, column: fromColumn });
  state.semanticEdges.push({ from, to: semantics.target, label: semantics.label, line: number, column: fromColumn });
  return true;
}

function parseStandaloneFlowchartNode(state: ParserState, line: string, number: number): void {
  const standalone = line.match(/^\s*([A-Za-z0-9_.-]+)\s*(?:\[|\(|\{|$)/);
  if (!standalone) return;
  const id = standalone[1];
  const column = line.indexOf(id) + 1;
  addNode(state, id, number, column, true);
  addSemanticNode(state, id, number, column, extractLabel(line, id, column));
}

export function parseLine(state: ParserState, line: string, number: number): ParserError | null {
  if (skipLine(line) || parseSpecialDeclaration(state, line, number)) return null;
  if (state.type === "flowchart") {
    const syntaxError = flowchartSyntaxError(line, number);
    if (syntaxError) return syntaxError;
  }
  if (parseRelation(state, line, number)) return null;
  if (state.type === "flowchart") parseStandaloneFlowchartNode(state, line, number);
  return null;
}
