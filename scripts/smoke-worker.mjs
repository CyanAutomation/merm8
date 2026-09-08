const endpoint = (process.env.MERM8_API_URL ?? "").replace(/\/$/, "");

if (!endpoint) {
  throw new Error("MERM8_API_URL is required");
}

async function request(path, init) {
  const response = await fetch(`${endpoint}${path}`, init);
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}: ${await response.text()}`);
  }
  return response;
}

const health = await request("/v1/healthz");
const healthPayload = await health.json();
if (healthPayload.status !== "ok") throw new Error("healthz did not report ok");
if (!health.headers.get("x-merm8-build")) throw new Error("missing deployment provenance header");

const spec = await request("/v1/spec");
const specPayload = await spec.json();
if (specPayload.openapi !== "3.0.3" || !specPayload.paths?.["/v1/analyze"]) {
  throw new Error("OpenAPI document does not describe /v1/analyze");
}

const valid = await request("/v1/analyze", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({
    code: "flowchart TD\n  A[Start] --> B{Decision}\n  B --> C[End]\n  B --> D[Alternate]\n  C --> E[Done]\n  D --> E",
    config: { "schema-version": "v1", rules: { "no-duplicate-node-ids": { enabled: true } } },
  }),
});
const validPayload = await valid.json();
if (!validPayload.valid || validPayload.issues.some(issue => issue["rule-id"] === "no-duplicate-node-ids")) {
  throw new Error("valid flowchart was not analysed cleanly");
}

const invalid = await request("/v1/analyze", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ code: "flowchart TD\n  A --> --> B" }),
});
const invalidPayload = await invalid.json();
if (invalidPayload.valid || invalidPayload.error?.code !== "syntax_error") {
  throw new Error("malformed Mermaid was accepted");
}

console.log(`Worker smoke test passed: ${endpoint}`);
