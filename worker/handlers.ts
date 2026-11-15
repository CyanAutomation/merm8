import { analyzeMermaid } from "../src/engine/analyze.js";
import { mcpHandler } from "../src/mcp/server.js";
import { reviewMermaidSemantics } from "../src/semantic/review.js";
import { SemanticReviewError } from "../src/semantic/types.js";
import { allowed, isRecord, json, ANALYZE_RETRY_AFTER_SECONDS, MAX_ANALYZE_BODY_BYTES, SEMANTIC_REVIEW_RETRY_AFTER_SECONDS, type Env } from "./shared.js";
import { ruleConfig, validateRuleConfig } from "./config.js";
import { toSarif } from "./sarif.js";
import { canonicalJson } from "../src/cache/canonical-json.js";
import { TtlLruCache } from "../src/cache/ttl-lru.js";

const analysisCache = new TtlLruCache<ReturnType<typeof analyzeMermaid>>(128, 60_000, 4 * 1024 * 1024);

type RateLimitOptions = {
  limiter: Env["ANALYZE_LIMITER"];
  namespace: string;
  service: string;
  unavailableMessage: string;
  limitedMessage: string;
  retryAfterSeconds: number;
};

function rateLimitUnavailable(message: string, retryAfterSeconds: number): Response {
  return json(
    { error: { code: "rate_limit_unavailable", message } },
    503,
    { "retry-after": String(retryAfterSeconds) },
  );
}

async function rateLimitResponse(
  request: Request,
  options: RateLimitOptions,
): Promise<Response | undefined> {
  const { limiter, namespace, service, unavailableMessage, limitedMessage, retryAfterSeconds } = options;
  if (!limiter) {
    console.error(`${service} rate limiter is not configured`);
    return rateLimitUnavailable(unavailableMessage, retryAfterSeconds);
  }

  const clientIp = request.headers.get("cf-connecting-ip")?.trim();
  if (!clientIp) {
    console.error(`${service} client IP is unavailable`);
    return rateLimitUnavailable(unavailableMessage, retryAfterSeconds);
  }

  try {
    const { success } = await limiter.limit({ key: `${namespace}:${clientIp}` });
    if (success) return undefined;
  } catch {
    console.error(`${service} rate limiter failed`);
    return rateLimitUnavailable(unavailableMessage, retryAfterSeconds);
  }

  return json(
    { error: { code: "rate_limited", message: limitedMessage } },
    429,
    { "retry-after": String(retryAfterSeconds) },
  );
}

async function parseAnalyzeBody(request: Request): Promise<{ body?: { code?: unknown; config?: unknown }; response?: Response }> {
  const contentLength = Number(request.headers.get("content-length"));
  if (Number.isFinite(contentLength) && contentLength > MAX_ANALYZE_BODY_BYTES) {
    return { response: json({ error: { code: "payload_too_large", message: "request body must not exceed 1 MiB" } }, 413) };
  }

  const bytes = await request.arrayBuffer();
  if (bytes.byteLength > MAX_ANALYZE_BODY_BYTES) {
    return { response: json({ error: { code: "payload_too_large", message: "request body must not exceed 1 MiB" } }, 413) };
  }

  try {
    return { body: JSON.parse(new TextDecoder().decode(bytes)) as { code?: unknown; config?: unknown } };
  } catch (error) {
    console.error("JSON parse error:", error);
    return { response: json({ error: { code: "invalid_json", message: "invalid JSON body" } }, 400) };
  }
}

function validateCodeBody(
  body: unknown,
  invalidObjectMessage: string,
): { code: string; config?: unknown } | Response {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return json({ error: { code: "invalid_request", message: invalidObjectMessage } }, 400);
  }
  const record = body as { code?: unknown; config?: unknown };
  if (typeof record.code !== "string") {
    return json({ error: { code: "invalid_request", message: "code must be a string" } }, 400);
  }
  return { code: record.code, config: record.config };
}

async function semanticReviewInput(request: Request, env: Env): Promise<{ code?: string; response?: Response }> {
  const rateLimitError = await rateLimitResponse(request, {
    limiter: env.SEMANTIC_REVIEW_LIMITER,
    namespace: "semantic-review",
    service: "Semantic review",
    unavailableMessage: "Semantic review is temporarily unavailable",
    limitedMessage: "Semantic review limit exceeded; try again later",
    retryAfterSeconds: SEMANTIC_REVIEW_RETRY_AFTER_SECONDS,
  });
  if (rateLimitError) return { response: rateLimitError };

  const parsed = await parseAnalyzeBody(request);
  if (parsed.response) return { response: parsed.response };
  const body = validateCodeBody(parsed.body, "code must be a string");
  if (body instanceof Response) return { response: body };
  return { code: body.code };
}

function semanticReviewFailure(error: unknown): Response {
  if (error instanceof SemanticReviewError) {
    return json({ error: { code: error.code, message: error.message, ...(error.details ? { details: error.details } : {}) } }, error.status);
  }
  console.error("Semantic review failed");
  return json({ error: { code: "semantic_review_failed", message: "Semantic review could not be completed" } }, 500);
}

export async function handleSemanticReview(request: Request, env: Env): Promise<Response> {
  const input = await semanticReviewInput(request, env);
  if (input.response) return input.response;
  try {
    return json(await reviewMermaidSemantics(input.code!, env));
  } catch (error) {
    return semanticReviewFailure(error);
  }
}

async function analysisInput(request: Request, env: Env): Promise<{ code?: string; config?: unknown; response?: Response }> {
  const rateLimitError = await rateLimitResponse(request, {
    limiter: env.ANALYZE_LIMITER,
    namespace: "analysis",
    service: "Analysis",
    unavailableMessage: "Analysis is temporarily unavailable",
    limitedMessage: "Analysis limit exceeded; try again later",
    retryAfterSeconds: ANALYZE_RETRY_AFTER_SECONDS,
  });
  if (rateLimitError) return { response: rateLimitError };

  const parsed = await parseAnalyzeBody(request);
  if (parsed.response) return { response: parsed.response };
  const body = validateCodeBody(parsed.body, "request body must be an object");
  if (body instanceof Response) return { response: body };
  const configError = validateRuleConfig(body.config);
  if (configError) return { response: json({ error: configError }, 400) };
  return { code: body.code, config: body.config };
}

export async function handleAnalysis(request: Request, sarif: boolean, env: Env): Promise<Response> {
  const input = await analysisInput(request, env);
  if (input.response) return input.response;
  const code = input.code!;
  const config = ruleConfig(input.config);
  const cacheKey = canonicalJson({ code, config });
  let analysis = analysisCache.get(cacheKey);
  const cacheHit = !!analysis;
  if (!analysis) {
    analysis = analyzeMermaid(code, config);
    analysisCache.set(cacheKey, analysis);
  }
  if (!sarif) return json(analysis, 200, { "cache-status": cacheHit ? "merm8; hit" : "merm8; fwd=uri-miss" });
  if (!analysis.valid) {
    return json({ error: { code: analysis.error?.code ?? "syntax_error", message: analysis.error?.message ?? "Mermaid source could not be parsed", details: analysis.error } }, 400, { "cache-status": cacheHit ? "merm8; hit" : "merm8; fwd=uri-miss" });
  }
  return new Response(JSON.stringify(toSarif(code, analysis)), { headers: { "content-type": "application/sarif+json; charset=utf-8", "cache-status": cacheHit ? "merm8; hit" : "merm8; fwd=uri-miss" } });
}

export async function handleMcp(request: Request, env: Env): Promise<Response> {
  if (!allowed(request, env.API_KEY)) {
    return json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" });
  }
  return mcpHandler.fetch(request);
}
