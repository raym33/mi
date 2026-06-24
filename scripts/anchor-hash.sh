#!/usr/bin/env bash
set -euo pipefail

# Print the current integrity anchor hash from a running coordinator.
# The anchor hash binds the settlement ledger and the challenge ledger, so it
# is the single value worth publishing externally (e.g. to a timestamping
# service) as tamper-evident proof of the cooperative accounting state.
#
# Usage:
#   ADMIN_TOKEN=admin-dev-token bash scripts/anchor-hash.sh
#   BASE_URL=https://mi.example.local:8443 ADMIN_TOKEN=... bash scripts/anchor-hash.sh

BASE_URL="${BASE_URL:-http://localhost:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:?set ADMIN_TOKEN to the coordinator admin token}"

response="$(curl -fsS "$BASE_URL/admin/integrity" -H "Authorization: Bearer $ADMIN_TOKEN")"

if command -v jq >/dev/null 2>&1; then
  echo "$response" | jq -r '.anchor.anchor_hash'
else
  # Fallback without jq: extract the anchor_hash field.
  echo "$response" | grep -o '"anchor_hash":"[^"]*"' | head -n1 | cut -d'"' -f4
fi
