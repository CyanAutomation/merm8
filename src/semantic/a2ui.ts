import type { Analysis } from "../domain/types.js";
import type { A2uiComponent, A2uiMessage, SemanticReview } from "./types.js";

const A2UI_BASIC_CATALOG_ID = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json" as const;
const A2UI_DIAGRAM_REVIEW_SURFACE_ID = "diagramReview" as const;

export type A2uiPresentationOrder = "summary-first" | "checks-first" | "clarity-first";
export type A2uiPresentationDensity = "concise" | "detailed";

const MAX_ISSUES_CONCISE = 3;
const MAX_ISSUES_DETAILED = 8;
const MAX_DISPLAY_TEXT_LENGTH = 220;

function column(id: string, children: string[]): A2uiComponent {
  return { id, component: "Column", children };
}

function card(id: string, child: string): A2uiComponent {
  return { id, component: "Card", child };
}

function text(id: string, path: string, variant: "caption" | "body" = "caption"): A2uiComponent {
  return { id, component: "Text", text: { "@path": path }, variant };
}

function displayText(value: string): string {
  return value.replace(/[\u0000-\u001f\u007f]/g, " ").replace(/\s+/g, " ").trim().slice(0, MAX_DISPLAY_TEXT_LENGTH);
}

function percent(value: number): string {
  return `${Math.round(Math.min(1, Math.max(0, value)) * 100)}%`;
}

function purposeLabel(value: string): string {
  const labels: Record<string, string> = {
    process: "Process",
    "decision-tree": "Decision tree",
    architecture: "Architecture",
    "data-model": "Data model",
    interaction: "Interaction",
    "state-machine": "State machine",
    other: "Other or unclear",
  };
  return labels[value] ?? "Other or unclear";
}

function priorityLabel(value: string): string {
  const labels: Record<string, string> = { none: "None", low: "Low", medium: "Medium", high: "High" };
  return labels[value] ?? "Unspecified";
}

function clarity(value: boolean): string {
  return value ? "Clear" : "Needs clarification";
}

function issueDetails(analysis: Analysis, density: A2uiPresentationDensity): { summary: string; details: string } {
  const limit = density === "detailed" ? MAX_ISSUES_DETAILED : MAX_ISSUES_CONCISE;
  const visible = analysis.issues.slice(0, limit);
  const total = analysis.issues.length;
  const summary = total === 0
    ? "No deterministic issues found."
    : `${total} deterministic issues${visible.length < total ? `; showing ${visible.length}` : ""}.`;
  const details = visible.map((issue) => {
    const line = typeof issue.line === "number" ? ` · line ${issue.line}` : "";
    return displayText(`${issue.severity.toUpperCase()} · ${issue["rule-id"]}${line}: ${issue.message}`);
  }).join("\n");
  return { summary, details: details || "No issue details to display." };
}

export function buildSemanticReviewA2ui(
  analysis: Analysis,
  review: SemanticReview,
  order: A2uiPresentationOrder,
  density: A2uiPresentationDensity,
): A2uiMessage[] {
  const summaryChildren = ["summaryTitle", "purposeText", "priorityText"];
  const semanticChildren = density === "detailed"
    ? ["semanticTitle", "labelsText", "branchesText", "abstractionText", "ambiguityText"]
    : ["semanticTitle", "semanticSummary"];
  const rootChildren: Record<A2uiPresentationOrder, string[]> = {
    "summary-first": ["summaryCard", "semanticCard", "issuesCard"],
    "checks-first": ["issuesCard", "summaryCard", "semanticCard"],
    "clarity-first": ["semanticCard", "summaryCard", "issuesCard"],
  };
  const { summary: issuesSummary, details: issues } = issueDetails(analysis, density);
  const semanticSummary = [
    `Labels: ${clarity(review["label-clarity"].value)}`,
    `Branches: ${clarity(review["branch-clarity"].value)}`,
    `Abstraction: ${review["abstraction-consistency"].value ? "Consistent" : "Needs review"}`,
    `Ambiguity: ${review.ambiguity.value ? "Detected" : "Not detected"}`,
  ].join(" · ");
  const semanticData = density === "detailed"
    ? {
      title: "Semantic signals",
      labels: `Labels: ${clarity(review["label-clarity"].value)} · ${percent(review["label-clarity"].probability)} estimated likelihood`,
      branches: `Branches: ${clarity(review["branch-clarity"].value)} · ${percent(review["branch-clarity"].probability)} estimated likelihood`,
      abstraction: `Abstraction: ${review["abstraction-consistency"].value ? "Consistent" : "Needs review"} · ${percent(review["abstraction-consistency"].probability)} estimated likelihood`,
      ambiguity: `Ambiguity: ${review.ambiguity.value ? "Detected" : "Not detected"} · ${percent(review.ambiguity.probability)} estimated likelihood`,
    }
    : { title: "Semantic signals", summary: semanticSummary };

  const components: A2uiComponent[] = [
    column("root", rootChildren[order]),
    card("summaryCard", "summaryContent"),
    column("summaryContent", summaryChildren),
    text("summaryTitle", "/summary/title", "body"),
    text("purposeText", "/summary/purpose"),
    text("priorityText", "/summary/priority"),
    card("semanticCard", "semanticContent"),
    column("semanticContent", semanticChildren),
    text("semanticTitle", "/semantic/title", "body"),
    ...(density === "detailed"
      ? [
        text("labelsText", "/semantic/labels"),
        text("branchesText", "/semantic/branches"),
        text("abstractionText", "/semantic/abstraction"),
        text("ambiguityText", "/semantic/ambiguity"),
      ]
      : [text("semanticSummary", "/semantic/summary")]),
    card("issuesCard", "issuesContent"),
    column("issuesContent", ["issuesTitle", "issuesSummary", "issuesDetails"]),
    text("issuesTitle", "/issues/title", "body"),
    text("issuesSummary", "/issues/summary"),
    text("issuesDetails", "/issues/details"),
  ];

  return [{
    version: "1.0",
    createSurface: {
      surfaceId: A2UI_DIAGRAM_REVIEW_SURFACE_ID,
      catalogId: A2UI_BASIC_CATALOG_ID,
      sendDataModel: false,
      components,
      dataModel: {
        presentation: { order, density },
        summary: {
          title: "Diagram review",
          purpose: `Purpose: ${purposeLabel(review.purpose.value)} · ${percent(review.purpose.confidence)} model confidence`,
          priority: `Review priority: ${priorityLabel(review["review-priority"].value)} · ${percent(review["review-priority"].confidence)} model confidence`,
        },
        semantic: semanticData,
        issues: { title: "Deterministic checks", summary: issuesSummary, details: issues },
      },
    },
  }];
}
