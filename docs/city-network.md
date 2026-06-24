# City network mode

City mode turns `mi` from a private LAN cluster into a small shared inference network for a neighborhood, coworking space, school, or city group.

It introduces four primitives:

- Consumers: people or teams allowed to call the OpenAI-compatible API.
- Providers: people contributing Mac compute.
- Provider tokens: shared secrets that let a node join under a provider account.
- Usage accounting: requests and coordinator-estimated tokens are counted for both the consumer and the provider.
- Consumer quotas: each API key can belong to an account with a token limit.
- Quota reservations: estimated request budget is reserved before dispatch, then reconciled with coordinator-estimated usage when the request finishes.
- Settlement chain: successful inference can create tamper-evident debit and reward events.
- SLA penalties: settlement can reduce provider rewards when successful requests exceed a configured latency target.
- Persistent local usage: usage survives coordinator restarts when `sqlite_path` or legacy `usage_store_path` is configured.
- Privacy tiers: private prompts stay on trusted nodes, while public prompts can use rented provider capacity.

This is not a payment system yet. It is the base ledger needed before payouts and prepaid credits.

## Start a city coordinator

```bash
go run ./coordinator/cmd/coordinator -config configs/coordinator.city.example.yaml
```

Or:

```bash
make run-city-coordinator
```

For HTTPS/WSS transport:

```bash
make dev-certs
make run-city-coordinator-tls
```

The TLS example also enables node mTLS, so provider nodes need `certs/node.crt` and `certs/node.key`.

## Join as a provider

On a Mac with Ollama running:

```bash
ollama pull llama3.1:8b
go run ./node-agent/cmd/node-agent -config configs/node-agent.city.example.yaml
```

Or:

```bash
make run-city-node
```

For WSS transport:

```bash
make run-city-node-tls
```

For another provider, copy `configs/node-agent.city.example.yaml` and change:

- `provider_id`
- `provider_token`
- `public_name`
- `coordinator_url`
- `privacy_mode`

Use `privacy_mode: "public"` for a rented node that should only receive non-sensitive public prompts. Keep `privacy_mode: "private"` for trusted machines that may handle private prompts.

## Enroll accounts dynamically

Static YAML accounts are fine for development. For a real city network, create accounts through the admin API so secrets are returned once and only SHA-256 hashes are stored in `data/city-usage.json`.

Create a consumer:

```bash
curl http://localhost:8080/admin/consumers \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "studio-b",
    "display_name": "Studio B",
    "total_token_limit": 250000
  }'
```

The response includes `api_key`. Give that to the consumer once.

Create a provider:

```bash
curl http://localhost:8080/admin/providers \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "neighbor-mac",
    "display_name": "Neighbor Mac Studio",
    "privacy_mode": "public"
  }'
```

The response includes `provider_token`. Put it in the node config:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
privacy_mode: "public"
```

There is also a helper script:

```bash
CONSUMER_ID=studio-b make city-enroll
PROVIDER_ID=neighbor-mac make city-enroll
PROVIDER_PRIVACY_MODE=public PROVIDER_ID=neighbor-mac make city-enroll
```

Rotate a consumer API key:

```bash
curl -X POST http://localhost:8080/admin/consumers/studio-b/rotate-key \
  -H 'Authorization: Bearer admin-dev-token'
```

Rotate a provider token:

```bash
curl -X POST http://localhost:8080/admin/providers/neighbor-mac/rotate-token \
  -H 'Authorization: Bearer admin-dev-token'
```

Disable a consumer:

```bash
curl -X DELETE http://localhost:8080/admin/consumers/studio-b \
  -H 'Authorization: Bearer admin-dev-token'
```

Disable a provider and disconnect its active nodes:

```bash
curl -X DELETE http://localhost:8080/admin/providers/neighbor-mac \
  -H 'Authorization: Bearer admin-dev-token'
```

The helper supports these operations too:

```bash
ACTION=rotate CONSUMER_ID=studio-b make city-enroll
ACTION=disable PROVIDER_ID=neighbor-mac make city-enroll
```

## Call as a consumer

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "fast",
    "privacy_tier": "public",
    "messages": [{"role": "user", "content": "Explain what this city AI network is in one sentence"}],
    "stream": true
  }'
```

If `privacy_tier` is omitted, the coordinator uses `private`. You can also send `X-Mi-Privacy-Tier: private`, `community`, or `public`.

Optional hardware hints can route requests to a specific class of node:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -H 'X-Mi-Accelerator: cuda' \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Use a CUDA-capable node if available"}]
  }'
```

Body fields are also supported: `mi_backend`, `mi_device_kind`, `mi_soc`, and `mi_accelerators`.

Provider privacy is enforced by the coordinator account policy. If a provider account is configured as `public`, its node cannot receive private work even if the local node-agent config claims `privacy_mode: "private"`.

Model aliases are configured under `models.aliases`. For example, `fast` can point to `llama3.1:8b`, while a future `code` alias can point to a coding model. The OpenAI-compatible model list only shows aliases whose concrete target is currently available on at least one healthy node.

Inspect the richer catalog:

```bash
curl http://localhost:8080/v1/models/catalog \
  -H 'Authorization: Bearer sk-mi-studio-a-dev'
```

## Inspect usage

```bash
curl http://localhost:8080/admin/city \
  -H 'Authorization: Bearer admin-dev-token'
```

Inspect settlement rewards and verify the hash chain:

```bash
curl http://localhost:8080/admin/settlement \
  -H 'Authorization: Bearer admin-dev-token'

curl http://localhost:8080/admin/settlement/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Inspect provider reputation:

```bash
curl http://localhost:8080/admin/reputation \
  -H 'Authorization: Bearer admin-dev-token'
```

Record and inspect benchmark challenges:

```bash
curl -X POST http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "provider_id": "ray-home",
    "challenge": "latency-smoke",
    "passed": true,
    "latency_ms": 420,
    "score": 94
  }'

curl http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token'
```

Run a synthetic challenge against the active network:

```bash
curl -X POST http://localhost:8080/admin/challenges/run \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{"model":"llama3.1:8b","expected_contains":"mi-ok"}'
```

Add `"provider_id":"ray-home"` to target a specific provider. Without `provider_id`, the runner rotates across eligible providers.

The example config writes city state to `data/mi-city.db` with SQLite/WAL. Legacy JSON state is still supported through `usage_store_path`.
The example settlement ledger writes hash-chained events to `data/mi-settlement.db` with SQLite/WAL. Legacy JSONL chains are still supported through `chain_path`. Back it up and periodically anchor its latest hash externally if rewards represent real money.
The example challenge chain writes to `data/challenge-chain.jsonl`.
Generated API keys and provider tokens are not stored in plaintext; only hashes are persisted.

Consumers can inspect their own account and remaining quota:

```bash
curl http://localhost:8080/v1/me \
  -H 'Authorization: Bearer sk-mi-studio-a-dev'
```

Anyone can inspect public network capacity:

```bash
curl http://localhost:8080/network/status
```

With TLS enabled:

```bash
curl --cacert certs/ca.crt https://localhost:8443/network/status
```

Run the full city smoke test:

```bash
make city-smoke
```

## What this unlocks

- A local AI cooperative where people contribute idle Macs.
- Shared inference for small businesses without sending prompts to a cloud API.
- Internal credits or payouts later, based on coordinator-estimated provider token contribution.
- Public rented capacity for non-sensitive work, with private work pinned to trusted nodes.
- Tamper-evident settlement logs for later payouts, invoices, or on-chain anchoring.
- Public endpoint on a city VPN, Tailscale network, or reverse proxy.
- Fair usage limits for schools, coworking spaces, and local AI clubs.
- Automatic failover before the first token when a provider node fails to start a request.
- Cooldowns for nodes that repeatedly fail before generating, so unstable machines stop absorbing traffic until they recover.

## Next hardening steps

- Replace static provider tokens with enrollment links.
- Add temporary enrollment links and one-command node join.
- Add Postgres as an optional multi-coordinator store for larger networks.
- Add quotas and prepaid credits.
- Add TLS/mTLS.
- Add mTLS for node-only endpoints.
- Add provider reputation and uptime scoring.
- Add pricing rules and invoice exports for rented capacity.
- Add request retry before first token.
- Add optional coordinator-to-provider encryption and stronger confidential-compute options.
