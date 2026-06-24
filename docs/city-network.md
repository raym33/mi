# City Network Mode

City mode turns `mi` into a small shared inference network for a neighborhood, coworking space, school, agency group, clinic network, or local AI cooperative.

It adds accounts, quotas, provider enrollment, privacy policy, durable state, settlement events, and admin visibility on top of the basic OpenAI-compatible endpoint.

## What Runs Today

City mode currently supports:

- Consumer accounts with API keys.
- Provider accounts with provider tokens.
- Static YAML accounts for development.
- Dynamic admin enrollment for consumers and providers.
- Rotation and disable operations for consumer keys and provider tokens.
- Per-consumer token limits.
- Pre-dispatch quota reservations for concurrent requests.
- Coordinator-estimated prompt and completion usage.
- Per-consumer and per-provider usage accounting.
- SQLite/WAL city state through `city.sqlite_path`.
- Legacy JSON city state through `city.usage_store_path`.
- Optional settlement ledger through `settlement.sqlite_path` or legacy `settlement.chain_path`.
- Optional challenge chain through `challenges.path`.
- Privacy tiers: `private`, `community`, and `public`.
- HTTPS/WSS and node mTLS examples.
- Admin JSON endpoints for nodes, city state, settlement, reputation, challenges, and integrity.

It does not yet include:

- A hosted SaaS control plane.
- A web dashboard.
- Built-in payment processing.
- Wallet management.
- Trustless proof of inference.
- Cryptographic privacy against the machine doing inference.

## Roles

Consumer:

- Receives an API key.
- Calls `/v1/chat/completions`.
- Can inspect its own usage and remaining quota through `/v1/me`.

Provider:

- Receives a provider token.
- Runs `node-agent` on a machine with a local inference backend.
- Earns usage/reward accounting when its nodes complete requests.

Coordinator operator:

- Runs the coordinator.
- Creates consumers and providers.
- Sets privacy policy and quotas.
- Reviews settlement, reputation, and challenge evidence.
- Backs up SQLite databases and anchors integrity hashes when rewards matter.

## Start The Example Network

Start Ollama and pull the example model on each provider machine:

```bash
ollama serve
ollama pull llama3.1:8b
```

Start the city coordinator:

```bash
make run-city-coordinator
```

Start one provider node:

```bash
make run-city-node
```

Run the smoke test:

```bash
make city-smoke
```

The default city config listens on `http://localhost:8080` and includes:

- Admin token: `admin-dev-token`
- Consumer API key: `sk-mi-studio-a-dev`
- Provider token: `pk-mi-ray-home-dev`
- Model alias: `fast` -> `llama3.1:8b`
- City state: `data/mi-city.db`
- Settlement state: `data/mi-settlement.db`
- Challenge chain: `data/challenge-chain.jsonl`

## Start With TLS And Node mTLS

Generate development certificates:

```bash
make dev-certs
```

Run the TLS coordinator:

```bash
make run-city-coordinator-tls
```

Run the TLS node:

```bash
make run-city-node-tls
```

Call the HTTPS endpoint:

```bash
curl --cacert certs/ca.crt https://localhost:8443/network/status
```

The TLS city example requires a client certificate for `/ws/node`, so only nodes with a certificate signed by `certs/ca.crt` can connect to the node WebSocket.

## Join A Provider Machine

On another Mac, copy `configs/node-agent.city.example.yaml` and set:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
public_name: "Neighbor Mac Studio"
city: "Madrid"
privacy_mode: "public"
coordinator_url: "ws://coordinator-host:8080/ws/node"
backend:
  type: "ollama"
  url: "http://127.0.0.1:11434"
models:
  - "llama3.1:8b"
```

Use:

- `privacy_mode: "private"` for trusted machines that may receive sensitive work.
- `privacy_mode: "community"` for known shared machines that should not receive private prompts.
- `privacy_mode: "public"` for rented or untrusted machines that should receive only non-sensitive prompts.

Provider account policy is enforced by the coordinator. If the coordinator account says a provider is `public`, the node cannot receive private work even if its local config claims `private`.

## Enroll Accounts Dynamically

Static YAML accounts are useful for development. For a real city network, prefer the admin API because generated secrets are returned once and only hashes are persisted.

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

The response includes `api_key`. Store it for the consumer; the coordinator persists only its hash.

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

The response includes `provider_token`. Put it in the node config.

Helper commands:

```bash
CONSUMER_ID=studio-b make city-enroll
PROVIDER_PRIVACY_MODE=public PROVIDER_ID=neighbor-mac make city-enroll
```

Rotate or disable accounts:

```bash
curl -X POST http://localhost:8080/admin/consumers/studio-b/rotate-key \
  -H 'Authorization: Bearer admin-dev-token'

curl -X POST http://localhost:8080/admin/providers/neighbor-mac/rotate-token \
  -H 'Authorization: Bearer admin-dev-token'

curl -X DELETE http://localhost:8080/admin/consumers/studio-b \
  -H 'Authorization: Bearer admin-dev-token'

curl -X DELETE http://localhost:8080/admin/providers/neighbor-mac \
  -H 'Authorization: Bearer admin-dev-token'
```

Helper equivalents:

```bash
ACTION=rotate CONSUMER_ID=studio-b make city-enroll
ACTION=rotate PROVIDER_ID=neighbor-mac make city-enroll
ACTION=disable CONSUMER_ID=studio-b make city-enroll
ACTION=disable PROVIDER_ID=neighbor-mac make city-enroll
```

## Call As A Consumer

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "fast",
    "privacy_tier": "public",
    "messages": [{"role": "user", "content": "Explain this city AI network in one sentence"}],
    "stream": true
  }'
```

If `privacy_tier` is omitted, the coordinator uses `private`. You can also send:

```text
X-Mi-Privacy-Tier: private
X-Mi-Privacy-Tier: community
X-Mi-Privacy-Tier: public
```

The coordinator returns service unavailable when no eligible node exists. It does not silently lower the requested privacy tier.

## Route By Hardware Or Backend

Nodes advertise backend and hardware metadata. A request can ask for a specific class of node:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -H 'X-Mi-Accelerator: metal' \
  -d '{
    "model": "fast",
    "messages": [{"role": "user", "content": "Use a Metal-capable node if one is eligible"}]
  }'
```

Equivalent body fields:

```json
{
  "mi_backend": "ollama",
  "mi_device_kind": "mac",
  "mi_soc": "apple_silicon",
  "mi_accelerators": ["metal"]
}
```

Today this is mostly useful for Mac/Ollama metadata and experiments. The same interface is intended for future MLX, CUDA, Snapdragon/QNN, LiteRT, Android, Linux, and Windows nodes.

## Model Aliases

Model aliases are configured under `models.aliases`:

```yaml
models:
  aliases:
    - id: "fast"
      target: "llama3.1:8b"
      display_name: "Fast local"
      description: "Low-latency shared assistant"
      tags: ["general", "fast"]
```

Clients call the alias:

```json
{"model": "fast"}
```

The coordinator dispatches to nodes that advertise the concrete target model. `/v1/models` only lists aliases whose target is currently available on at least one healthy node.

Inspect the richer catalog:

```bash
curl http://localhost:8080/v1/models/catalog \
  -H 'Authorization: Bearer sk-mi-studio-a-dev'
```

## Inspect Operations

Public capacity:

```bash
curl http://localhost:8080/network/status
```

Consumer account:

```bash
curl http://localhost:8080/v1/me \
  -H 'Authorization: Bearer sk-mi-studio-a-dev'
```

Nodes:

```bash
curl http://localhost:8080/admin/nodes \
  -H 'Authorization: Bearer admin-dev-token'
```

City accounts and usage:

```bash
curl http://localhost:8080/admin/city \
  -H 'Authorization: Bearer admin-dev-token'
```

Settlement and verification:

```bash
curl http://localhost:8080/admin/settlement \
  -H 'Authorization: Bearer admin-dev-token'

curl http://localhost:8080/admin/settlement/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Provider reputation:

```bash
curl http://localhost:8080/admin/reputation \
  -H 'Authorization: Bearer admin-dev-token'
```

Integrity anchor:

```bash
curl http://localhost:8080/admin/integrity \
  -H 'Authorization: Bearer admin-dev-token'
```

## Benchmark Challenges

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
    "score": 94
  }'
```

Run a synthetic challenge:

```bash
curl -X POST http://localhost:8080/admin/challenges/run \
  -H 'Authorization: Bearer admin-dev-token' \
  -H 'Content-Type: application/json' \
  -d '{"model":"llama3.1:8b","expected_contains":"mi-ok"}'
```

Add `"provider_id":"ray-home"` to test one provider. Without `provider_id`, the runner rotates across eligible providers.

Inspect and verify:

```bash
curl http://localhost:8080/admin/challenges \
  -H 'Authorization: Bearer admin-dev-token'

curl http://localhost:8080/admin/challenges/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Challenge evidence feeds provider reputation, but current synthetic checks are not a trustless anti-farming system. They are operational signals for cooperative networks.

## Persistence

Recommended city storage:

```yaml
city:
  sqlite_path: "data/mi-city.db"
settlement:
  sqlite_path: "data/mi-settlement.db"
challenges:
  path: "data/challenge-chain.jsonl"
```

SQLite is opened in WAL mode with a busy timeout. Legacy storage is still available:

```yaml
city:
  usage_store_path: "data/city-usage.json"
settlement:
  chain_path: "data/settlement-chain.jsonl"
```

Back up `data/mi-city.db`, `data/mi-settlement.db`, and `data/challenge-chain.jsonl` if they represent credits, payouts, invoices, or audit evidence.

## What This Unlocks

- Local AI for a school, coworking space, agency, clinic, or city group.
- Shared inference without every user sending prompts to a cloud API.
- Internal chargeback or prepaid credits.
- Provider rewards for underused Macs.
- Public rented capacity for non-sensitive prompts.
- Private routing for sensitive prompts on trusted nodes.
- Tamper-evident records that can be reviewed before payouts.
- Later anchoring of local accounting to a public chain or timestamp service.

## Recommended Next Hardening

- Add a dashboard for nodes, usage, reputation, settlement, and earnings.
- Add Prometheus metrics and uptime windows.
- Add one-command provider enrollment links.
- Add macOS LaunchAgent installation for always-on nodes.
- Add invoice and payout exports.
- Add exact model-family tokenizers.
- Add Postgres as an optional store for larger multi-coordinator deployments.
- Add stronger private-compute controls before routing sensitive data to untrusted hardware.
