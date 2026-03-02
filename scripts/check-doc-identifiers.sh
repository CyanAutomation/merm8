#!/usr/bin/env bash
set -euo pipefail

files=(
  "README.md"
  "API_GUIDE.md"
  "openapi.yaml"
  "openapi.json"
  "internal/api/openapi.go"
)

failed=0

if rg -n '"max_fanout"|\bmax_fanout\s*:' "${files[@]}"; then
  echo "Deprecated identifier spelling found: max_fanout"
  failed=1
fi

if rg -n 'severity\s*[:=]\s*"warning"|"severity"\s*:\s*"warning"' "${files[@]}"; then
  echo "Deprecated severity spelling found: warning"
  failed=1
fi

if rg -n '"max"\s*:|\bmax\s*:' "${files[@]}"; then
  echo "Deprecated max-fanout option name found: max"
  failed=1
fi

if [[ "$failed" -ne 0 ]]; then
  echo "Use canonical forms: max-fanout, warn, and limit." >&2
  exit 1
fi

echo "Docs/spec identifier spellings look canonical."
