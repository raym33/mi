#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
MODEL="${MODEL:-fast}"

curl -fsS "$BASE_URL/v1/models"
printf '\n'

response="$(curl -fsS "$BASE_URL/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"$MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Reply with one short sentence from mi.\"}],
    \"stream\": true
  }")"
printf '%s\n' "$response"
if grep -q '"error"' <<<"$response"; then
  exit 1
fi
printf '\n'
