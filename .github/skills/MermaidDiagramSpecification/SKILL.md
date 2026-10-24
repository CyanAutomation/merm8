---
name: MermaidDiagramSpecification
description: Apply Mermaid syntax accurately while respecting the diagram types and structures merm8 supports.
---

# Mermaid Diagram Specification

## Inputs / Outputs / Non-goals

- Inputs: Official Mermaid syntax, parser AST modules, diagram fixtures, and runtime capability metadata.
- Outputs: Correct Mermaid examples and changes that match the target parser and rule support.
- Non-goals: Do not present the full Mermaid language as implemented by every merm8 runtime.

## Trigger conditions

Use this skill when changing Mermaid parsing, examples, syntax hints, diagram models, or rules tied to diagram structure.

## Mandatory rules

- Distinguish Mermaid's language from merm8's supported subset. Current recognized families include flowchart, sequence, class, ER, and state; parser recognition does not by itself guarantee lint support.
- Check runtime capability from `GET /v1/diagram-types` and `GET /v1/rules` where available. The Worker and Go server can expose different rule sets.
- Use Mermaid's official documentation for syntax claims. Avoid describing all Mermaid diagrams as directed graphs; sequence, class, and ER diagrams have different structures and semantics.
- For parser behavior, inspect `parser-node/ast-*.ts` and the fixtures in `parser-node/parse.test.ts`. For Worker behavior, inspect `src/parser`, `src/engine`, and `worker`.
- Keep examples valid for the intended Mermaid version. Include diagram type headers, and distinguish node IDs from display labels.
- Keep parser syntax errors, successfully parsed but unsupported types, and lint findings as separate outcomes.

## Validation checklist

- [ ] Run `npm run test:parser` for Node parser or fixture changes.
- [ ] Run relevant Worker tests when changing Worker parsing or analysis.
- [ ] Run `PARSER_SCRIPT=./parser-node/parse.ts go test ./internal/parser` when Go's parsed model is affected.
- [ ] Verify each new example against the parser or official Mermaid editor.

## Expected output format

- Summary: Identify the diagram family and syntax or support behavior changed.
- Evidence: Report parser, fixture, or Worker checks used.
- Risks: Note Mermaid-version or runtime-support differences.

## Failure/stop conditions

- Stop before claiming a diagram type or syntax feature is lint-supported without checking the selected runtime's capability list.
- If Mermaid's AST omits source details needed by a rule, identify the parser/model dependency explicitly.
