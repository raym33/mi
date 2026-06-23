#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-admin-dev-token}"
CONSUMER_ID="${CONSUMER_ID:-}"
PROVIDER_ID="${PROVIDER_ID:-}"

if [[ -n "$CONSUMER_ID" ]]; then
  curl -fsS "$BASE_URL/admin/consumers" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{
      \"id\": \"$CONSUMER_ID\",
      \"display_name\": \"${CONSUMER_NAME:-$CONSUMER_ID}\",
      \"total_token_limit\": ${TOTAL_TOKEN_LIMIT:-250000}
    }"
  printf '\n'
fi

if [[ -n "$PROVIDER_ID" ]]; then
  curl -fsS "$BASE_URL/admin/providers" \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{
      \"id\": \"$PROVIDER_ID\",
      \"display_name\": \"${PROVIDER_NAME:-$PROVIDER_ID}\"
    }"
  printf '\n'
fi

if [[ -z "$CONSUMER_ID" && -z "$PROVIDER_ID" ]]; then
  cat <<'USAGE'
Set CONSUMER_ID or PROVIDER_ID.

Examples:
  CONSUMER_ID=studio-b ./scripts/city-enroll.sh
  PROVIDER_ID=neighbor-mac ./scripts/city-enroll.sh
USAGE
fi
