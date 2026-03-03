#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

go run ./scripts/generate_openapi.go

git diff --exit-code -- openapi.json openapi.yaml
