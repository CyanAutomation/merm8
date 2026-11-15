---
name: DocumentationMaintenance
description: Keep merm8 guides, examples, and changelog accurate against the current code and API contracts.
---

# Documentation Maintenance

## Inputs / Outputs / Non-goals

- Inputs: `README.md`, `docs/`, `API_GUIDE.md`, `IMPLEMENTATION_GUIDE.md`, `CHANGELOG.md`, and the implementation or contract being documented.
- Outputs: Focused documentation updates supported by current code, tests, or canonical contracts.
- Non-goals: Do not change application behavior to make stale documentation appear correct, or rewrite accurate material for style alone.

## Trigger conditions

Use this skill when adding or changing user-facing behavior, examples, setup instructions, API or configuration docs, troubleshooting guidance, or changelog entries.

## Mandatory rules

- Verify claims against the current implementation and its tests. For API behavior, consult `openapi.yaml`, `API_GUIDE.md`, or the runtime-specific contract; do not infer Worker parity.
- Keep examples executable and scoped to the stated runtime, supported Mermaid syntax, and documented defaults. Do not turn parser recognition into a claim of lint support.
- Preserve the docs index and links when adding or moving documents. Keep operational/security guidance aligned with the actual deployment and configuration.
- Follow `CONTRIBUTING.md` for user-visible changelog entries. Do not add a changelog entry for internal-only wording or formatting changes.
- Prefer a focused correction over a broad rewrite. State uncertainty instead of inventing behavior.

## Validation checklist

- [ ] Check changed examples against the target runtime or contract where practical.
- [ ] Run `npm run test:readme-links` after changing README link-checking behavior, and `npm test` for README anchor/link changes.
- [ ] Review links, headings, version numbers, defaults, and command paths in the changed documents.
- [ ] For API documentation changes, use the `RestApiDesign` skill and run its contract checks as applicable.

## Expected output format

- Summary: Name the documents updated and the implementation facts clarified.
- Evidence: Report example, link, or contract checks run.
- Risks: Note claims that could not be verified or runtime-specific differences.

## Failure/stop conditions

- If code and canonical contract docs disagree, identify the conflict and avoid silently declaring either behavior correct.
- If an example depends on unavailable services, mark that limitation and verify the static contract instead.
