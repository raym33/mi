# Renting Compute Privately

`mi` can run as a local compute market: providers contribute machines, consumers spend quota or credits, and the coordinator tracks usage and rewards.

The privacy model is routing-based. It keeps sensitive requests on trusted nodes and sends only non-sensitive work to public rented capacity.

## Product Use Case

For a small business or community, the initial value is:

- Use local machines instead of renting cloud GPUs for every request.
- Keep sensitive work on owned or trusted machines.
- Let public or rented providers serve only non-sensitive prompts.
- Track consumer usage and provider contribution.
- Build internal credits, invoices, or later payouts from settlement records.

This is useful before any public DePIN token exists.

## The Core Rule

Private prompts should go only to trusted machines.

Public rented machines should receive only prompts that the consumer is comfortable revealing to that provider.

`mi` enforces this by combining:

- Request privacy tier.
- Provider account privacy policy.
- Node-declared privacy tiers.
- Scheduler eligibility checks.

## Privacy Tiers

Every request has a `privacy_tier`. If omitted, it defaults to `private`.

| Tier | Intended data | Eligible providers |
| --- | --- | --- |
| `private` | Customer data, contracts, source code, financials, health, internal chat | Trusted private providers only |
| `community` | Known coworking, school, team, or city-group data | Private or community providers |
| `public` | Generic prompts, public text, non-sensitive batch jobs | Private, community, or public providers |

Provider modes expand like this:

- `private`: accepts `private`, `community`, and `public`.
- `community`: accepts `community` and `public`.
- `public`: accepts only `public`.

A provider can also declare exact tiers:

```yaml
privacy_tiers:
  - "public"
```

## How Enforcement Works

The provider account is controlled by the coordinator operator:

```yaml
providers:
  - id: "neighbor-mac"
    display_name: "Neighbor Mac Studio"
    token: "pk-mi-..."
    privacy_mode: "public"
```

The node also declares what it accepts:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
privacy_mode: "public"
```

The coordinator intersects both. If the account says `public`, the node cannot self-promote to `private` by editing local config.

If a request asks for `private` and only public nodes are online, the coordinator returns no eligible node. It does not silently downgrade the request.

## Calling With A Tier

JSON body:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "fast",
    "privacy_tier": "public",
    "messages": [{"role": "user", "content": "Draft a generic slogan for a bakery"}]
  }'
```

Header:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'X-Mi-Privacy-Tier: private' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "private",
    "messages": [{"role": "user", "content": "Summarize this internal contract..."}]
  }'
```

## Renting Out A Mac

A provider who wants to rent capacity to unknown consumers should use `public`.

Coordinator-side provider:

```yaml
providers:
  - id: "neighbor-mac"
    display_name: "Neighbor Mac Studio"
    token: "pk-mi-..."
    privacy_mode: "public"
```

Node config:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
public_name: "Neighbor Mac Studio"
city: "Madrid"
privacy_mode: "public"
coordinator_url: "wss://mi.example.local/ws/node"
backend:
  type: "ollama"
  url: "http://127.0.0.1:11434"
hardware:
  kind: "mac"
  vendor: "apple"
  soc: "apple_silicon"
  accelerators: ["metal"]
```

That node can receive public jobs and accrue usage/reward accounting. It will not receive `private` or `community` jobs.

## Making It Private For The User

Use these controls together:

- Default clients to `privacy_tier: "private"`.
- Mark owned/trusted providers as `privacy_mode: "private"`.
- Mark rented unknown providers as `privacy_mode: "public"`.
- Use HTTPS/WSS.
- Enable node mTLS.
- Use provider tokens and rotate them when needed.
- Use separate consumer API keys.
- Avoid prompt-body logging.
- Keep settlement/challenge logs metadata-only.

Important limitation: a normal provider machine can inspect a prompt it receives. `mi` prevents private prompts from being scheduled to public rented nodes; it does not cryptographically hide prompts from nodes that are eligible to run them.

For truly untrusted hardware processing sensitive prompts, future work would need confidential compute, TEEs where available, signed agent distribution, redaction, split inference, encrypted inference research, or a different trust model.

## Rewards And Payments

The current repo tracks:

- Requests per consumer.
- Estimated prompt tokens.
- Estimated completion tokens.
- Estimated total tokens.
- Requests per provider.
- Consumer debits.
- Provider rewards.
- Provider latency penalties.
- Hash-linked settlement events.

This supports:

- Internal credits.
- Manual invoice export.
- Off-platform payouts.
- Community accounting.
- Future on-chain anchoring.

It does not yet include:

- Wallets.
- Fiat payment processor.
- Stablecoin transfers.
- Exact tokenizers.
- Dispute windows.
- Slashing.
- Trustless proof of inference.

## Suggested Rollout

1. Start with owned private nodes only.
2. Add public rented nodes for non-sensitive prompts.
3. Review `/admin/city`, `/admin/settlement`, `/admin/reputation`, and `/admin/integrity`.
4. Pay providers manually from reviewed settlement records.
5. Anchor `anchor_hash` externally for payout periods.
6. Add stronger pricing, tokenization, and dispute tooling before opening the network to adversarial providers.
