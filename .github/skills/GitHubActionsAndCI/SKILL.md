---
name: GitHubActionsAndCI
description: Change merm8 GitHub Actions workflows, CI checks, security automation, and repository automation safely.
---

# GitHub Actions and CI

## Inputs / Outputs / Non-goals

- Inputs: `.github/workflows/`, `.github/zizmor.yml`, workflow validation scripts, and relevant application build/test commands.
- Outputs: A workflow change with scoped triggers, permissions, secrets, and checks that match its purpose.
- Non-goals: Do not deploy production or change application behavior as a side effect of workflow maintenance.

## Trigger conditions

Use this skill when changing GitHub Actions triggers, jobs, permissions, action versions, concurrency, caches, environments, reusable workflows, or automated repository tasks.

## Mandatory rules

- Read `AGENTS.md` and the affected workflow, then check its validator tests and the behavior the workflow is meant to guarantee.
- Grant the minimum job-level `permissions` required. Keep third-party actions pinned to full commit SHAs with a version comment, and preserve `persist-credentials: false` unless a narrowly scoped job requires credentials.
- Keep secrets out of command text and logs. Use GitHub secrets or environment secrets, and do not expose them to untrusted pull request code.
- Preserve event/path filters, concurrency behavior, protected environments, commit verification, and deployment gates unless the request explicitly changes them.
- Treat deployment, release, and external automation submissions as publishing actions. Do not invoke them while validating workflow edits.
- For Kaseki workflows, preserve bounded requests, allowlists, idempotency, main-branch restrictions, and response validation.

## Validation checklist

- [ ] Run `npm run test:workflows` after changing workflow behavior or its validation logic.
- [ ] Run `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.10` for workflow syntax and expression changes when Go and network access are available.
- [ ] Review the changed workflow's permissions, secrets exposure, triggers, concurrency, and deployment gates.
- [ ] Run `npm test` when the change affects the broader repository checks.

## Expected output format

- Summary: Describe the workflow behavior and triggering events changed.
- Evidence: Report validators and syntax checks run, or blockers.
- Risks: Note permission, secret exposure, skipped-check, or deployment implications.

## Failure/stop conditions

- Stop before a production deployment, release, or external submission unless the user explicitly requested that action.
- If a workflow's security boundary or required check is unclear, preserve the existing restriction and describe the unresolved decision.
