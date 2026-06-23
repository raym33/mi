#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
MODEL="${MODEL:-llama3.1:8b}"

curl "$BASE_URL/v1/models"
printf '\n'

curl "$BASE_URL/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d "{
    \"model\": \"$MODEL\",
    \"messages\": [{\"role\": \"user\", \"content\": \"Reply with one short sentence from mi.\"}],
    \"stream\": true
  }"
printf '\n'

