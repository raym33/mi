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

For another provider, copy `configs/node-agent.city.example.yaml` and change:

- `provider_id`
- `provider_token`
- `public_name`
- `coordinator_url`

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

Consumers can inspect their own account and remaining quota:

```bash
curl http://localhost:8080/v1/me \
  -H 'Authorization: Bearer sk-mi-studio-a-dev'
```

Anyone can inspect public network capacity:

```bash
curl http://localhost:8080/network/status
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
- Move from JSON persistence to SQLite/Postgres for larger networks.
- Add quotas and prepaid credits.
- Add TLS/mTLS.
- Add provider reputation and uptime scoring.
- Add request retry before first token.
- Add prompt privacy controls and optional coordinator-to-provider encryption.
