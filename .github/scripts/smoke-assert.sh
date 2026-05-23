#!/usr/bin/env bash
# smoke-assert.sh — verifies every forge construct in examples/smoke/ created its
# AWS resource against Nimbus and that link injection injected the correct SST_*
# env vars into the Handler Lambda.
#
# Runs after `forge deploy --stage ci` succeeds.
# Expects AWS_ENDPOINT_URL, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, and
# AWS_DEFAULT_REGION to be set in the environment (see smoke.yml).
set -uo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
# Nimbus always returns account 000000000000 from STS GetCallerIdentity.
ACCOUNT_ID="000000000000"
APP="forge-smoke"
STAGE="ci"
PREFIX="${APP}-${STAGE}"

CLI="aws --endpoint-url $ENDPOINT --region $REGION --no-cli-pager --output json"

PASS=0
FAIL=0

ok()   { echo "  ✓ $1";    PASS=$((PASS+1)); }
fail() { echo "  ✗ $1: ${2:-}"; FAIL=$((FAIL+1)); }

check() {
  local label="$1"; shift
  if "$@" >/dev/null 2>&1; then
    ok "$label"
  else
    fail "$label" "command failed: $*"
  fi
}

check_match() {
  local label="$1" pattern="$2"; shift 2
  local out
  if out=$("$@" 2>&1) && echo "$out" | grep -q "$pattern"; then
    ok "$label"
  else
    fail "$label" "pattern '$pattern' not found in: $(echo "$out" | head -3)"
  fi
}

echo "=== forge smoke assertions (${APP} / stage=${STAGE}) ==="
echo "    endpoint : ${ENDPOINT}"
echo "    account  : ${ACCOUNT_ID}"
echo ""

# ── DynamoDB ──────────────────────────────────────────────────────────────────

echo "── DynamoDB"
check "table ${PREFIX}-Records exists" \
  $CLI dynamodb describe-table --table-name "${PREFIX}-Records"

# ── S3 ────────────────────────────────────────────────────────────────────────

echo ""
echo "── S3"
# Bucket name includes account ID suffix (see constructs/helpers.go bucketName).
BUCKET_NAME="${PREFIX}-assets-${ACCOUNT_ID}"
check "bucket ${BUCKET_NAME} exists" \
  $CLI s3api head-bucket --bucket "${BUCKET_NAME}"

# ── SQS ───────────────────────────────────────────────────────────────────────

echo ""
echo "── SQS"
check_match "queue ${PREFIX}-Events exists" "${PREFIX}-Events" \
  $CLI sqs get-queue-url --queue-name "${PREFIX}-Events"
check_match "DLQ ${PREFIX}-Events-dlq exists" "${PREFIX}-Events-dlq" \
  $CLI sqs get-queue-url --queue-name "${PREFIX}-Events-dlq"

# ── SNS ───────────────────────────────────────────────────────────────────────

echo ""
echo "── SNS"
check_match "topic ${PREFIX}-Alerts exists" "${PREFIX}-Alerts" \
  $CLI sns list-topics

# ── Lambda ────────────────────────────────────────────────────────────────────

echo ""
echo "── Lambda"
for fn in Handler Events-consumer Alerts-sub-0 Heartbeat; do
  check "function ${PREFIX}-${fn} exists" \
    $CLI lambda get-function --function-name "${PREFIX}-${fn}"
done

# ── Link injection ────────────────────────────────────────────────────────────
# Verify that NewDynamoDB + NewBucket + NewSecret link env vars were injected
# into the Handler function's environment.

echo ""
echo "── Link injection (Handler Lambda env vars)"
ENV_JSON=$($CLI lambda get-function-configuration \
  --function-name "${PREFIX}-Handler" \
  --query 'Environment.Variables' 2>/dev/null || echo '{}')

for key in \
  SST_TABLE_RECORDS_NAME \
  SST_TABLE_RECORDS_ARN \
  SST_BUCKET_ASSETS_NAME \
  SST_BUCKET_ASSETS_ARN \
  SST_SECRET_SMOKE_KEY; do
  if echo "$ENV_JSON" | grep -q "\"${key}\""; then
    ok "env ${key} injected"
  else
    fail "env ${key} missing" "env dump: ${ENV_JSON}"
  fi
done

# Spot-check the secret value was resolved.
SECRET_VAL=$(echo "$ENV_JSON" | python3 -c \
  "import json,sys; d=json.load(sys.stdin); print(d.get('SST_SECRET_SMOKE_KEY',''))" 2>/dev/null || echo "")
if [ "$SECRET_VAL" = "ci-smoke-secret" ]; then
  ok "SST_SECRET_SMOKE_KEY = ci-smoke-secret (correct value)"
else
  fail "SST_SECRET_SMOKE_KEY value mismatch" "got: ${SECRET_VAL}"
fi

# ── API Gateway v2 ────────────────────────────────────────────────────────────

echo ""
echo "── API Gateway v2 (HTTP API)"
API_ID=$($CLI apigatewayv2 get-apis \
  --query "Items[?Name=='${PREFIX}-Api'].ApiId | [0]" \
  --output text 2>/dev/null || echo "")
if [ -n "$API_ID" ] && [ "$API_ID" != "None" ]; then
  ok "HTTP API ${PREFIX}-Api found (id=${API_ID})"
  check_match "route GET / registered" "GET /" \
    $CLI apigatewayv2 get-routes --api-id "$API_ID"
  check_match "route GET /health registered" "GET /health" \
    $CLI apigatewayv2 get-routes --api-id "$API_ID"
else
  fail "HTTP API ${PREFIX}-Api not found" \
    "check: $CLI apigatewayv2 get-apis"
fi

# ── EventBridge Scheduler ─────────────────────────────────────────────────────

echo ""
echo "── EventBridge Scheduler"
check_match "schedule ${PREFIX}-Heartbeat exists" "${PREFIX}-Heartbeat" \
  $CLI scheduler list-schedules

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="

[ "$FAIL" -eq 0 ]
