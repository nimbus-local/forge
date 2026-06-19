#!/usr/bin/env bash
# smoke-nuke.sh — forcefully removes ALL Pulumi state and AWS resources for a
# given app prefix from local Nimbus so that `make smoke` can be re-run cleanly
# after a dirty or partial exit.
#
# Usage:
#   ./scripts/smoke-nuke.sh [PREFIX]
#
#   PREFIX defaults to "forge-smoke". Pass a different prefix to target
#   another app (e.g. "my-app-dev").
#
# Requires Nimbus running at AWS_ENDPOINT_URL (default http://localhost:4567).
# Standard AWS env vars (AWS_ACCESS_KEY_ID etc.) must be set for Nimbus auth.

set -euo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4567}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
PREFIX="${1:-forge-smoke}"

AWS="aws --endpoint-url $ENDPOINT --region $REGION --no-cli-pager"

# Verify Nimbus is reachable before doing anything.
if ! curl -sf "${ENDPOINT}/_nimbus/health" >/dev/null 2>&1; then
    echo "error: Nimbus is not running at ${ENDPOINT}" >&2
    echo "       Start it with: make smoke-up" >&2
    exit 1
fi

deleted=0

# ── helper ────────────────────────────────────────────────────────────────────

nuke() {
    local label="$1"; shift
    echo "  $label"
    if "$@" 2>/dev/null; then
        deleted=$((deleted + 1))
    fi
}

# ── S3 buckets (Pulumi state + assets) ───────────────────────────────────────

echo "=== Nuking ${PREFIX}-* resources on Nimbus (${ENDPOINT}) ==="
echo ""
echo "── S3 buckets"

buckets=$($AWS --output text s3api list-buckets \
    --query "Buckets[?starts_with(Name, \`${PREFIX}-\`)].Name" 2>/dev/null || true)

for bucket in $buckets; do
    nuke "s3://${bucket}" $AWS s3 rb "s3://${bucket}" --force
done

# ── CloudWatch log groups ─────────────────────────────────────────────────────

echo ""
echo "── CloudWatch log groups"

log_groups=$($AWS --output text logs describe-log-groups \
    --log-group-name-prefix "/aws/lambda/${PREFIX}-" \
    --query "logGroups[].logGroupName" 2>/dev/null || true)

for lg in $log_groups; do
    nuke "$lg" $AWS logs delete-log-group --log-group-name "$lg"
done

# ── SNS topics ────────────────────────────────────────────────────────────────

echo ""
echo "── SNS topics"

topic_arns=$($AWS --output text sns list-topics \
    --query "Topics[].TopicArn" 2>/dev/null || true)

for arn in $topic_arns; do
    # ARN format: arn:aws:sns:<region>:<account>:<name>
    topic_name="${arn##*:}"
    if [[ "$topic_name" == ${PREFIX}-* ]]; then
        nuke "$topic_name" $AWS sns delete-topic --topic-arn "$arn"
    fi
done

# ── SQS queues ────────────────────────────────────────────────────────────────

echo ""
echo "── SQS queues"

queue_urls=$($AWS --output text sqs list-queues \
    --queue-name-prefix "${PREFIX}-" \
    --query "QueueUrls[]" 2>/dev/null || true)

for url in $queue_urls; do
    queue_name="${url##*/}"
    nuke "$queue_name" $AWS sqs delete-queue --queue-url "$url"
done

# ── DynamoDB tables ───────────────────────────────────────────────────────────

echo ""
echo "── DynamoDB tables"

tables=$($AWS --output text dynamodb list-tables \
    --query "TableNames[?starts_with(@, \`${PREFIX}-\`)]" 2>/dev/null || true)

for table in $tables; do
    nuke "$table" $AWS dynamodb delete-table --table-name "$table"
done

# ── Lambda functions ──────────────────────────────────────────────────────────

echo ""
echo "── Lambda functions"

functions=$($AWS --output text lambda list-functions \
    --query "Functions[?starts_with(FunctionName, \`${PREFIX}-\`)].FunctionName" 2>/dev/null || true)

for fn in $functions; do
    nuke "$fn" $AWS lambda delete-function --function-name "$fn"
done

# ── Step Functions state machines ────────────────────────────────────────────

echo ""
echo "── Step Functions state machines"

sfn_arns=$($AWS --output text stepfunctions list-state-machines \
    --query "stateMachines[?starts_with(name, \`${PREFIX}-\`)].stateMachineArn" 2>/dev/null || true)

for arn in $sfn_arns; do
    name="${arn##*:}"
    nuke "$name" $AWS stepfunctions delete-state-machine --state-machine-arn "$arn"
done

# ── Kinesis streams ───────────────────────────────────────────────────────────

echo ""
echo "── Kinesis streams"

streams=$($AWS --output text kinesis list-streams \
    --query "StreamNames[?starts_with(@, \`${PREFIX}-\`)]" 2>/dev/null || true)

for stream in $streams; do
    nuke "$stream" $AWS kinesis delete-stream --stream-name "$stream"
done

# ── EventBridge Scheduler schedules ──────────────────────────────────────────

echo ""
echo "── EventBridge Scheduler schedules"

schedules=$($AWS --output text scheduler list-schedules \
    --query "Schedules[?starts_with(Name, \`${PREFIX}-\`)].Name" 2>/dev/null || true)

for sched in $schedules; do
    nuke "$sched" $AWS scheduler delete-schedule --name "$sched"
done

# ── Summary ───────────────────────────────────────────────────────────────────

echo ""
echo "=== Removed ${deleted} resource(s). Re-run 'make smoke' for a fresh deploy. ==="
