#!/usr/bin/env bash
set -euo pipefail

SERVICE_URL="${MERM8_SMOKE_TEST_URL:-http://localhost:8080}"
SERVICE_URL="${SERVICE_URL%/}"
REQUEST_TIMEOUT="${SMOKE_TEST_TIMEOUT_SECONDS:-10}"
RESPONSE_FILE="$(mktemp)"
trap 'rm -f "$RESPONSE_FILE"' EXIT

request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local -a args=(--silent --show-error --connect-timeout "$REQUEST_TIMEOUT" --max-time "$REQUEST_TIMEOUT" -X "$method")
  local result

  if [[ -n "${ANALYZE_AUTH_TOKEN:-}" ]]; then
    args+=(-H "Authorization: Bearer ${ANALYZE_AUTH_TOKEN}")
  fi
  if [[ -n "$body" ]]; then
    args+=(-H 'Content-Type: application/json' --data "$body")
  fi

  if ! result="$(curl "${args[@]}" -o "$RESPONSE_FILE" -w '%{http_code} %{content_type}' "$SERVICE_URL$path")"; then
    echo "FAIL: request to $SERVICE_URL$path failed" >&2
    return 1
  fi
  read -r HTTP_STATUS HTTP_CONTENT_TYPE <<<"$result"
  HTTP_BODY="$(cat "$RESPONSE_FILE")"
}

expect_status() {
  local expected="$1"
  if [[ "$HTTP_STATUS" != "$expected" ]]; then
    echo "FAIL: expected HTTP $expected, got $HTTP_STATUS: $HTTP_BODY" >&2
    return 1
  fi
}

expect_json() {
  local filter="$1"
  if ! jq -e "$filter" >/dev/null <<<"$HTTP_BODY"; then
    echo "FAIL: response did not satisfy jq filter ($filter): $HTTP_BODY" >&2
    return 1
  fi
}

analyze() {
  local code="$1"
  local config='{}'
  local body
  if (($# >= 2)); then
    config="$2"
  fi
  body="$(jq -cn --arg code "$code" --argjson config "$config" '{code:$code,config:$config}')"
  request POST /v1/analyze "$body"
}

echo "Checking service at $SERVICE_URL"
request GET /v1/healthz
expect_status 200
expect_json '.status == "ok"'
echo "PASS: versioned health endpoint"

if [[ "${TEST_RATE_LIMIT_ENABLED:-0}" == "1" ]]; then
  # Deployment smoke only: use a disposable service and fresh client quota window.
  if [[ ! "${TEST_RATE_LIMIT_PER_MIN:-}" =~ ^[1-9][0-9]*$ ]]; then
    echo "Set TEST_RATE_LIMIT_PER_MIN to the service's ANALYZE_RATE_LIMIT_PER_MINUTE value." >&2
    exit 2
  fi
  echo "Rate-limit mode: this must be a fresh service/client window with limit $TEST_RATE_LIMIT_PER_MIN."
  for ((i = 1; i <= TEST_RATE_LIMIT_PER_MIN; i++)); do
    analyze $'graph TD\nA-->B'
    expect_status 200
  done
  analyze $'graph TD\nA-->B'
  expect_status 429
  expect_json '.error.code == "rate_limited"'
  echo "PASS: analyze requests are rate limited at the configured quota"
  exit 0
fi

analyze $'graph TD\nA[Start] --> B[Process]\nB --> C{Decision}\nC -->|Yes| D[Output]\nC -->|No| E[Error]\nD --> F[End]'
expect_status 200
expect_json '.valid == true and .metrics["node-count"] == 6 and .metrics["edge-count"] == 5'
echo "PASS: valid diagram returns semantic analysis metrics"

flow_config='{"schema-version":"v1","rules":{"max-fanout":{"limit":1}}}'
analyze $'graph TD\nA-->B\nA-->C' "$flow_config"
expect_status 200
expect_json '.valid == true and any(.issues[]; .["rule-id"] == "max-fanout")'
echo "PASS: configured max-fanout rule reports a violation"

request POST /v1/analyze/sarif "$(jq -cn --arg code $'graph TD\nA-->B' '{code:$code}')"
expect_status 200
if [[ "$HTTP_CONTENT_TYPE" != application/sarif+json* ]]; then
  echo "FAIL: expected application/sarif+json, got $HTTP_CONTENT_TYPE" >&2
  exit 1
fi
expect_json '.version == "2.1.0" and .runs[0].tool.driver.name == "merm8" and (.runs[0].results | length == 0)'
echo "PASS: SARIF endpoint returns a SARIF 2.1.0 document"

sarif_issue_config='{"schema-version":"v1","rules":{"max-fanout":{"limit":1,"severity":"error"}}}'
sarif_issue_body="$(jq -cn --arg code $'graph TD\nA-->B\nA-->C' --argjson config "$sarif_issue_config" '{code:$code,config:$config}')"
request POST /v1/analyze/sarif "$sarif_issue_body"
expect_status 200
if [[ "$HTTP_CONTENT_TYPE" != application/sarif+json* ]]; then
  echo "FAIL: expected application/sarif+json, got $HTTP_CONTENT_TYPE" >&2
  exit 1
fi
expect_json '.version == "2.1.0" and any(.runs[0].results[]; .ruleId == "max-fanout" and .level == "error")'
echo "PASS: SARIF includes configured rule violations and severity"

request POST /v1/analyze '{}'
expect_status 400
expect_json '.error.code == "missing_code"'
echo "PASS: missing code is rejected with the documented error code"

echo "All smoke checks passed."
