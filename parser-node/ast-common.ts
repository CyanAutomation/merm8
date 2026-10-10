import type { RawRecord, SourceLocation, Suppression } from "./ast-types.ts";

function asRecord(value: unknown): RawRecord | null {
  return typeof value === "object" && value !== null ? value as RawRecord : null;
}

export function findNodeLocation(lines: string[], id: string): SourceLocation | null {
  const escaped = escapeRegExp(id);
  const patterns = [
    new RegExp(`(^|\\s)${escaped}(?=\\s*[\\[({])`),
    new RegExp(`(^|\\s)${escaped}(?=\\s*[-.=xo]+>)`),
    new RegExp(`(^|\\s)${escaped}(?=\\s*$)`),
  ];

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    for (const pattern of patterns) {
      const match = line.match(pattern);
      if (match) {
        const start = (match.index ?? 0) + match[1].length;
        return { line: i + 1, column: start + 1 };
      }
    }
  }

  return null;
}

export function findEdgeLocation(lines: string[], from: string, to: string): SourceLocation | null {
  if (!from || !to) return null;

  const escapedFrom = escapeRegExp(from);
  const escapedTo = escapeRegExp(to);
  const fromPattern = new RegExp(`(^|\\s)${escapedFrom}(?=\\s*(?:[\\[({]|[-.=xo]+>))`);
  const toPattern = new RegExp(`(^|\\s)${escapedTo}(?=\\s*(?:[\\[({]|$))`);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const arrowIndex = line.search(/[-.=xo]+>/);
    if (arrowIndex < 0) continue;

    const fromMatch = line.match(fromPattern);
    if (!fromMatch) continue;
    const fromIndex = (fromMatch.index ?? 0) + fromMatch[1].length;
    if (fromIndex > arrowIndex) continue;

    const toMatch = line.slice(arrowIndex).match(toPattern);
    if (toMatch) return { line: i + 1, column: fromIndex + 1 };
  }

  return null;
}

function escapeRegExp(value: string): string {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

export function normalizeFlowchartDirection(dir: unknown): string {
  const normalized = String(dir || "").trim().toUpperCase();
  if (normalized === "TB") return "TD";
  return ["TD", "LR", "RL", "BT"].includes(normalized) ? normalized : "TD";
}

export function normalizeNodeID(id: unknown): string {
  // Preserve Mermaid's canonical node identity (case-sensitive), while trimming
  // incidental surrounding whitespace from parser/runtime values.
  return String(id).trim();
}

export function extractSuppressions(source: string): Suppression[] {
  const suppressions: Suppression[] = [];
  const lines = source.split(/\r?\n/);

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    const disableNextLineMatch = line.match(
      /^%%\s*merm8-(?:disable|ignore)-next-line\s+(all|[a-z0-9-]+)\s*$/i,
    );
    if (disableNextLineMatch) {
      suppressions.push({
        ruleId: disableNextLineMatch[1].toLowerCase(),
        scope: "next-line",
        line: i + 1,
        targetLine: i + 2,
      });
      continue;
    }

    const disableMatch = line.match(/^%%\s*merm8-(?:disable|ignore)\s+(all|[a-z0-9-]+)\s*$/i);
    if (disableMatch) {
      suppressions.push({
        ruleId: disableMatch[1].toLowerCase(),
        scope: "file",
        line: i + 1,
        targetLine: i + 1,
      });
    }
  }

  return suppressions;
}

export function extractLabel(vertex: unknown): string {
  if (!vertex) return "";
  const record = asRecord(vertex);
  if (!record) return "";
  if (typeof record.text === "string") return record.text;
  if (typeof record.label === "string") return record.label;
  const text = asRecord(record.text);
  if (typeof text?.label === "string") return text.label;
  return "";
}
