import { workerOpenApi } from "./openapi.js";
import { handleAnalysis, handleMcp, handleSemanticReview } from "./handlers.js";
import { ruleMetadata } from "./rule-metadata.js";
import { json, type Env } from "./shared.js";

type RouteHandler = (request: Request, env: Env, url: URL) => Response | Promise<Response>;

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
const worker = {
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

export default worker;
