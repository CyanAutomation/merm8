import type { Analysis, DiagramType, Edge, Issue, Node } from "../domain/types.js";

export interface NoulQuestion {
  type: "noul";
  instructions: string;
  criteria: { true: string; false: string };
}

export interface ChoiceQuestion {
  type: "choice";
  instructions: string;
  criteria: Record<string, string>;
}

export type JevQuestion = NoulQuestion | ChoiceQuestion;
export type JevQuestions = Record<string, JevQuestion>;

export interface SemanticState {
  diagram: {
    type: DiagramType;
    nodes: Array<Pick<Node, "id" | "label">>;
    edges: Array<Pick<Edge, "from" | "to" | "label">>;
  };
  structural: {
    valid: true;
    "issue-count": number;
    issues: Array<Pick<Issue, "rule-id" | "severity">>;
    "issues-truncated": boolean;
  };
  "has-branching": boolean;
}

export interface NoulAnswer { type: "noul"; noul: number }
export interface ChoiceAnswer {
  type: "choice";
  choice: string;
  probabilities: Record<string, number>;
  confidence: number;
}
export type JevAnswer = NoulAnswer | ChoiceAnswer;
export interface JevDecisionResult { model: string; answers: Record<string, JevAnswer> }

export interface SemanticReview {
  purpose: { value: string; confidence: number };
  "label-clarity": { value: boolean; probability: number };
  "branch-clarity": { value: boolean; probability: number };
  "abstraction-consistency": { value: boolean; probability: number };
  ambiguity: { value: boolean; probability: number };
  "review-priority": { value: string; confidence: number };
}

export interface SemanticReviewResponse extends Analysis {
  "semantic-review": SemanticReview;
  meta: { source: "jev"; model: string };
}

export type JevErrorCode = "configuration" | "credentials" | "timeout" | "http" | "invalid_response" | "network";

export class JevClientError extends Error {
  readonly code: JevErrorCode;
  readonly status?: number;

  constructor(code: JevErrorCode, message: string, status?: number) {
    super(message);
    this.name = "JevClientError";
    this.code = code;
    this.status = status;
  }
}

export type SemanticReviewErrorCode = "invalid_mermaid" | "semantic_state_unavailable" | "semantic_state_too_large" | "semantic_review_unconfigured" | "missing_openrouter_credentials" | "jev_timeout" | "jev_invalid_response" | "jev_upstream_error";

export class SemanticReviewError extends Error {
  readonly code: SemanticReviewErrorCode;
  readonly status: number;
  readonly details?: unknown;

  constructor(code: SemanticReviewErrorCode, message: string, status: number, details?: unknown) {
    super(message);
    this.name = "SemanticReviewError";
    this.code = code;
    this.status = status;
    this.details = details;
  }
}

export interface SemanticReviewEnvironment {
  OPENROUTER_API_KEY?: string;
  MERM8_DECISION_MODEL?: string;
}
