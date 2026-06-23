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
- prompt, completion, and total tokens
- consumer debit in micros
- provider reward in micros
- previous event hash
- current event hash

Prompt bodies are not recorded.

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
```

Fields:

- `enabled`: turns settlement events on or off.
- `chain_path`: append-only JSONL chain path.
- `price_per_thousand_tokens_micros`: consumer debit rate per 1,000 tokens.
- `provider_reward_share_bps`: provider share of the debit, in basis points.

Example: if total tokens are `1000`, price is `1000` micros, and provider share is `7000`, the consumer debit is `1000` micros and the provider reward is `700` micros.

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

## Payment roadmap

The current settlement layer is payment-ready accounting, not a payment processor.

Recommended path:

1. Internal credits: consumers buy token credits, providers accrue rewards.
2. Invoice export: operator pays providers off-platform.
3. Stablecoin payout: map provider balances to wallet addresses.
4. On-chain anchoring: periodically publish `last_hash` to a public chain.
5. Slashing and disputes: add signed provider claims, uptime proofs, and challenge windows.
6. Stronger verification: add TEEs, signed node attestations, redaction, or proof-of-inference as those techniques mature.

## Security model

The chain proves that recorded settlement events were not tampered with after recording. It does not prove that:

- the model output was correct
- a provider did not inspect a public prompt it received
- the hardware is confidential
- the operator cannot choose not to record an event

Those require additional controls such as mTLS, provider reputation, signed node binaries, trusted execution environments, external anchoring, or cryptographic inference proofs.
