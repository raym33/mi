# DePIN settlement and rewards

`mi` includes a local settlement chain for city-scale and rented-compute deployments.

The goal is not to put prompts on a public blockchain. The goal is to make usage, consumer debits, provider rewards, and audit state tamper-evident.

## What gets recorded

For every successful inference, the coordinator can append one settlement event:

- request ID
- consumer ID
- provider ID
- node ID
- model alias
- privacy tier
- prompt, completion, and total tokens estimated by the coordinator
- latency and dispatch attempts
- consumer debit in micros
- provider reward in micros
- latency penalty in micros, when configured
- previous event hash
- current event hash

Prompt bodies are not recorded.

The coordinator does not use worker-reported token counts for accounting. It estimates prompt usage from the request it received and completion usage from streamed chunks it relayed using a simple rune-count heuristic. This prevents a provider node from simply inflating `prompt_tokens` or `completion_tokens` in its final `done` message. The current estimate is intentionally simple and should be replaced by model-family tokenizers before real money settlement.

## Hash-chain design

Events are written to a JSONL file. Each event includes the previous event hash and its own SHA-256 hash. If someone modifies, deletes, or reorders an event, verification fails.

This is intentionally a small local chain rather than a public token chain. It gives operators a clean path to:

- export invoices
- calculate provider payouts
- audit consumer usage
- detect tampering
- later anchor the latest hash to Ethereum, Solana, a rollup, or another settlement layer

## Configuration

```yaml
settlement:
  enabled: true
  chain_path: "data/settlement-chain.jsonl"
  price_per_thousand_tokens_micros: 1000
  provider_reward_share_bps: 7000
  target_latency_ms: 5000
  latency_penalty_bps: 1000
```

Fields:

- `enabled`: turns settlement events on or off.
- `chain_path`: append-only JSONL chain path.
- `price_per_thousand_tokens_micros`: consumer debit rate per 1,000 tokens.
- `provider_reward_share_bps`: provider share of the debit, in basis points.
- `target_latency_ms`: soft SLA target for successful requests.
- `latency_penalty_bps`: provider reward reduction when a successful request exceeds the target latency.

Example: if coordinator-estimated total tokens are `1000`, price is `1000` micros, and provider share is `7000`, the consumer debit is `1000` micros and the base provider reward is `700` micros. If the request exceeds `target_latency_ms` and `latency_penalty_bps` is `1000`, the provider reward is reduced by 10%.

## Admin endpoints

Inspect settlement state:

```bash
curl http://localhost:8080/admin/settlement \
  -H 'Authorization: Bearer admin-dev-token'
```

Limit recent events:

```bash
curl 'http://localhost:8080/admin/settlement?limit=5' \
  -H 'Authorization: Bearer admin-dev-token'
```

Verify the hash chain:

```bash
curl http://localhost:8080/admin/settlement/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

A healthy chain returns:

```json
{
  "valid": true,
  "events": 12,
  "last_hash": "..."
}
```

Inspect provider reputation:

```bash
curl http://localhost:8080/admin/reputation \
  -H 'Authorization: Bearer admin-dev-token'
```

The first reputation model uses objective local signals:

- active and healthy node count
- cooldown status
- recent node error streaks
- completed settlement events
- total served tokens
- accrued provider rewards
- disabled provider state
- benchmark challenge pass rate and score

The coordinator refreshes provider scores on a background interval and when the admin reputation endpoint is inspected, keeping reputation recomputation out of the per-request hot path. The scheduler uses those scores plus coordinator-observed latency, TTFT, estimated tokens/sec, and node failure rate as routing penalties, so providers with weak challenge history, error streaks, cooldowns, penalties, slow responses, or poor reliability are less likely to receive traffic when healthier eligible providers exist.

This is intentionally off-chain and explainable. Later versions can add benchmarking, signed attestations, challenge jobs, staking, slashing, and public dashboards.

## Benchmark challenges

Challenge events are a separate tamper-evident chain for provider benchmarking and anti-farming signals.

```yaml
challenges:
  enabled: true
  path: "data/challenge-chain.jsonl"
  auto_run: true
  interval: "15m"
  timeout: "30s"
  model: "llama3.1:8b"
  provider_id: ""
  privacy_tier: "public"
  prompt: "Reply with exactly: mi-ok"
  expected_contains: "mi-ok"
  max_tokens: 8
```

Record a manual challenge result:

```bash
curl -X POST http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "provider_id": "ray-home",
    "node_id": "ray-home-node",
    "challenge": "latency-smoke",
    "passed": true,
    "latency_ms": 420,
    "score": 94,
    "notes": "short synthetic prompt"
  }'
```

Run one synthetic challenge immediately:

```bash
curl -X POST http://localhost:8080/admin/challenges/run \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "llama3.1:8b",
    "expected_contains": "mi-ok"
  }'
```

Target a specific provider during onboarding or dispute review:

```bash
curl -X POST http://localhost:8080/admin/challenges/run \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "llama3.1:8b",
    "provider_id": "ray-home",
    "expected_contains": "mi-ok"
  }'
```

Inspect and verify challenges:

```bash
curl http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token'

curl http://localhost:8080/admin/challenges/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Challenge summaries feed provider reputation. Providers with weak pass rates or low challenge scores receive lower reputation until their later performance improves. When `provider_id` is omitted, the synthetic runner rotates across eligible providers instead of always testing the cheapest current node. Synthetic challenge requests use normal chat-shaped request IDs and avoid obvious benchmark wording, but true indistinguishability still requires mixing verification into real traffic. Synthetic challenge records store pass/fail, score, node, provider, latency, and hash links, not model output.

## Integrity anchor

Export a combined integrity manifest before invoicing, payout review, or external anchoring:

```bash
curl http://localhost:8080/admin/integrity \
  -H 'Authorization: Bearer admin-dev-token'
```

The response includes settlement verification, challenge verification, event counts, latest hashes, and one `anchor_hash` over the compact manifest. Publish that `anchor_hash` to an external timestamping system, public chain, signed transparency log, or release artifact. Later, the operator can recompute the same manifest from local chain files and prove whether payout inputs changed.

## Payment roadmap

The current settlement layer is cooperative accounting, not a payment processor or trustless proof-of-inference system.

Recommended path:

1. Internal credits: consumers buy token credits, providers accrue rewards.
2. Invoice export: operator pays providers off-platform.
3. Stablecoin payout: map provider balances to wallet addresses.
4. On-chain anchoring: periodically publish `/admin/integrity` `anchor_hash` to a public chain.
5. Slashing and disputes: add signed provider claims, uptime proofs, automated challenge windows, and provider bonds.
6. Stronger verification: add TEEs, signed node attestations, redaction, or proof-of-inference as those techniques mature.

## Security model

The chain proves that recorded settlement events were not tampered with after recording. It does not prove that:

- the model output was correct
- a provider did not inspect a public prompt it received
- the hardware is confidential
- the operator cannot choose not to record an event

Those require additional controls such as mTLS, provider reputation, signed node binaries, trusted execution environments, external anchoring, or cryptographic inference proofs.
