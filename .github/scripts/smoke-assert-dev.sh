#!/usr/bin/env bash
# smoke-assert-dev.sh — verifies the forge dev tunnel deployed its resources
# correctly against Nimbus.
#
# Checks:
#   - shared SQS dev queues (req + res) exist
#   - stub Handler Lambda has FORGE_REQUEST_QUEUE_URL + FORGE_RESPONSE_QUEUE_URL
#
# Runs after `forge dev --stage ci-dev` has started (deploy complete).
# Expects AWS_ENDPOINT_URL, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and
# AWS_DEFAULT_REGION to be set in the environment (see smoke.yml).
set -uo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
APP="forge-smoke"
STAGE="ci-dev"
PREFIX="${APP}-${STAGE}"

CLI="aws --endpoint-url $ENDPOINT --region $REGION --no-cli-pager --output json"

PASS=0
FAIL=0

ok()   { echo "  ✓ $1";           PASS=$((PASS+1)); }
fail() { echo "  ✗ $1: ${2:-}";   FAIL=$((FAIL+1)); }

check_match() {
  local label="$1" pattern="$2"; shift 2
  local out
  if out=$("$@" 2>&1) && echo "$out" | grep -q "$pattern"; then
    ok "$label"
  else
    fail "$label" "pattern '$pattern' not found in: $(echo "$out" | head -3)"
  fi
}

echo "=== forge dev smoke assertions (${APP} / stage=${STAGE}) ==="
echo "    endpoint : ${ENDPOINT}"
echo ""

# ── Dev SQS queues ────────────────────────────────────────────────────────────

echo "── Dev SQS queues"
check_match "request queue ${PREFIX}-forge-dev-req exists" "${PREFIX}-forge-dev-req" \
  $CLI sqs get-queue-url --queue-name "${PREFIX}-forge-dev-req"
check_match "response queue ${PREFIX}-forge-dev-res exists" "${PREFIX}-forge-dev-res" \
  $CLI sqs get-queue-url --queue-name "${PREFIX}-forge-dev-res"

# ── Stub Lambda env vars ──────────────────────────────────────────────────────

echo ""
echo "── Stub Lambda env vars (Handler)"
ENV_JSON=$($CLI lambda get-function-configuration \
  --function-name "${PREFIX}-Handler" \
  --query 'Environment.Variables' 2>/dev/null || echo '{}')

for key in FORGE_REQUEST_QUEUE_URL FORGE_RESPONSE_QUEUE_URL FORGE_STAGE; do
  if echo "$ENV_JSON" | grep -q "\"${key}\""; then
    ok "env ${key} injected into stub Lambda"
  else
    fail "env ${key} missing from stub Lambda" "env dump: ${ENV_JSON}"
  fi
done

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

[ "$FAIL" -eq 0 ]
