---
name: ApplicationSecurityAndSupplyChain
description: Review security-sensitive merm8 code and maintain dependency and supply-chain safeguards.
---

# Application Security and Supply Chain

## Inputs / Outputs / Non-goals

- Inputs: Authentication and configuration code, parser boundaries, dependency manifests and lockfiles, `.github/workflows/codeql.yml`, `.github/workflows/dependency-review.yml`, and `.github/workflows/govulncheck.yml`.
- Outputs: A scoped security change or evidence-based review with relevant checks and residual risks identified.
- Non-goals: Do not claim a complete security audit from routine feature tests or make production security configuration changes without an explicit request.

## Trigger conditions

Use this skill when handling authentication, authorization, secrets, untrusted diagram input, subprocess boundaries, request limits, dependency updates, vulnerability findings, or security scanning configuration.

## Mandatory rules

- Trace data from external input to parsing, subprocess, storage, and response boundaries. Read the applicable security and operational docs, including `docs/authentication.md`, `docs/parser-tuning.md`, or `docs/worker-api.md`.
- Preserve input limits, timeouts, cancellation, authentication, rate limits, and secret handling. Treat Go server and Worker controls separately.
- For dependency changes, update the owning manifest and lockfile together; check compatibility and the repository's vulnerability scanning workflow. Do not upgrade unrelated packages.
- For GitHub Actions security changes, also use `GitHubActionsAndCI` and preserve least privilege, immutable action pins, and secret boundaries.
- Distinguish a reachable vulnerability from a package-level advisory and support conclusions with the affected code path and available scan evidence.

## Validation checklist

- [ ] Run focused tests for the affected trust boundary and `go vet ./...` for Go changes where applicable.
- [ ] Run `npm run test:workflows` for workflow security automation changes.
- [ ] Check the relevant repository scanner in CI: CodeQL, dependency review, govulncheck, or zizmor, as appropriate.
- [ ] Review the final diff for accidentally added credentials, weakened limits, or unrelated dependency changes.

## Expected output format

- Summary: State the security boundary or supply-chain control changed.
- Evidence: Identify code paths, tests, and scanner results; distinguish checks not run.
- Risks: Describe remaining exposure, deployment requirements, or unverified assumptions.

## Failure/stop conditions

- Stop before weakening authentication, limits, or workflow protections without an explicit security decision.
- If a suspected vulnerability cannot be validated from available code or scanner data, report it as uncertain rather than asserting exploitability.
