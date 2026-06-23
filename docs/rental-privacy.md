# Renting compute privately

`mi` can be used as a local compute marketplace: providers contribute idle Macs, consumers pay or spend credits for inference, and the coordinator keeps account usage.

The product value for a user or small business is simple:

- Lower inference cost by using nearby idle Apple Silicon instead of cloud GPUs.
- Faster local latency for routine assistant, coding, document, and support workloads.
- Spend controls through API keys, quotas, and per-consumer usage.
- Provider-side earning potential for Macs that would otherwise sit idle.
- A privacy boundary that prevents sensitive prompts from being scheduled onto public rented nodes.

## Privacy tiers

Every request has a `privacy_tier`. If it is omitted, the coordinator treats it as `private`.

| Tier | Intended data | Eligible nodes |
| --- | --- | --- |
| `private` | Customer data, contracts, source code, financials, health, internal chat | Trusted private nodes only |
| `community` | Shared city or coworking data where providers are known members | `private` or `community` nodes |
| `public` | Non-sensitive prompts that can use rented capacity | Any node that accepts `public` |

Each node announces what it accepts through `privacy_mode`:

```yaml
privacy_mode: "private"
```

Modes expand like this:

- `private`: accepts `private`, `community`, and `public`.
- `community`: accepts `community` and `public`.
- `public`: accepts only `public`.

For custom deployments, a node can declare exact tiers instead:

```yaml
privacy_tiers:
  - "public"
```

## Calling with a tier

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

If no eligible node exists for the requested tier, the coordinator returns service unavailable instead of silently falling back to a less-private provider.

## Renting out a Mac

A provider who wants to rent capacity to unknown consumers should use:

```yaml
provider_id: "neighbor-mac"
provider_token: "pk-mi-..."
public_name: "Neighbor Mac Studio"
city: "Madrid"
privacy_mode: "public"
coordinator_url: "wss://mi.example.local/ws/node"
```

That node can earn usage credits for public prompts, but it will not receive private or community prompts.

## Making it private for the user

This implementation enforces scheduling privacy. It prevents sensitive requests from being sent to untrusted rented nodes.

For production, combine it with:

- HTTPS/WSS for client and node traffic.
- Node mTLS so only approved provider agents can connect.
- Provider token rotation and revocation.
- Consumer API keys and quotas.
- A private default tier in clients and SDK wrappers.
- Short retention logs with no prompt body storage.

Important limitation: a normal remote provider machine can still technically inspect the prompt it receives. For truly untrusted hardware processing sensitive data, future versions need stronger isolation such as Apple Virtualization isolation, TEEs where available, confidential GPU services, client-side redaction, or encrypted/split inference techniques. Until then, `private` means “scheduled only to trusted nodes,” not “cryptographically invisible to the machine doing inference.”

## Monetization model

The current ledger counts prompt, output, and total tokens per consumer and provider. A city operator can turn that into:

- Prepaid token bundles for consumers.
- Provider credits based on served tokens.
- Higher rates for larger models or low-latency private pools.
- Organization pools for schools, law firms, agencies, clinics, and coworking spaces.

The next product step is pricing rules: per-model rates, provider payout rates, minimum reliability thresholds, and invoice exports.
