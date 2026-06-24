# Security

`mi` is local-first, but local-first does not remove the need for authentication, encrypted transport, careful privacy claims, backups, and operational discipline.

This document describes what the repo implements today and what still requires future work.

## Threat Model Summary

`mi` is currently appropriate for:

- A trusted LAN.
- A Tailscale/WireGuard mesh.
- A school, coworking space, studio, agency, or small business with known operators.
- A local compute cooperative where public rented nodes receive only non-sensitive prompts.

It is not yet appropriate as a trustless public compute marketplace for sensitive prompts. Remote provider machines can inspect prompts they receive unless future confidential-compute or encrypted inference techniques are added.

## Implemented Security Layers

Transport:

- HTTP for local development.
- HTTPS for API clients.
- WSS for node WebSocket connections.
- Optional node mTLS for `/ws/node`.

Authentication:

- Consumer API keys for `/v1/*` endpoints.
- Provider tokens for node enrollment in city mode.
- Admin bearer token for `/admin/*` endpoints.
- Optional development escape hatch with `dev_admin_open: true`.

Authorization and routing:

- Provider account privacy policy.
- Node-declared privacy tiers.
- Coordinator-side intersection of account policy and node claim.
- Default request privacy tier is `private`.
- No silent fallback from private to public when no eligible node exists.

Accounting integrity:

- Coordinator-estimated usage rather than worker-reported billing counts.
- SQLite/WAL city state for accounts, hashed secrets, quotas, and usage.
- Hash-linked settlement events.
- Hash-linked challenge events.
- `/admin/integrity` anchor hash for external timestamping.

Data minimization:

- Settlement events do not store prompt bodies.
- Challenge events do not store model outputs.
- Dynamically generated consumer keys and provider tokens are stored as hashes, not plaintext.
- Logs should use request IDs, model IDs, timing, and node metadata, not prompt bodies.

## Important Limitations

Privacy tiers are scheduling policy, not cryptography.

If a prompt is sent to a normal remote provider machine, that machine can technically inspect it. Use `private` only with machines you trust, and reserve `public` for non-sensitive prompts.

Settlement is tamper-evident, not tamper-proof.

The local ledger can detect edits to recorded events, but it cannot force the operator to record every request, prevent database deletion, or prove model correctness. Use backups and external anchoring when rewards matter.

Token accounting is approximate.

The coordinator currently estimates tokens from Unicode rune counts. This is better than trusting worker-reported counts, but it is not exact model tokenization.

Synthetic challenges are evidence, not proof.

Challenge results help reputation, but they are not a complete anti-farming or proof-of-inference system.

## Recommended Deployment

For a real small-community deployment, use one of:

- Tailscale or another WireGuard mesh.
- A private LAN with firewall rules.
- A reverse proxy with HTTPS.
- The built-in TLS/WSS mode with node mTLS.

Avoid exposing the plain HTTP coordinator to the public internet.

Use:

- Strong `admin_token`.
- Separate consumer API keys per team or user.
- Provider tokens per provider account.
- Rotation when a key or token leaves your control.
- `privacy_mode: "public"` for untrusted rented providers.
- Backups for SQLite and challenge files.
- External anchoring of `/admin/integrity` before payout periods close.

## Built-In TLS And mTLS

Generate development certificates:

```bash
make dev-certs
```

Start the HTTPS/WSS coordinator:

```bash
make run-city-coordinator-tls
```

Start a node over WSS:

```bash
make run-city-node-tls
```

Call the API with the generated CA:

```bash
curl --cacert certs/ca.crt https://localhost:8443/network/status
```

The TLS city example also enables node mTLS:

- The coordinator trusts node certificates signed by `certs/ca.crt`.
- The node presents `certs/node.crt` and `certs/node.key`.
- `/ws/node` rejects node connections without a valid client certificate.
- Normal HTTPS API clients still use API keys and do not need node certificates.

## Real Certificates

For a real network, generate certificates for the hostname that provider nodes use:

```bash
COMMON_NAME=mi.example.local ALT_DNS=mi.example.local ALT_IP=100.64.0.10 make dev-certs
```

Node config:

```yaml
coordinator_url: "wss://mi.example.local:8443/ws/node"
tls:
  ca_file: "certs/ca.crt"
  cert_file: "certs/node.crt"
  key_file: "certs/node.key"
```

Coordinator config:

```yaml
tls:
  cert_file: "certs/server.crt"
  key_file: "certs/server.key"
  node_client_ca_file: "certs/ca.crt"
```

## Development Escape Hatches

The node agent supports:

```yaml
tls:
  insecure_skip_verify: true
```

Use this only for temporary local testing. It disables server certificate verification.

The coordinator supports:

```yaml
dev_admin_open: true
```

Use this only for throwaway development. In normal deployments, set a real `admin_token`.

## Privacy Tier Policy

Every request has a privacy tier:

- `private`: sensitive prompts, trusted nodes only.
- `community`: known local group prompts.
- `public`: non-sensitive prompts that may use rented capacity.

Every provider has a coordinator-side privacy policy. Every node also declares what it accepts. The coordinator intersects both sets before registration and scheduling.

This prevents a public rented provider from editing local config and receiving private prompts.

It does not hide prompts from eligible machines.

## Settlement Integrity

Recommended stores:

```yaml
city:
  sqlite_path: "data/mi-city.db"
settlement:
  sqlite_path: "data/mi-settlement.db"
challenges:
  path: "data/challenge-chain.jsonl"
```

Back up:

- `data/mi-city.db`
- `data/mi-settlement.db`
- `data/challenge-chain.jsonl`

Verify settlement:

```bash
curl http://localhost:8080/admin/settlement/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Verify challenges:

```bash
curl http://localhost:8080/admin/challenges/verify \
  -H 'Authorization: Bearer admin-dev-token'
```

Export an anchor hash:

```bash
curl http://localhost:8080/admin/integrity \
  -H 'Authorization: Bearer admin-dev-token'
```

Publish the `anchor_hash` externally when it matters that later edits or truncation are detectable.

## Production Checklist

- Use HTTPS/WSS.
- Enable node mTLS for provider nodes.
- Use a strong admin token.
- Use separate consumer keys.
- Use separate provider tokens.
- Set public rented providers to `privacy_mode: "public"`.
- Keep sensitive workloads on trusted private providers.
- Back up SQLite databases and challenge files.
- Anchor `/admin/integrity` regularly if rewards are paid.
- Avoid prompt-body logging.
- Rotate keys and tokens after suspected exposure.
- Add model-family tokenizers before real-money token billing.
- Add signed receipts and dispute windows before adversarial rental markets.
