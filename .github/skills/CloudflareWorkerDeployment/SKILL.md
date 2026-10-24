---
name: CloudflareWorkerDeployment
description: Develop, verify, and deploy merm8's Worker with Wrangler and its separate API contract.
---

# Cloudflare Worker and Deployment

## Inputs / Outputs / Non-goals

- Inputs: `worker/`, `src/`, `worker-tests/`, `wrangler.toml`, and the deployment workflow.
- Outputs: A Worker change or deployment that matches the Worker-specific contract and bindings.
- Non-goals: Do not assume parity with the Go server or introduce a container workflow; this repo has no Dockerfile or image-publishing workflow.

## Trigger conditions

Use this skill when changing Cloudflare Worker routes, parser/engine behavior, bindings, Wrangler configuration, or deployment automation.

## Mandatory rules

- The Wrangler entry point is `worker/index.ts`; shared Worker-safe parser and analysis code lives in `src/`.
- The Worker is a separate implementation from the Go API. Check `docs/worker-api.md`, `worker/openapi.ts`, and runtime routes before claiming endpoint or rule parity.
- Use Wrangler configuration and bindings from `wrangler.toml`. Do not put credentials in source or plain configuration; use Wrangler secrets for `API_KEY` and `OPENROUTER_API_KEY` when those features are configured.
- The deterministic analysis route is public. `API_KEY` is for `/mcp`; semantic review is a separate optional feature. Preserve the request limits and rate-limit behavior documented in `docs/worker-api.md`.
- Deployment uses the repository's Cloudflare workflow and `wrangler deploy`, not Docker. Treat deployment as a publishing action and only perform it when the task explicitly requests deployment.

## Validation checklist

- [ ] Run `npm run typecheck` after Worker or TypeScript changes.
- [ ] Run `npm run test:worker` after Worker behavior changes.
- [ ] Run `npm run test:workflows` after editing deployment or CI workflow validation logic.
- [ ] Run `npm test` for broad repository JavaScript checks and README links when practical.
- [ ] Use `npm run dev` for a local Wrangler smoke test when route behavior changes.

## Expected output format

- Summary: Describe the Worker behavior, configuration, or deployment workflow changed.
- Evidence: Report typecheck, tests, and local Wrangler checks run.
- Risks: Note Worker/Go differences, binding requirements, or deployment effects.

## Failure/stop conditions

- Stop before making a deployment or changing production bindings unless deployment/configuration changes were requested.
- If a Worker feature relies on a missing Cloudflare binding or secret, report the exact requirement instead of adding a credential to source.
