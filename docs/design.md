# Design

`mi` is a local-first inference control plane. It exposes one OpenAI-compatible API and dispatches work to provider nodes that connect outbound over WebSocket.

The working deployment target today is Apple Silicon Macs running Ollama. The protocol and scheduler already carry enough metadata to support future MLX, CUDA, Linux, Windows, Android, Snapdragon/QNN, and LiteRT nodes.

## Design Principles

- Keep the first deployment local and understandable.
- Preserve OpenAI-compatible client ergonomics.
- Make provider machines connect outbound, so they do not need open inbound ports.
- Keep prompt bodies out of logs, settlement events, and challenge events by default.
- Route by observed behavior, not only advertised specs.
- Treat privacy claims narrowly and honestly.
- Keep the backend abstraction small: Ollama today, more runtimes later.
- Use stable model aliases so clients do not need to know every node's concrete model ID.

## Main Components

Coordinator:

- Serves `/v1/chat/completions`, `/v1/models`, `/v1/models/catalog`, `/v1/me`, `/network/status`, and admin endpoints.
- Serves `/admin/dashboard` as a small built-in operator UI over the admin JSON endpoints.
- Authenticates consumers and admin requests.
- Accepts node WebSocket connections.
- Normalizes OpenAI-style chat requests into internal inference requests.
- Applies privacy policy, quota reservations, output caps, scheduling, streaming, accounting, and settlement.

Node agent:

- Runs on a provider machine.
- Connects outbound to the coordinator at `/ws/node`.
- Registers models, backend type, hardware metadata, capacity, provider identity, and privacy tiers.
- Sends heartbeats.
- Forwards inference requests to the local backend.
- Streams chunks and final completion metadata back to the coordinator.

Backend:

- Ollama is implemented today.
- The node config has a backend abstraction for future MLX, llama.cpp, QNN, LiteRT, CUDA, and other runtimes.

City market:

- Stores consumers, providers, hashed secrets, quotas, usage, and account policy.
- Uses SQLite/WAL when `city.sqlite_path` is set.
- Supports legacy JSON state through `city.usage_store_path`.

Settlement ledger:

- Records successful request settlement events.
- Uses SQLite/WAL when `settlement.sqlite_path` is set.
- Supports legacy JSONL through `settlement.chain_path`.

Challenge ledger:

- Records provider benchmark evidence in a separate hash chain.
- Feeds provider reputation.

## Request Flow

1. A client calls `POST /v1/chat/completions`.
2. The coordinator authenticates the consumer when city mode or API keys are configured.
3. The coordinator resolves the privacy tier. Missing tier defaults to `private`.
4. The coordinator resolves model aliases and request capability hints.
5. The coordinator applies a default output cap if `max_tokens` is absent.
6. For quota-limited consumers, the coordinator reserves an estimated token budget.
7. The scheduler selects an eligible node.
8. The coordinator sends an internal inference request to that node over WebSocket.
9. The node streams chunks from its local backend.
10. The coordinator streams OpenAI-style chunks to the client.
11. The coordinator estimates actual prompt and completion usage from what it saw.
12. Usage, quota reconciliation, settlement, reputation signals, and observed metrics are updated.

## Scheduling

Eligibility filters:

- Requested model or alias target.
- Node health and liveness.
- Available capacity.
- Privacy tier.
- Provider account policy.
- Optional backend, device, SoC, and accelerator hints.

Cost and priority signals:

- Queue depth.
- Capacity pressure.
- Node cooldowns.
- Error streaks.
- Provider reputation.
- Coordinator-observed latency.
- Coordinator-observed time to first token.
- Coordinator-estimated tokens per second.
- Coordinator-observed failure rate.

Provider reputation is refreshed in the background and on admin inspection. It is not recomputed from the full ledger on every user request.

## Failover Semantics

The coordinator can retry a second eligible node only before the first response chunk has been sent to the client.

This covers:

- Node disconnect before generation.
- Backend startup failure.
- Capacity races.
- Immediate provider errors.

After the first chunk reaches the client, the request is pinned to that node. If the node fails mid-stream, the coordinator returns the error instead of replaying the prompt on another provider and risking duplicate or divergent output.

Nodes that fail before first output enter cooldown. Repeated failures extend cooldown up to a cap. A successful request clears the error streak.

## Privacy Tiers

Supported tiers:

- `private`
- `community`
- `public`

Provider modes expand as:

- `private`: accepts `private`, `community`, and `public`.
- `community`: accepts `community` and `public`.
- `public`: accepts only `public`.

The node advertises tiers, but the coordinator intersects that with the provider account policy. A public provider account cannot self-promote by changing its local node config.

If no eligible node exists for the requested privacy tier, the coordinator returns no-node availability instead of silently lowering privacy.

## Usage And Quotas

The coordinator estimates usage instead of trusting node-reported token counts:

- Prompt estimate comes from request messages.
- Completion estimate comes from streamed content relayed by the coordinator.
- Current estimate is based on Unicode rune count.
- Requests without `max_tokens` receive a default cap.
- Quota-limited consumers reserve budget before dispatch.
- Reservations are reconciled when the request succeeds or fails.

This prevents concurrent overspend and simple worker-side token inflation. It is not exact model tokenization.

## Response Metadata

Responses include useful dispatch metadata as headers. For streaming responses, these are sent as HTTP trailers.

- `X-Mi-Privacy-Tier`
- `X-Mi-Dispatch-Attempts`
- `X-Mi-Node-Id`
- `X-Mi-Provider-Id`
- `X-Mi-Backend`
- `X-Mi-Device-Kind`
- `X-Mi-Accelerators`
- `X-Mi-Usage-Source`
- `X-Mi-Observed-Latency-Ms`
- `X-Mi-Observed-TTFT-Ms`
- `X-Mi-Observed-Tokens-Per-Second`

`X-Mi-Usage-Source` is currently `coordinator_estimate`.

## Node Metadata

Node registration includes:

- Node ID.
- Provider ID.
- Public name.
- City.
- Models.
- Max concurrency.
- Backend type.
- Backend URL.
- Hardware kind.
- Hardware vendor.
- Hardware model.
- SoC.
- Accelerators.
- Power mode.
- Network mode.
- Privacy tiers.
- Protocol version.

Admin node snapshots expose:

- Health.
- Queue and active request counts.
- Error streak.
- Cooldown state.
- Last error.
- Completed requests.
- Failed requests.
- Failure rate.
- Observed latency.
- Observed TTFT.
- Observed tokens per second.
- Backend and hardware metadata.

## Capability Hints

Requests can ask for hardware or backend capabilities in JSON:

```json
{
  "model": "fast",
  "mi_backend": "ollama",
  "mi_device_kind": "mac",
  "mi_soc": "apple_silicon",
  "mi_accelerators": ["metal"],
  "messages": [{"role": "user", "content": "Use a Metal-capable Mac"}]
}
```

Equivalent headers:

- `X-Mi-Backend`
- `X-Mi-Device-Kind`
- `X-Mi-SoC`
- `X-Mi-Accelerator`
- `X-Mi-Accelerators`

This is already implemented as scheduler filtering. Future agents can use the same interface for CUDA servers, Snapdragon devices, Android phones, Windows laptops, Linux boxes, and MLX-backed Macs.

## Protocol Versioning

WebSocket envelopes include a version field and registration/heartbeat payloads include `protocol_version`.

Current behavior:

- Missing version is treated as v1 for backwards compatibility.
- The field is visible in protocol messages.
- Strict negotiation, feature flags, and version-based rejection are not implemented yet.

The field is present so future agents can add gradual upgrade logic without changing the shape of every message again.

## Storage

Recommended:

```yaml
city:
  sqlite_path: "data/mi-city.db"
settlement:
  sqlite_path: "data/mi-settlement.db"
challenges:
  path: "data/challenge-chain.jsonl"
```

SQLite is opened in WAL mode with a busy timeout. This is suitable for a single coordinator process.

Future larger deployments may need Postgres or another external database for multi-coordinator operation.

## Current Boundaries

- Ollama is the only implemented inference backend.
- There is no browser dashboard yet.
- Settlement is local cooperative accounting.
- Synthetic challenges are operational evidence, not proof of inference.
- Token estimates are approximate.
- Privacy tiers do not make prompts invisible to eligible machines.
- SQLite storage is for one coordinator process, not a distributed database.

## Next Design Work

- Prometheus metrics.
- Admin dashboard.
- One-command provider enrollment.
- macOS LaunchAgent installer.
- Exact tokenizers by model family.
- Pricing and payout exports.
- Postgres option for larger deployments.
- MLX backend for Apple Silicon.
- Android/Snapdragon prototype.
- Strict protocol negotiation and capability feature flags.
