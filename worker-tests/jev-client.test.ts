import assert from "node:assert/strict";
import test from "node:test";
import { analyzeDiagram } from "../src/engine/analyze.js";
import { parseMermaid } from "../src/parser/parse.js";
import { requestJevDecisions, JEV_DECISIONS_URL } from "../src/semantic/jev-client.js";
import { semanticQuestions } from "../src/semantic/questions.js";
import { buildSemanticState } from "../src/semantic/state.js";
import { JevClientError, type JevQuestion } from "../src/semantic/types.js";

function sampleState() {
  const parsed = parseMermaid("flowchart TD\nA[Receive order] --> B[Validate payment]");
  assert.ok(parsed.diagram);
  return buildSemanticState(parsed.diagram, analyzeDiagram(parsed.diagram));
}

function validPayload(questions: Record<string, JevQuestion>) {
  return {
    model: "typesafe/jev-1.13",
    answers: Object.fromEntries(Object.entries(questions).map(([key, question]) => {
      if (question.type === "noul") return [key, { type: "noul", noul: 0.82 }];
      const choices = Object.keys(question.criteria);
      return [key, {
        type: "choice",
        choice: choices[0],
        confidence: 0.91,
        probabilities: Object.fromEntries(choices.map((choice, index) => [choice, index === 0 ? 1 : 0])),
      }];
    })),
  };
}

test("sends all typed semantic questions in one authenticated Decisions API request", async () => {
  let calls = 0;
  let requestBody: Record<string, unknown> | undefined;
  const result = await requestJevDecisions(sampleState(), semanticQuestions, {
    apiKey: "server-secret",
    model: "~typesafe/jev-latest",
    fetchImpl: async (input, init) => {
      calls += 1;
      assert.equal(input, JEV_DECISIONS_URL);
      assert.equal(init?.method, "POST");
      assert.equal(new Headers(init?.headers).get("authorization"), "Bearer server-secret");
      assert.equal(new Headers(init?.headers).get("content-type"), "application/json");
      requestBody = JSON.parse(String(init?.body)) as Record<string, unknown>;
      return Response.json(validPayload(semanticQuestions));
    },
  });

  assert.equal(calls, 1);
  assert.equal(requestBody?.model, "~typesafe/jev-latest");
  assert.deepEqual(Object.keys(requestBody?.questions as object), Object.keys(semanticQuestions));
  assert.deepEqual(requestBody?.state, sampleState());
  assert.equal(result.model, "typesafe/jev-1.13");
  assert.equal(result.answers["label-clarity"].type, "noul");
  assert.equal(result.answers["diagram-purpose"].type, "choice");
});

test("rejects malformed typed answers instead of returning a partial result", async () => {
  const malformed = validPayload(semanticQuestions) as { model: string; answers: Record<string, { type: string; noul?: number }> };
  malformed.answers["label-clarity"] = { type: "noul", noul: 1.2 };
  await assert.rejects(
    requestJevDecisions(sampleState(), semanticQuestions, {
      apiKey: "server-secret", maxRetries: 0,
      fetchImpl: async () => Response.json(malformed),
    }),
    (error: unknown) => error instanceof JevClientError && error.code === "invalid_response",
  );
});

test("retries transient provider errors once and logs only request metadata", async () => {
  let calls = 0;
  const logs: Array<{ event: string; metadata: Record<string, string | number> }> = [];
  await requestJevDecisions(sampleState(), semanticQuestions, {
    apiKey: "server-secret", retryDelayMs: 0,
    log: (event, metadata) => logs.push({ event, metadata }),
    fetchImpl: async () => {
      calls += 1;
      return calls === 1 ? new Response(null, { status: 503 }) : Response.json(validPayload(semanticQuestions));
    },
  });
  assert.equal(calls, 2);
  assert.equal(logs.length, 1);
  assert.equal(logs[0].event, "semantic_review_jev");
  assert.equal(logs[0].metadata["question-count"], 6);
  assert.equal(logs[0].metadata["retry-count"], 1);
  assert.equal(JSON.stringify(logs).includes("server-secret"), false);
});

test("does not retry non-transient authentication failures", async () => {
  let calls = 0;
  await assert.rejects(
    requestJevDecisions(sampleState(), semanticQuestions, {
      apiKey: "server-secret",
      fetchImpl: async () => { calls += 1; return new Response(null, { status: 401 }); },
    }),
    (error: unknown) => error instanceof JevClientError && error.code === "http" && error.status === 401,
  );
  assert.equal(calls, 1);
});

test("times out fetch using an abort signal", async () => {
  await assert.rejects(
    requestJevDecisions(sampleState(), semanticQuestions, {
      apiKey: "server-secret", timeoutMs: 5, maxRetries: 0,
      fetchImpl: async (_input, init) => new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => {
          const error = new Error("aborted");
          error.name = "AbortError";
          reject(error);
        }, { once: true });
      }),
    }),
    (error: unknown) => error instanceof JevClientError && error.code === "timeout",
  );
});

test("requires OpenRouter credentials before attempting a request", async () => {
  let calls = 0;
  await assert.rejects(
    requestJevDecisions(sampleState(), semanticQuestions, {
      fetchImpl: async () => { calls += 1; return Response.json({}); },
    }),
    (error: unknown) => error instanceof JevClientError && error.code === "credentials",
  );
  assert.equal(calls, 0);
});

test("rejects invalid model configuration before attempting a request", async () => {
  let calls = 0;
  await assert.rejects(
    requestJevDecisions(sampleState(), semanticQuestions, {
      apiKey: "server-secret",
      model: "not a valid model name",
      fetchImpl: async () => { calls += 1; return Response.json({}); },
    }),
    (error: unknown) => error instanceof JevClientError && error.code === "configuration",
  );
  assert.equal(calls, 0);
});

test("does not trust a provider model name that contains the API key", async () => {
  const payload = validPayload(semanticQuestions);
  payload.model = "provider/server-secret";
  const result = await requestJevDecisions(sampleState(), semanticQuestions, {
    apiKey: "server-secret",
    fetchImpl: async () => Response.json(payload),
  });

  assert.equal(result.model, "~typesafe/jev-latest");
  assert.equal(JSON.stringify(result).includes("server-secret"), false);
});
