---
name: <match-directory-name>
description: <purpose and when this skill applies>
---

# <Skill title>

## Inputs / Outputs / Non-goals

- Inputs: <files, artifacts, and context needed>
- Outputs: <concrete result>
- Non-goals: <nearby work outside the skill's scope>

## Trigger conditions

Use this skill when prompts include or imply:

- <specific task or intent>

## Mandatory rules

- Read the current implementation and canonical project docs before relying on examples.
- Preserve public contracts unless the task explicitly calls for a contract change.
- Keep changes scoped and report assumptions that affect the result.

## Validation checklist

- [ ] Run checks that cover the changed behavior; name the commands in this skill.
- [ ] If a relevant check cannot run, report why and what remains unverified.
- [ ] Update related docs or contract artifacts when the change requires it.

## Expected output format

- Summary: <what changed and why>
- Evidence: <checks run, or checks blocked and why>
- Risks: <material remaining risks, if any>

## Failure/stop conditions

- Stop and ask for clarification only when an unresolved choice could materially change the requested outcome or break a public contract.
- If a check is blocked, report the blocker and continue independent work where possible.
