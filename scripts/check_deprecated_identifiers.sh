#!/usr/bin/env bash
set -euo pipefail

files=(README.md API_GUIDE.md openapi.yaml openapi.json internal/api/openapi.go)

fail=0

check() {
  local pattern="$1"
  local label="$2"

  if ! command -v rg &> /dev/null; then
    echo "ERROR: ripgrep (rg) is required but not installed. Install it from " >&2
    exit 1
  fi
  
  if rg -n --pcre2 "$pattern" "${files[@]}"; then
    echo "\nERROR: Found deprecated form: ${label}" >&2
    fail=1
  fi
}

# Deprecated severity spelling (canonical: warn)
check '"severity"\s*:\s*"warning"|severity:\s*warning\b' 'severity: warning'

# Deprecated max-fanout option key (canonical: limit)
check '"max"\s*:|\bmax:\s*[0-9]+' 'max-fanout option key max (use limit)'

if [[ "$fail" -ne 0 ]]; then
  echo "\nDeprecated identifier spellings detected." >&2
  exit 1
fi

echo "Deprecated identifier check passed."
