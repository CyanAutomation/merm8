import { analyzeDiagram } from "../engine/analyze.js";
import { parseMermaid } from "../parser/parse.js";
import { semanticQuestions } from "./questions.js";
import { buildSemanticState, SemanticStateLimitError } from "./state.js";
import { requestJevDecisions, type JevClientOptions } from "./jev-client.js";
import {
  JevClientError,
  SemanticReviewError,
  type ChoiceAnswer,
  type SemanticReviewEnvironment,
  type SemanticReviewResponse,
} from "./types.js";

const BOOLEAN_NORMALIZATION_THRESHOLD = 0.5;

function choiceAnswer(answers: Record<string, unknown>, key: string): ChoiceAnswer {
  const answer = answers[key] as ChoiceAnswer | undefined;
  if (!answer || answer.type !== "choice") throw new Error(`missing JEV choice answer: ${key}`);
  return answer;
}

function booleanAnswer(answers: Record<string, unknown>, key: string): { value: boolean; probability: number } {
  const answer = answers[key] as { type?: string; noul?: number } | undefined;
  if (!answer || answer.type !== "noul" || typeof answer.noul !== "number") throw new Error(`missing JEV noul answer: ${key}`);
  // 0.5 only normalizes the returned probability into the boolean value in this API contract.
  return { value: answer.noul >= BOOLEAN_NORMALIZATION_THRESHOLD, probability: answer.noul };
}

function mapJevError(error: unknown): SemanticReviewError {
  if (!(error instanceof JevClientError)) {
    return new SemanticReviewError("jev_upstream_error", "Semantic review could not be completed", 502);
  }
  if (error.code === "credentials") {
    return new SemanticReviewError("missing_openrouter_credentials", "OpenRouter credentials are not configured", 503);
  }
  if (error.code === "configuration") {
    return new SemanticReviewError("semantic_review_unconfigured", "Semantic review is not configured", 503);
  }
  if (error.code === "timeout") {
    return new SemanticReviewError("jev_timeout", "Semantic review provider timed out", 504);
  }
  if (error.code === "invalid_response") {
    return new SemanticReviewError("jev_invalid_response", "Semantic review provider returned an invalid response", 502);
  }
  const retryableHttp = error.code === "http" && ((error.status ?? 0) === 429 || (error.status ?? 0) >= 500);
  const unavailable = error.code === "network" || retryableHttp;
  return new SemanticReviewError("jev_upstream_error", "Semantic review provider is unavailable", unavailable ? 503 : 502);
}

export interface SemanticReviewOptions extends Omit<JevClientOptions, "apiKey" | "model"> {}

export async function reviewMermaidSemantics(
  code: string,
  env: SemanticReviewEnvironment,
  options: SemanticReviewOptions = {},
): Promise<SemanticReviewResponse> {
  const parsed = parseMermaid(code);
  if (!parsed.diagram) {
    throw new SemanticReviewError("invalid_mermaid", "Mermaid source could not be parsed", 400, parsed.error);
  }
  if ((parsed.diagram.semantic?.nodes ?? parsed.diagram.nodes).length === 0) {
    throw new SemanticReviewError("semantic_state_unavailable", "The parsed diagram contains no nodes for semantic review", 422);
  }

  const analysis = analyzeDiagram(parsed.diagram);
  let state;
  try {
    state = buildSemanticState(parsed.diagram, analysis);
  } catch (error) {
    if (error instanceof SemanticStateLimitError) {
      throw new SemanticReviewError("semantic_state_too_large", error.message, 413);
    }
    throw error;
  }

  let decisions;
  try {
    decisions = await requestJevDecisions(state, semanticQuestions, {
      ...options,
      apiKey: env.OPENROUTER_API_KEY,
      model: env.MERM8_DECISION_MODEL,
    });
  } catch (error) {
    throw mapJevError(error);
  }

  const answers = decisions.answers as Record<string, unknown>;
  const purpose = choiceAnswer(answers, "diagram-purpose");
  const reviewPriority = choiceAnswer(answers, "review-priority");
  return {
    valid: true,
    "diagram-type": analysis["diagram-type"]!,
    "lint-supported": analysis["lint-supported"]!,
    issues: analysis.issues,
    "semantic-review": {
      purpose: { value: purpose.choice, confidence: purpose.confidence },
      "label-clarity": booleanAnswer(answers, "label-clarity"),
      "branch-clarity": booleanAnswer(answers, "branch-clarity"),
      "abstraction-consistency": booleanAnswer(answers, "abstraction-consistency"),
      ambiguity: booleanAnswer(answers, "ambiguity"),
      "review-priority": { value: reviewPriority.choice, confidence: reviewPriority.confidence },
    },
    meta: { source: "jev", model: decisions.model },
  };
}
