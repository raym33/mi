#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-sk-mi-studio-a-dev}"
ADMIN_TOKEN="${ADMIN_TOKEN:-admin-dev-token}"
MODEL="${MODEL:-fast}"

echo "== public network status =="
curl -fsS "$BASE_URL/network/status"
printf '\n\n'

echo "== consumer account =="
curl -fsS "$BASE_URL/v1/me" \
  -H "Authorization: Bearer $API_KEY"
printf '\n\n'

echo "== inference =="
curl -fsS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"$MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Reply in one short sentence from the mi city network.\"}],
    \"stream\": true
  }"
printf '\n\n'

echo "== city ledger =="
curl -fsS "$BASE_URL/admin/city" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
printf '\n'
