# City network mode

City mode turns `mi` from a private LAN cluster into a small shared inference network for a neighborhood, coworking space, school, or city group.

It introduces four primitives:

- Consumers: people or teams allowed to call the OpenAI-compatible API.
- Providers: people contributing Mac compute.
- Provider tokens: shared secrets that let a node join under a provider account.
- Usage accounting: requests and tokens are counted for both the consumer and the provider.
- Consumer quotas: each API key can belong to an account with a token limit.
- Persistent local usage: usage survives coordinator restarts when `usage_store_path` is configured.

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
    "display_name": "Neighbor Mac Studio"
  }'
```

The response includes `provider_token`. Put it in the node config:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
```

There is also a helper script:

```bash
CONSUMER_ID=studio-b make city-enroll
PROVIDER_ID=neighbor-mac make city-enroll
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
    "model": "llama3.1:8b",
    "messages": [{"role": "user", "content": "Explain what this city AI network is in one sentence"}],
    "stream": true
  }'
```

## Inspect usage

```bash
curl http://localhost:8080/admin/city \
  -H 'Authorization: Bearer admin-dev-token'
```

The example config writes usage to `data/city-usage.json`. Keep that file backed up if it represents real credits.
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
- Internal credits or payouts later, based on measured provider token contribution.
- Public endpoint on a city VPN, Tailscale network, or reverse proxy.
- Fair usage limits for schools, coworking spaces, and local AI clubs.

## Next hardening steps

- Replace static provider tokens with enrollment links.
- Add temporary enrollment links and one-command node join.
- Move from JSON persistence to SQLite/Postgres for larger networks.
- Add quotas and prepaid credits.
- Add TLS/mTLS.
- Add mTLS for node-only endpoints.
- Add provider reputation and uptime scoring.
- Add request retry before first token.
- Add prompt privacy controls and optional coordinator-to-provider encryption.
