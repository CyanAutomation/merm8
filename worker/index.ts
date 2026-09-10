import { analyzeMermaid } from "../src/engine/analyze.js";
import { mcpHandler } from "../src/mcp/server.js";
import { workerOpenApi } from "./openapi.js";

export interface Env { API_KEY?: string; REST_ALLOWED_ORIGINS?: string; BUILD_VERSION?: string; BUILD_SHA?: string }
const json = (body: unknown, status = 200, headers: HeadersInit = {}) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json; charset=utf-8", ...headers } });
const allowed = (request: Request, value?: string) => !!value && (request.headers.get("authorization") === `Bearer ${value}` || request.headers.get("x-api-key") === value);
const MAX_ANALYZE_BODY_BYTES = 1024 * 1024;
const ruleMetadata = [
  { id: "max-fanout", description: "Limit the number of outgoing connections from a node.", severity: "warning", "default-config": { limit: 5 } },
  { id: "no-cycles", description: "Disallow cycles in flowcharts.", severity: "error" },
  { id: "no-disconnected-nodes", description: "Disallow nodes that have no connections.", severity: "error" },
  { id: "no-duplicate-node-ids", description: "Disallow repeated node IDs.", severity: "error" },
] as const;

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

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url); const path = url.pathname;
    const cors = corsHeaders(request, env);
    if (request.method === "OPTIONS") {
      return withCors(new Response(null, { status: Object.keys(cors).length ? 204 : 403, headers: cors }), cors, env);
    }
    const response = await (async (): Promise<Response> => {
      if (path === "/") return json({ status: "ok" });
      if (path === "/v1/docs") return Response.redirect(`${url.origin}/v1/spec`, 302);
      if (path === "/v1/spec") return json(workerOpenApi);
      if (path === "/v1/healthz" || path === "/v1/health") return json({ status: "ok" });
      if (path === "/v1/ready") return json({ status: "ready", parser: "worker-native" });
      if (path === "/v1/diagram-types") return json({ "parser-recognized": ["flowchart", "sequence", "class", "er", "state"], "lint-supported": ["flowchart"] });
      if (path === "/v1/rules") return json({ rules: ruleMetadata });
      if (path === "/v1/version") return json({ "service-version": env.BUILD_VERSION ?? "development", "build-commit": env.BUILD_SHA ?? "" });
      if (path === "/mcp") { if (!allowed(request, env.API_KEY)) return json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" }); return mcpHandler.fetch(request); }
      if ((path === "/v1/analyze" || path === "/v1/analyse") && request.method === "POST") {
        const parsed = await parseAnalyzeBody(request);
        if (parsed.response) return parsed.response;
        const body = parsed.body!;
        if (typeof body.code !== "string") return json({ error: { code: "invalid_request", message: "code must be a string" } }, 400);
        return json(analyzeMermaid(body.code, ruleConfig(body.config)));
      }
      return json({ error: { code: "not_found", message: "Not found" } }, 404);
    })();
    return withCors(response, cors, env);
  }
};
