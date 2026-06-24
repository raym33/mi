#!/usr/bin/env bash
set -euo pipefail

# Self-contained demo: boots a coordinator and a node-agent with the dependency-
# free `echo` backend (no Ollama / GPU needed), runs a real chat completion over
# the OpenAI-compatible API, then shows the operator surface (metrics + payout
# CSV) reflecting the settlement that just happened.
#
#   make demo   (or)   bash scripts/demo.sh

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

BASE_URL="http://localhost:8088"
API_KEY="sk-mi-demo"
ADMIN_TOKEN="demo-admin-token"

# Fresh state each run so numbers are easy to read.
rm -rf data/demo
mkdir -p data/demo bin

echo "== building =="
go build -o bin/coordinator ./coordinator/cmd/coordinator
go build -o bin/node-agent ./node-agent/cmd/node-agent

cleanup() {
  [ -n "${NODE_PID:-}" ] && kill "$NODE_PID" 2>/dev/null || true
  [ -n "${COORD_PID:-}" ] && kill -TERM "$COORD_PID" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT

echo "== starting coordinator on :8088 =="
bin/coordinator -config configs/coordinator.demo.yaml >/tmp/mi-demo-coordinator.log 2>&1 &
COORD_PID=$!

# Wait for the coordinator to answer health checks.
for _ in $(seq 1 50); do
  if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then break; fi
  sleep 0.1
done

echo "== starting echo node-agent =="
bin/node-agent -config configs/node-agent.demo.yaml >/tmp/mi-demo-node.log 2>&1 &
NODE_PID=$!

# Wait for the node to register and advertise the model.
for _ in $(seq 1 50); do
  if curl -fsS "$BASE_URL/network/status" | grep -q "demo-model"; then break; fi
  sleep 0.1
done

echo ""
echo "== network status =="
curl -fsS "$BASE_URL/network/status"
printf '\n\n'

echo "== chat completion (OpenAI-compatible) =="
curl -fsS "$BASE_URL/v1/chat/completions" \
  -H "Authorization: Bearer $API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "demo-model",
    "privacy_tier": "private",
    "messages": [{"role": "user", "content": "hello mi cooperative"}],
    "stream": false
  }'
printf '\n\n'

echo "== admin metrics (Prometheus) =="
curl -fsS "$BASE_URL/admin/metrics" -H "Authorization: Bearer $ADMIN_TOKEN" \
  | grep -E '^mi_(settlement_events_total|provider_reward_micros|nodes) ' || true
printf '\n'

echo "== provider payout CSV =="
curl -fsS "$BASE_URL/admin/payouts.csv" -H "Authorization: Bearer $ADMIN_TOKEN"
printf '\n'

echo "== integrity anchor =="
curl -fsS "$BASE_URL/admin/integrity" -H "Authorization: Bearer $ADMIN_TOKEN"
printf '\n\n'

echo "Demo complete. Coordinator log: /tmp/mi-demo-coordinator.log  Node log: /tmp/mi-demo-node.log"
