export interface Env {
  API_KEY?: string;
  OPENROUTER_API_KEY?: string;
  MERM8_DECISION_MODEL?: string;
  REST_ALLOWED_ORIGINS?: string;
  BUILD_VERSION?: string;
  BUILD_SHA?: string;
}

export const MAX_ANALYZE_BODY_BYTES = 1024 * 1024;

export const json = (body: unknown, status = 200, headers: HeadersInit = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json; charset=utf-8", ...headers },
  });

export const allowed = (request: Request, value?: string) =>
  !!value &&
  (request.headers.get("authorization") === "Bearer " + value ||
    request.headers.get("x-api-key") === value);

export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
