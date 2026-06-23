#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-admin-dev-token}"
CONSUMER_ID="${CONSUMER_ID:-}"
PROVIDER_ID="${PROVIDER_ID:-}"
ACTION="${ACTION:-create}"

if [[ -n "$CONSUMER_ID" ]]; then
  case "$ACTION" in
    create)
      curl -fsS "$BASE_URL/admin/consumers" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H 'Content-Type: application/json' \
        -d "{
          \"id\": \"$CONSUMER_ID\",
          \"display_name\": \"${CONSUMER_NAME:-$CONSUMER_ID}\",
          \"total_token_limit\": ${TOTAL_TOKEN_LIMIT:-250000}
        }"
      ;;
    rotate)
      curl -fsS -X POST "$BASE_URL/admin/consumers/$CONSUMER_ID/rotate-key" \
        -H "Authorization: Bearer $ADMIN_TOKEN"
      ;;
    disable)
      curl -fsS -X DELETE "$BASE_URL/admin/consumers/$CONSUMER_ID" \
        -H "Authorization: Bearer $ADMIN_TOKEN"
      ;;
    *)
      echo "unknown ACTION=$ACTION" >&2
      exit 2
      ;;
  esac
  printf '\n'
fi

if [[ -n "$PROVIDER_ID" ]]; then
  case "$ACTION" in
    create)
      curl -fsS "$BASE_URL/admin/providers" \
        -H "Authorization: Bearer $ADMIN_TOKEN" \
        -H 'Content-Type: application/json' \
        -d "{
          \"id\": \"$PROVIDER_ID\",
          \"display_name\": \"${PROVIDER_NAME:-$PROVIDER_ID}\",
          \"privacy_mode\": \"${PROVIDER_PRIVACY_MODE:-private}\"
        }"
      ;;
    rotate)
      curl -fsS -X POST "$BASE_URL/admin/providers/$PROVIDER_ID/rotate-token" \
        -H "Authorization: Bearer $ADMIN_TOKEN"
      ;;
    disable)
      curl -fsS -X DELETE "$BASE_URL/admin/providers/$PROVIDER_ID" \
        -H "Authorization: Bearer $ADMIN_TOKEN"
      ;;
    *)
      echo "unknown ACTION=$ACTION" >&2
      exit 2
      ;;
  esac
  printf '\n'
fi

if [[ -z "$CONSUMER_ID" && -z "$PROVIDER_ID" ]]; then
  cat <<'USAGE'
Set CONSUMER_ID or PROVIDER_ID.

Examples:
  CONSUMER_ID=studio-b ./scripts/city-enroll.sh
  PROVIDER_ID=neighbor-mac ./scripts/city-enroll.sh
  PROVIDER_PRIVACY_MODE=public PROVIDER_ID=neighbor-mac ./scripts/city-enroll.sh
  ACTION=rotate CONSUMER_ID=studio-b ./scripts/city-enroll.sh
  ACTION=disable PROVIDER_ID=neighbor-mac ./scripts/city-enroll.sh
USAGE
fi
