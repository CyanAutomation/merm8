---
name: <skill-name>
description: <one-line purpose and when to use>
---

# Skill Instructions

## Inputs / Outputs / Non-goals

- Inputs: <required artifacts, files, and context>
- Outputs: <what this skill must produce>
- Non-goals: <what this skill must not do>

## Trigger conditions

Use this skill when prompts include or imply:

- <trigger phrase 1>
- <trigger phrase 2>

## Mandatory rules

- Follow domain constraints and avoid silent public API/schema changes.
- Keep changes scoped and deterministic.
- Record assumptions and unresolved ambiguities.

## Validation checklist

- [ ] Required commands/checks were run.
- [ ] Relevant tests were updated/executed.
- [ ] Risk/impact was documented.

## Expected output format

- Summary: <what changed and why>
- Evidence: <commands/tests/checks>
- Risks: <known risks and mitigations>

## Failure/stop conditions

- Stop if requirements are ambiguous in a way that can cause breaking changes.
- Stop if required validation cannot be performed and report the blocker.
