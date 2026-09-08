import { analyzeMermaid, supportedRules } from "../src/engine/analyze.js";
import { mcpHandler } from "../src/mcp/server.js";

export interface Env { API_KEY?: string; REST_ALLOWED_ORIGINS?: string; BUILD_VERSION?: string; BUILD_SHA?: string }
const json = (body: unknown, status = 200, headers: HeadersInit = {}) => new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json; charset=utf-8", ...headers } });
const allowed = (request: Request, value?: string) => !!value && (request.headers.get("authorization") === `Bearer ${value}` || request.headers.get("x-api-key") === value);

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url); const path = url.pathname;
    if (path === "/" || path === "/v1/docs") return Response.redirect(`${url.origin}/v1/spec`, 302);
    if (path === "/v1/healthz" || path === "/v1/health") return json({ status: "ok" });
    if (path === "/v1/ready") return json({ status: "ready", parser: "worker-native" });
    if (path === "/v1/diagram-types") return json({ "parser-recognized": ["flowchart", "sequence", "class", "er", "state"], "lint-supported": ["flowchart"] });
    if (path === "/v1/rules") return json({ rules: supportedRules });
    if (path === "/v1/version") return json({ "service-version": env.BUILD_VERSION ?? "development", "build-commit": env.BUILD_SHA ?? "" });
    if (path === "/mcp") { if (!allowed(request, env.API_KEY)) return json({ error: { code: "unauthorized", message: "A valid API key is required" } }, 401, { "www-authenticate": "Bearer" }); return mcpHandler.fetch(request); }
    if ((path === "/v1/analyze" || path === "/v1/analyse") && request.method === "POST") {
      let body: { code?: unknown; config?: unknown }; try { body = await request.json(); } catch (e) { console.error("JSON parse error:", e); return json({ error: { code: "invalid_json", message: "invalid JSON body" } }, 400); }
      if (typeof body.code !== "string") return json({ error: { code: "invalid_request", message: "code must be a string" } }, 400);
      return json(analyzeMermaid(body.code, (body.config && typeof body.config === "object" ? body.config : {}) as Record<string, Record<string, unknown>>));
    }
    return json({ error: { code: "not_found", message: "Not found" } }, 404);
  }
};
