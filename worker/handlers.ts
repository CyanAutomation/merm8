import { analyzeMermaid } from "../src/engine/analyze.js";
import { mcpHandler } from "../src/mcp/server.js";
import { reviewMermaidSemantics } from "../src/semantic/review.js";
import { SemanticReviewError } from "../src/semantic/types.js";
import { allowed, isRecord, json, MAX_ANALYZE_BODY_BYTES, type Env } from "./shared.js";
import { ruleConfig, validateRuleConfig } from "./config.js";
import { toSarif } from "./sarif.js";

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

async function semanticReviewInput(request: Request, env: Env): Promise<{ code?: string; response?: Response }> {
  if (!allowed(request, env.API_KEY)) {
    return { response: json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" }) };
  }
  const parsed = await parseAnalyzeBody(request);
  if (parsed.response) return { response: parsed.response };
  const body = parsed.body;
  if (!body || typeof body !== "object" || Array.isArray(body) || typeof body.code !== "string") {
    return { response: json({ error: { code: "invalid_request", message: "code must be a string" } }, 400) };
  }
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

async function analysisInput(request: Request): Promise<{ code?: string; config?: unknown; response?: Response }> {
  const parsed = await parseAnalyzeBody(request);
  if (parsed.response) return { response: parsed.response };
  const body = parsed.body!;
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return { response: json({ error: { code: "invalid_request", message: "request body must be an object" } }, 400) };
  }
  if (typeof body.code !== "string") {
    return { response: json({ error: { code: "invalid_request", message: "code must be a string" } }, 400) };
  }
  const configError = validateRuleConfig(body.config);
  if (configError) return { response: json({ error: configError }, 400) };
  return { code: body.code, config: body.config };
}

export async function handleAnalysis(request: Request, sarif: boolean): Promise<Response> {
  const input = await analysisInput(request);
  if (input.response) return input.response;
  const code = input.code!;
  const analysis = analyzeMermaid(code, ruleConfig(input.config));
  if (!sarif) return json(analysis);
  if (!analysis.valid) {
    return json({ error: { code: analysis.error?.code ?? "syntax_error", message: analysis.error?.message ?? "Mermaid source could not be parsed", details: analysis.error } }, 400);
  }
  return new Response(JSON.stringify(toSarif(code, analysis)), { headers: { "content-type": "application/sarif+json; charset=utf-8" } });
}

export async function handleMcp(request: Request, env: Env): Promise<Response> {
  if (!allowed(request, env.API_KEY)) {
    return json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" });
  }
  return mcpHandler.fetch(request);
}
