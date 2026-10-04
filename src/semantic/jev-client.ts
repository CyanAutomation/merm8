import type { SemanticState } from "./types.js";
import { JevClientError, type JevAnswer, type JevDecisionResult, type JevQuestion } from "./types.js";

export const JEV_DECISIONS_URL = "https://openrouter.ai/api/alpha/decisions";
export const DEFAULT_DECISION_MODEL = "~typesafe/jev-latest";
export const DEFAULT_JEV_TIMEOUT_MS = 15_000;
const DEFAULT_MAX_RETRIES = 1;
const RETRYABLE_HTTP_STATUSES = new Set([429, 503, 529]);

export interface JevClientOptions {
  apiKey?: string;
  model?: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
  maxRetries?: number;
  retryDelayMs?: number;
  log?: (event: string, metadata: Record<string, string | number>) => void;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function probability(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1;
}

function isProbabilityDistribution(value: unknown, keys: string[]): value is Record<string, number> {
  if (!isRecord(value)) return false;
  const actualKeys = Object.keys(value);
  const values = Object.values(value);
  if (actualKeys.length !== keys.length || !keys.every((key) => Object.hasOwn(value, key)) || !values.every(probability)) return false;
  const total = values.reduce<number>((sum, item) => sum + Number(item), 0);
  return Math.abs(total - 1) <= 0.03;
}

function validAnswer(question: JevQuestion, answer: unknown): answer is JevAnswer {
  if (!isRecord(answer)) return false;
  if (question.type === "noul") return answer.type === "noul" && probability(answer.noul);
  const choices = Object.keys(question.criteria);
  return answer.type === "choice" &&
    typeof answer.choice === "string" && choices.includes(answer.choice) &&
    isProbabilityDistribution(answer.probabilities, choices) &&
    probability(answer.confidence);
}

function resolvedModel(value: unknown, requested: string, apiKey: string): string {
  if (typeof value !== "string") return requested;
  const candidate = value.trim();
  return !candidate.includes(apiKey) && /^[a-zA-Z0-9._:/@~-]{1,128}$/.test(candidate) ? candidate : requested;
}

function validatePayload(value: unknown, questions: Record<string, JevQuestion>, requestedModel: string, apiKey: string): JevDecisionResult {
  if (!isRecord(value)) {
    throw new JevClientError("invalid_response", "JEV response did not contain typed answers");
  }
  const answers = isRecord(value.answers) ? value.answers : undefined;
  if (!answers) throw new JevClientError("invalid_response", "JEV response did not contain typed answers");
  const expected = Object.keys(questions);
  const answerKeys = Object.keys(answers);
  if (answerKeys.length !== expected.length || !expected.every((key) => validAnswer(questions[key], answers[key]))) {
    throw new JevClientError("invalid_response", "JEV response did not match the requested typed answers");
  }
  return { model: resolvedModel(value.model, requestedModel, apiKey), answers: answers as Record<string, JevAnswer> };
}

function abortError(error: unknown): boolean {
  return error instanceof Error && (error.name === "AbortError" || error.name === "TimeoutError");
}

function retryable(error: JevClientError): boolean {
  return error.code === "timeout" || error.code === "network" ||
    (error.code === "http" && error.status !== undefined && RETRYABLE_HTTP_STATUSES.has(error.status));
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function emitLog(options: JevClientOptions, metadata: Record<string, string | number>): void {
  try {
    (options.log ?? ((event, values) => console.info(event, values)))("semantic_review_jev", metadata);
  } catch {
    // Logging must never change the semantic review result.
  }
}

async function requestAttempt(
  state: SemanticState,
  questions: Record<string, JevQuestion>,
  model: string,
  apiKey: string,
  options: JevClientOptions,
  timeoutMs: number,
): Promise<JevDecisionResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await (options.fetchImpl ?? fetch)(JEV_DECISIONS_URL, {
      method: "POST",
      headers: { authorization: `Bearer ${apiKey}`, "content-type": "application/json" },
      body: JSON.stringify({ model, state, questions }),
      signal: controller.signal,
    });
    if (!response.ok) throw new JevClientError("http", `JEV returned HTTP ${response.status}`, response.status);
    let payload: unknown;
    try {
      payload = await response.json();
    } catch (error) {
      if (controller.signal.aborted || abortError(error)) throw new JevClientError("timeout", `JEV request timed out after ${timeoutMs}ms`);
      throw new JevClientError("invalid_response", "JEV returned invalid JSON");
    }
    return validatePayload(payload, questions, model, apiKey);
  } catch (error) {
    if (error instanceof JevClientError) throw error;
    if (controller.signal.aborted || abortError(error)) throw new JevClientError("timeout", `JEV request timed out after ${timeoutMs}ms`);
    throw new JevClientError("network", "JEV network request failed");
  } finally {
    clearTimeout(timer);
  }
}

export async function requestJevDecisions(
  state: SemanticState,
  questions: Record<string, JevQuestion>,
  options: JevClientOptions = {},
): Promise<JevDecisionResult> {
  const startedAt = Date.now();
  const configuredModel = options.model?.trim();
  const model = configuredModel || DEFAULT_DECISION_MODEL;
  const timeoutMs = Number.isInteger(options.timeoutMs) && (options.timeoutMs ?? 0) > 0
    ? options.timeoutMs!
    : DEFAULT_JEV_TIMEOUT_MS;
  const maxRetries = Number.isInteger(options.maxRetries) && (options.maxRetries ?? 0) >= 0
    ? Math.min(options.maxRetries!, 2)
    : DEFAULT_MAX_RETRIES;
  const retryDelayMs = Number.isInteger(options.retryDelayMs) && (options.retryDelayMs ?? -1) >= 0
    ? options.retryDelayMs!
    : 100;
  let retryCount = 0;
  let outcome = "error";
  let resolved = "unresolved";

  try {
    if (!/^[-a-zA-Z0-9._:/@~]{1,128}$/.test(model)) throw new JevClientError("configuration", "MERM8_DECISION_MODEL is invalid");
    const apiKey = options.apiKey?.trim();
    if (!apiKey) throw new JevClientError("credentials", "OPENROUTER_API_KEY is not configured");

    for (let attempt = 0; ; attempt += 1) {
      try {
        const result = await requestAttempt(state, questions, model, apiKey, options, timeoutMs);
        resolved = result.model;
        outcome = "success";
        return result;
      } catch (error) {
        const clientError = error instanceof JevClientError
          ? error
          : new JevClientError("network", "JEV network request failed");
        if (attempt >= maxRetries || !retryable(clientError)) throw clientError;
        retryCount += 1;
        await wait(retryDelayMs * 2 ** retryCount);
      }
    }
  } catch (error) {
    outcome = error instanceof JevClientError ? error.code : "error";
    throw error;
  } finally {
    emitLog(options, {
      operation: "semantic-review",
      outcome,
      "requested-model": model,
      "resolved-model": resolved,
      "question-count": Object.keys(questions).length,
      "duration-ms": Math.max(0, Date.now() - startedAt),
      "retry-count": retryCount,
    });
  }
}
