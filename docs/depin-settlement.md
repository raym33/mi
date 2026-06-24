# DePIN Settlement And Rewards

`mi` includes a cooperative settlement layer for city-scale and rented-compute deployments.

The current goal is not to create a public token chain. The goal is to produce local, reviewable, tamper-evident records for usage, consumer debits, provider rewards, benchmark evidence, and later external anchoring.

## Current Status

Implemented:

- Settlement events for successful inference requests.
- Consumer debit calculation.
- Provider reward calculation.
- Optional latency/SLA penalty.
- Hash-linked event chain.
- SQLite/WAL storage through `settlement.sqlite_path`.
- Legacy append-only JSONL storage through `settlement.chain_path`.
- Verification endpoint for the settlement chain.
- Benchmark challenge chain and verification endpoint.
- Combined `/admin/integrity` manifest with one `anchor_hash`.
- Provider reputation from settlement, health, cooldown, error, reward, penalty, and challenge signals.

Not implemented yet:

- Wallets.
- Stablecoin or fiat payouts.
- On-chain transactions.
- Provider staking or slashing.
- Exact model-family tokenizers.
- Signed provider receipts.
- Proof-of-inference.
- A dispute workflow.

## What Gets Recorded

For every successful inference, the coordinator can record one settlement event:

- Event index.
- Record timestamp.
- Request ID.
- Consumer ID.
- Provider ID.
- Node ID.
- Model ID or alias.
- Privacy tier.
- Prompt tokens estimated by the coordinator.
- Completion tokens estimated by the coordinator.
- Total tokens.
- Latency in milliseconds.
- Dispatch attempts.
- Consumer debit in micros.
- Provider reward in micros.
- Provider latency penalty in micros, when configured.
- Previous event hash.
- Current event hash.

Prompt bodies and model outputs are not recorded in settlement events.

## Accounting Model

The coordinator does not trust worker-reported token counts for billing. It estimates prompt usage from the request it received and completion usage from streamed chunks it relayed.

Today the estimate is intentionally simple:

- Text is counted as Unicode runes.
- Estimated tokens are roughly `runes / 4`, rounded up.
- Requests that omit `max_tokens` receive a default output cap before dispatch.
- Quota-limited consumers reserve an estimated budget before dispatch and reconcile when the request finishes.

This reduces simple provider-side token inflation, but it is not exact tokenization. Real-money deployments should add model-family tokenizers and clear dispute rules.

## Hash-Chain Design

Each settlement event includes:

- `previous_hash`
- `hash`

The event hash is computed from the event contents. Verification recomputes the chain in order.

This detects:

- Editing a recorded event.
- Reordering events.
- Removing an event from the middle while later events remain.
- Changing token counts, prices, rewards, latency, or account IDs after the fact.

This does not detect by itself:

- A request that was never recorded.
- Deleting the entire database.
- Truncating the tail of the chain if there is no external anchor.
- A malicious operator intentionally choosing not to run settlement.
- Whether the model output was correct.
- Whether a provider inspected a prompt it received.

For payouts, back up the stores and periodically publish `/admin/integrity` `anchor_hash` to an external timestamping service, public chain, signed transparency log, or release artifact.

## Configuration

Recommended city deployment:

```yaml
settlement:
  enabled: true
  sqlite_path: "data/mi-settlement.db"
  price_per_thousand_tokens_micros: 1000
  provider_reward_share_bps: 7000
  target_latency_ms: 5000
  latency_penalty_bps: 1000
```

Legacy JSONL mode:

```yaml
settlement:
  enabled: true
  chain_path: "data/settlement-chain.jsonl"
```

Fields:

- `enabled`: records settlement events when true.
- `sqlite_path`: SQLite/WAL store path. Recommended for city deployments.
- `chain_path`: legacy append-only JSONL chain path, used when `sqlite_path` is empty.
- `price_per_thousand_tokens_micros`: consumer debit rate per 1,000 estimated tokens.
- `provider_reward_share_bps`: provider share of the debit, in basis points.
- `target_latency_ms`: soft SLA target for successful requests.
- `latency_penalty_bps`: provider reward reduction when a successful request exceeds the target latency.

Example:

- Total estimated tokens: `1000`
- Price: `1000` micros per 1,000 tokens
- Provider share: `7000` bps, or 70%
- Consumer debit: `1000` micros
- Base provider reward: `700` micros
- If latency exceeds the target and penalty is `1000` bps, provider reward is reduced by 10%

## Admin Endpoints

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

Verify the settlement chain:

```bash
curl http://localhost:8080/admin/settlement/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Example healthy response:

```json
{
  "valid": true,
  "events": 12,
  "last_hash": "..."
}
```

Export the combined integrity manifest:

```bash
curl http://localhost:8080/admin/integrity \
  -H 'Authorization: Bearer admin-dev-token'
```

The integrity response binds:

- Settlement verification result.
- Challenge verification result.
- Settlement event count.
- Challenge event count.
- Latest settlement hash.
- Latest challenge hash.
- One compact `anchor_hash`.

Publish `anchor_hash` externally before invoice runs, payout reviews, or dispute windows.

## Provider Reputation

Inspect provider reputation:

```bash
curl http://localhost:8080/admin/reputation \
  -H 'Authorization: Bearer admin-dev-token'
```

The current reputation model uses explainable local signals:

- Healthy active node count.
- Cooldown state.
- Recent node error streaks.
- Completed settlement events.
- Total served tokens.
- Accrued provider rewards.
- Provider SLA penalties.
- Disabled provider state.
- Benchmark challenge pass rate.
- Benchmark challenge score.

The coordinator refreshes provider scores in the background and when the admin reputation endpoint is inspected. Scores are passed to the scheduler without recomputing the full settlement history on every request.

The scheduler combines reputation with coordinator-observed node metrics:

- Latency.
- Time to first token.
- Estimated tokens per second.
- Failure rate.
- Queue and capacity pressure.

Poor reliability, challenge failures, cooldowns, and slow responses therefore reduce future routing priority.

## Benchmark Challenges

Challenge events live in a separate tamper-evident chain.

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

Record a manual challenge:

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

Run one synthetic challenge:

```bash
curl -X POST http://localhost:8080/admin/challenges/run \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "llama3.1:8b",
    "expected_contains": "mi-ok"
  }'
```

Target one provider:

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

Inspect and verify:

```bash
curl http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token'

curl http://localhost:8080/admin/challenges/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Challenge summaries feed provider reputation. Synthetic challenge requests use normal chat-shaped request IDs and avoid obvious benchmark wording, but true anti-farming requires mixing verification into real traffic, richer challenge sets, and dispute handling. Treat the current runner as operational evidence, not a trustless proof system.

## Payment Path

The current ledger is enough for cooperative accounting:

1. Consumers use API keys with quotas.
2. Providers accrue estimated rewards.
3. The operator reviews `/admin/settlement`, `/admin/reputation`, and `/admin/integrity`.
4. The operator exports invoices or pays providers off-platform.
5. The operator anchors `anchor_hash` externally before finalizing a payout period.

Future payment work should add:

- Pricing rules per model, provider, privacy tier, and latency class.
- Consumer prepaid credits.
- Provider payout reports.
- Wallet addresses.
- Stablecoin or fiat payout export.
- Dispute windows.
- Signed provider receipts.
- Slashing or bond mechanisms for adversarial networks.

## Security Model

Settlement proves only that recorded events have not changed relative to the local hash chain and any external anchors.

It does not prove:

- The provider ran the claimed model correctly.
- The provider did not inspect the prompt.
- The hardware was confidential.
- The operator recorded every eligible request.
- The token estimate matched the exact model tokenizer.

For higher-trust or real-money deployments, combine settlement with mTLS, backups, external anchoring, model-specific tokenizers, signed releases, provider reputation, dispute workflows, and stronger private-compute techniques.
