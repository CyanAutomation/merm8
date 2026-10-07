#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GREMLINS="github.com/go-gremlins/gremlins/cmd/gremlins@v0.5.1"
MUTATION_WORKERS="${MUTATION_WORKERS:-2}"

cd "$REPO_ROOT"
go run "$GREMLINS" unleash ./internal/engine --workers "$MUTATION_WORKERS"
