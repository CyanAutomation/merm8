import { analyzeMermaid } from "../src/engine/analyze.js";
import { mcpHandler } from "../src/mcp/server.js";
import { reviewMermaidSemantics } from "../src/semantic/review.js";
import { SemanticReviewError } from "../src/semantic/types.js";
import { workerOpenApi } from "./openapi.js";

export interface Env {
  API_KEY?: string;
  OPENROUTER_API_KEY?: string;
  MERM8_DECISION_MODEL?: string;
  REST_ALLOWED_ORIGINS?: string;
  BUILD_VERSION?: string;
  BUILD_SHA?: string;
}
const json = (body: unknown, status = 200, headers: HeadersInit = {}) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json; charset=utf-8", ...headers } });
const allowed = (request: Request, value?: string) => !!value && (request.headers.get("authorization") === `Bearer ${value}` || request.headers.get("x-api-key") === value);
const MAX_ANALYZE_BODY_BYTES = 1024 * 1024;
const ruleMetadata = [
  { id: "max-fanout", description: "Limit the number of outgoing connections from a node.", severity: "warning", "default-config": { limit: 5 } },
  { id: "no-cycles", description: "Disallow cycles in flowcharts.", severity: "error" },
  { id: "no-disconnected-nodes", description: "Disallow nodes that have no connections.", severity: "error" },
  { id: "no-duplicate-node-ids", description: "Disallow repeated node IDs.", severity: "error" },
  { id: "no-undefined-actors", description: "Require sequence diagram actors to be declared.", severity: "error", "diagram-type": "sequence" },
  { id: "no-duplicate-classes", description: "Disallow repeated class declarations.", severity: "error", "diagram-type": "class" },
  { id: "no-self-referential", description: "Flag entity relationships that point back to the same entity.", severity: "warning", "diagram-type": "er" },
  { id: "no-unreachable-state", description: "Disallow states that cannot be reached from the initial state.", severity: "error", "diagram-type": "state" },
] as const;
const supportedRuleIds = new Set<string>(ruleMetadata.map(rule => rule.id));
const sarifLevel = (severity: string): string => severity === "error" ? "error" : severity === "warning" ? "warning" : "note";
type ConfigError = { code: string; message: string };

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function toSarif(code: string, analysis: ReturnType<typeof analyzeMermaid>) {
  const rules = new Map<string, { id: string; shortDescription: { text: string }; defaultConfiguration: { level: string } }>();
  for (const item of analysis.issues) {
    if (rules.has(item["rule-id"])) continue;
    const metadata = ruleMetadata.find(rule => rule.id === item["rule-id"]);
    rules.set(item["rule-id"], {
      id: item["rule-id"],
      shortDescription: { text: metadata?.description ?? item["rule-id"] },
      defaultConfiguration: { level: sarifLevel(item.severity) },
    });
  }
  const results = analysis.issues.map(item => ({
    ruleId: item["rule-id"],
    level: sarifLevel(item.severity),
    message: { text: item.message },
    locations: [{ physicalLocation: {
      artifactLocation: { uri: "diagram.mmd" },
      ...(item.line ? { region: { startLine: item.line, ...(item.column ? { startColumn: item.column } : {}) } } : {}),
    } }],
    partialFingerprints: { issueFingerprint: item.fingerprint },
  }));
  return {
    $schema: "https://json.schemastore.org/sarif-2.1.0.json",
    version: "2.1.0",
    runs: [{
      tool: { driver: {
        name: "merm8",
        informationUri: "https://github.com/CyanAutomation/merm8",
        rules: [...rules.values()],
      } },
      ...(results.length ? { artifacts: [{ location: { uri: "diagram.mmd" }, contents: { text: code } }] } : {}),
      results,
      invocations: [{ executionSuccessful: analysis.valid, properties: { "request-uri": "/v1/analyze/sarif" } }],
    }],
  };
}

function corsHeaders(request: Request, env: Env): HeadersInit {
  const origin = request.headers.get("origin");
  const allowedOrigins = (env.REST_ALLOWED_ORIGINS ?? "").split(",").map(value => value.trim()).filter(Boolean);
  if (!origin || !allowedOrigins.includes(origin)) return {};
  return {
    "access-control-allow-origin": origin,
    "access-control-allow-methods": "GET, POST, OPTIONS",
    "access-control-allow-headers": "content-type, authorization, x-api-key",
    "access-control-max-age": "86400",
    vary: "Origin",
  };
}

function withCors(response: Response, headers: HeadersInit, env: Env): Response {
  const merged = new Headers(response.headers);
  merged.set("content-security-policy", "default-src 'none'; frame-ancestors 'none'");
  merged.set("referrer-policy", "no-referrer");
  merged.set("x-content-type-options", "nosniff");
  merged.set("x-frame-options", "DENY");
  merged.set("x-merm8-build", env.BUILD_SHA ?? env.BUILD_VERSION ?? "development");
  new Headers(headers).forEach((value, name) => merged.set(name, value));
  return new Response(response.body, { status: response.status, statusText: response.statusText, headers: merged });
}

function ruleConfig(value: unknown): Record<string, Record<string, unknown>> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const config = value as Record<string, unknown>;
  const nestedRules = config.rules;
  if (nestedRules && typeof nestedRules === "object" && !Array.isArray(nestedRules)) {
    return nestedRules as Record<string, Record<string, unknown>>;
  }
  return config as Record<string, Record<string, unknown>>;
}

function validateMaxFanoutLimit(ruleId: string, options: Record<string, unknown>): ConfigError | null {
  if (ruleId !== "max-fanout" || !("limit" in options)) return null;
  if (Number.isInteger(options.limit) && (options.limit as number) >= 0) return null;
  return { code: "invalid_option", message: "max-fanout.limit must be a non-negative integer" };
}

function validateRuleOptions(ruleId: string, value: unknown): ConfigError | null {
  if (!supportedRuleIds.has(ruleId)) return { code: "unknown_rule", message: `unknown rule: ${ruleId}` };
  if (!isRecord(value)) return { code: "invalid_option", message: `${ruleId} configuration must be an object` };
  if ("enabled" in value && typeof value.enabled !== "boolean") {
    return { code: "invalid_option", message: `${ruleId}.enabled must be a boolean` };
  }
  if ("severity" in value && !["error", "warning", "info"].includes(String(value.severity))) {
    return { code: "invalid_option", message: `${ruleId}.severity must be error, warning, or info` };
  }
  return validateMaxFanoutLimit(ruleId, value);
}

function validateRuleEntries(candidate: Record<string, unknown>, allowSchemaVersion: boolean): ConfigError | null {
  for (const [ruleId, options] of Object.entries(candidate)) {
    if (allowSchemaVersion && ruleId === "schema-version") continue;
    const error = validateRuleOptions(ruleId, options);
    if (error) return error;
  }
  return null;
}

function validateRuleConfig(value: unknown): ConfigError | null {
  if (value === undefined) return null;
  if (!isRecord(value)) {
    return { code: "invalid_request", message: "config must be an object" };
  }
  const nested = "rules" in value;
  const candidate = nested ? value.rules : value;
  if (!isRecord(candidate)) {
    return { code: "invalid_request", message: "config.rules must be an object" };
  }
  return validateRuleEntries(candidate, !nested);
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

async function handleSemanticReview(request: Request, env: Env): Promise<Response> {
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

async function handleAnalysis(request: Request, sarif: boolean): Promise<Response> {
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

async function handleMcp(request: Request, env: Env): Promise<Response> {
  if (!allowed(request, env.API_KEY)) {
    return json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" });
  }
  return mcpHandler.fetch(request);
}

type RouteHandler = (request: Request, env: Env, url: URL) => Response | Promise<Response>;

const routeHandlers = new Map<string, RouteHandler>([
  ["* /", () => json({ status: "ok" })],
  ["* /v1/docs", (_request, _env, url) => Response.redirect(`${url.origin}/v1/spec`, 302)],
  ["* /v1/spec", () => json(workerOpenApi)],
  ["* /v1/healthz", () => json({ status: "ok" })],
  ["* /v1/health", () => json({ status: "ok" })],
  ["* /v1/ready", () => json({ status: "ready", parser: "worker-native" })],
  ["* /v1/diagram-types", () => json({ "parser-recognized": ["flowchart", "sequence", "class", "er", "state"], "lint-supported": ["flowchart", "sequence", "class", "er", "state"] })],
  ["* /v1/rules", () => json({ rules: ruleMetadata })],
  ["* /v1/version", (_request, env) => json({ "service-version": env.BUILD_VERSION ?? "development", "build-commit": env.BUILD_SHA ?? "" })],
  ["* /mcp", handleMcp],
  ["POST /v1/semantic-review", handleSemanticReview],
  ["POST /v1/analyze/sarif", request => handleAnalysis(request, true)],
  ["POST /v1/analyze", request => handleAnalysis(request, false)],
  ["POST /v1/analyse", request => handleAnalysis(request, false)],
]);

async function routeRequest(request: Request, env: Env, url: URL): Promise<Response> {
  const key = `${request.method} ${url.pathname}`;
  const handler = routeHandlers.get(key) ?? routeHandlers.get(`* ${url.pathname}`);
  return handler ? handler(request, env, url) : json({ error: { code: "not_found", message: "Not found" } }, 404);
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const cors = corsHeaders(request, env);
    if (request.method === "OPTIONS") {
      return withCors(new Response(null, { status: Object.keys(cors).length ? 204 : 403, headers: cors }), cors, env);
    }
    const response = await routeRequest(request, env, url);
    return withCors(response, cors, env);
  }
};
