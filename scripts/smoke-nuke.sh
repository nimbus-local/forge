#!/usr/bin/env bash
# smoke-nuke.sh — forcefully removes all Pulumi state + S3 buckets for a given
# app prefix from local Nimbus. Run this after Nimbus restarts to eliminate the
# "resource not found" errors caused by Pulumi state referencing resources that
# no longer exist.
#
# Usage:
#   ./scripts/smoke-nuke.sh [PREFIX]
#
#   PREFIX defaults to "forge-smoke". Pass a different prefix to target
#   another app (e.g. "my-app-dev").
#
# Requires AWS_ENDPOINT_URL (or falls back to http://localhost:4566).
# Standard AWS env vars (AWS_ACCESS_KEY_ID etc.) must be set for Nimbus auth.

set -euo pipefail

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-us-east-1}"
PREFIX="${1:-forge-smoke}"

AWS_CMD="aws --endpoint-url $ENDPOINT --region $REGION --no-cli-pager --output text"

# Verify Nimbus is reachable before doing anything.
if ! curl -sf "${ENDPOINT}/_nimbus/health" >/dev/null 2>&1; then
    echo "error: Nimbus is not running at ${ENDPOINT}" >&2
    echo "       Start it with: cd ~/source/nimbus-local/nimbus && docker compose up -d" >&2
    exit 1
fi

echo "=== Nuking all ${PREFIX}-* S3 buckets on Nimbus (${ENDPOINT}) ==="

buckets=$($AWS_CMD s3api list-buckets \
    --query "Buckets[?starts_with(Name, \`${PREFIX}-\`)].Name" 2>/dev/null || echo "")

if [ -z "$buckets" ] || [ "$buckets" = "None" ]; then
    echo "  No matching buckets found — nothing to do."
    exit 0
fi

count=0
for bucket in $buckets; do
    echo "  s3://${bucket}"
    $AWS_CMD s3 rb "s3://${bucket}" --force 2>/dev/null || true
    count=$((count + 1))
done

echo ""
echo "=== Removed ${count} bucket(s). Re-run 'make smoke' for a fresh deploy. ==="
