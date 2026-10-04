import assert from "node:assert/strict";
import test from "node:test";
import worker from "../worker/index.js";
import { semanticQuestions } from "../src/semantic/questions.js";
import type { JevQuestion } from "../src/semantic/types.js";

const authenticatedEnv = { API_KEY: "merm8-key", OPENROUTER_API_KEY: "openrouter-key" };

function jevPayload(questions: Record<string, JevQuestion> = semanticQuestions) {
  return {
    model: "typesafe/jev-1.13",
    answers: Object.fromEntries(Object.entries(questions).map(([key, question]) => {
      if (question.type === "noul") return [key, { type: "noul", noul: key === "ambiguity" ? 0.2 : 0.9 }];
      const choice = key === "review-priority" ? "low" : "process";
      const choices = Object.keys(question.criteria);
      return [key, { type: "choice", choice, confidence: 0.88, probabilities: Object.fromEntries(choices.map((item) => [item, item === choice ? 1 : 0])) }];
    })),
  };
}

function request(body: string, headers: Record<string, string> = { authorization: "Bearer merm8-key", "content-type": "application/json" }) {
  return new Request("https://example.test/v1/semantic-review", { method: "POST", headers, body });
}

test("protects the billable semantic endpoint with a configured Merm8 API key", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls += 1; return Response.json(jevPayload()); });
  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA --> B" }), { "content-type": "application/json" }), authenticatedEnv);
  assert.equal(response.status, 401);
  assert.equal(response.headers.get("www-authenticate"), "Bearer");
  const unconfigured = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA --> B" }), { "content-type": "application/json" }), { OPENROUTER_API_KEY: "openrouter-key" });
  assert.equal(unconfigured.status, 401);
  assert.equal(calls, 0);
});

test("validates JSON and required code before semantic processing", async (t) => {
  t.mock.method(console, "error", () => {});
  const malformed = await worker.fetch(request("{"), authenticatedEnv);
  assert.equal(malformed.status, 400);

  const missingCode = await worker.fetch(request(JSON.stringify({ diagram: "flowchart TD" })), authenticatedEnv);
  assert.equal(missingCode.status, 400);
});

test("supports authenticated semantic review preflight through the configured CORS policy", async () => {
  const response = await worker.fetch(new Request("https://example.test/v1/semantic-review", {
    method: "OPTIONS",
    headers: {
      origin: "https://merm8-splash.vercel.app",
      "access-control-request-method": "POST",
      "access-control-request-headers": "content-type, authorization",
    },
  }), { ...authenticatedEnv, REST_ALLOWED_ORIGINS: "https://merm8-splash.vercel.app" });
  assert.equal(response.status, 204);
  assert.equal(response.headers.get("access-control-allow-origin"), "https://merm8-splash.vercel.app");
  assert.match(response.headers.get("access-control-allow-headers") ?? "", /authorization/);
});

test("does not call JEV when Mermaid parsing fails", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls += 1; return Response.json(jevPayload()); });
  const response = await worker.fetch(request(JSON.stringify({ code: "not a Mermaid diagram" })), authenticatedEnv);
  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), {
    error: { code: "invalid_mermaid", message: "Mermaid source could not be parsed", details: { code: "syntax_error", message: "Unsupported or unrecognised Mermaid diagram type", line: 1, column: 1 } },
  });
  assert.equal(calls, 0);
});

test("does not call JEV when parsing yields no semantic nodes", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls += 1; return Response.json(jevPayload()); });
  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD" })), authenticatedEnv);
  assert.equal(response.status, 422);
  assert.deepEqual(await response.json(), { error: { code: "semantic_state_unavailable", message: "The parsed diagram contains no nodes for semantic review" } });
  assert.equal(calls, 0);
});

test("returns an explicit unconfigured error when OpenRouter credentials are missing", async () => {
  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA[Start] --> B[Finish]" })), { API_KEY: "merm8-key" });
  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), { error: { code: "missing_openrouter_credentials", message: "OpenRouter credentials are not configured" } });
});

test("returns deterministic analysis and separate semantic review from a single JEV request", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    calls += 1;
    assert.equal(input, "https://openrouter.ai/api/alpha/decisions");
    assert.equal(new Headers(init?.headers).get("authorization"), "Bearer openrouter-key");
    const payload = JSON.parse(String(init?.body)) as { model: string; state: { diagram: { nodes: Array<{ id: string; label: string }> } }; questions: Record<string, unknown> };
    assert.equal(payload.model, "~typesafe/jev-latest");
    assert.equal(Object.keys(payload.questions).length, 6);
    assert.deepEqual(payload.state.diagram.nodes.slice(0, 2), [{ id: "A", label: "Receive order" }, { id: "B", label: "Validate payment" }]);
    return Response.json(jevPayload());
  });

  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA[Receive order] --> B[Validate payment]" })), authenticatedEnv);
  assert.equal(response.status, 200);
  const result = await response.json() as {
    valid: boolean;
    issues: unknown[];
    "semantic-review": { purpose: { value: string }; "label-clarity": { value: boolean; probability: number }; ambiguity: { value: boolean } };
    meta: { source: string; model: string };
  };
  assert.equal(calls, 1);
  assert.equal(result.valid, true);
  assert.deepEqual(result.issues, []);
  assert.equal(result["semantic-review"].purpose.value, "process");
  assert.equal(result["semantic-review"]["label-clarity"].value, true);
  assert.equal(result["semantic-review"]["label-clarity"].probability, 0.9);
  assert.equal(result["semantic-review"].ambiguity.value, false);
  assert.deepEqual(result.meta, { source: "jev", model: "typesafe/jev-1.13" });
  assert.equal(JSON.stringify(result).includes("openrouter-key"), false);
});

test("maps upstream network failures to a safe unavailable response", async (t) => {
  let calls = 0;
  t.mock.method(globalThis, "fetch", async () => { calls += 1; throw new TypeError("upstream private detail"); });
  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA --> B" })), authenticatedEnv);
  assert.equal(response.status, 503);
  assert.deepEqual(await response.json(), { error: { code: "jev_upstream_error", message: "Semantic review provider is unavailable" } });
  assert.equal(calls, 2);
});

test("maps upstream timeout responses to a safe gateway timeout", async (t) => {
  t.mock.method(globalThis, "fetch", async () => {
    const error = new Error("provider internal details");
    error.name = "TimeoutError";
    throw error;
  });
  const response = await worker.fetch(request(JSON.stringify({ code: "flowchart TD\nA --> B" })), authenticatedEnv);
  assert.equal(response.status, 504);
  assert.deepEqual(await response.json(), { error: { code: "jev_timeout", message: "Semantic review provider timed out" } });
});

test("keeps deterministic analyze independent of JEV and includes semantic route in OpenAPI", async (t) => {
  t.mock.method(globalThis, "fetch", async () => { throw new Error("must not call OpenRouter from /v1/analyze"); });
  const response = await worker.fetch(new Request("https://example.test/v1/analyze", {
    method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ code: "flowchart TD\nA --> B\nB --> A" }),
  }), {});
  assert.equal(response.status, 200);
  assert.equal((await response.json() as { valid: boolean }).valid, true);

  const specResponse = await worker.fetch(new Request("https://example.test/v1/spec"), {});
  const spec = await specResponse.json() as { paths: Record<string, { post?: { security?: unknown[]; requestBody?: unknown; responses?: Record<string, unknown> } }> };
  const endpoint = spec.paths["/v1/semantic-review"]?.post;
  assert.ok(endpoint);
  assert.equal(endpoint.security?.length, 2);
  assert.ok(endpoint.requestBody);
  assert.ok(endpoint.responses?.["401"]);
  assert.ok(endpoint.responses?.["504"]);
});
