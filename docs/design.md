# mi design

`mi` is a local-first inference control plane for Apple Silicon fleets today and broader ARM edge fleets over time.

## Principles

- Local network first, public network later.
- OpenAI-compatible API from day one.
- Node agents connect outbound so Macs do not need open inbound ports.
- Backend abstraction first: Ollama today, MLX for Apple Silicon, QNN/LiteRT/Vulkan paths for future Android and Xiaomi nodes.
- Scheduler decisions should be based on observed behavior, not static specs.
- Prompt logging is off by design; logs should use request IDs, model IDs, and timing.
- Users call stable aliases like `fast` or `code`; the coordinator maps those to concrete node models.

## Request flow

1. A client calls `POST /v1/chat/completions` on the coordinator.
2. The coordinator resolves the request privacy tier, defaulting to `private`.
3. The coordinator normalizes the OpenAI request into an internal inference request.
4. The scheduler reserves a healthy node that advertises the requested model and accepts the requested privacy tier.
5. The coordinator sends the request to that node over the node WebSocket.
6. The node streams chunks from its configured backend back to the coordinator.
7. The coordinator emits OpenAI-style SSE chunks to the client.

Node WebSocket envelopes include a `version` field, and register/heartbeat payloads include `protocol_version`. Missing versions are treated as v1 for backwards compatibility, so future Android, Snapdragon, CUDA, Linux, Windows, and Apple Silicon agents can be upgraded gradually.

Requests can include optional hardware routing hints:

```json
{
  "model": "fast",
  "mi_backend": "vllm",
  "mi_device_kind": "server",
  "mi_accelerators": ["cuda"],
  "messages": [{"role": "user", "content": "Use a CUDA node"}]
}
```

Equivalent headers are also supported:

- `X-Mi-Backend`
- `X-Mi-Device-Kind`
- `X-Mi-SoC`
- `X-Mi-Accelerator`
- `X-Mi-Accelerators`

## Failover

The scheduler retries another eligible node only while no chunk has been sent to the client. This covers node disconnects, capacity races, and startup failures without duplicating generated text.

Once the first chunk is emitted, the request is pinned to that node. If the node fails mid-generation, the error is surfaced instead of replaying the prompt on another node.

Nodes that fail before the first token enter a short cooldown. Repeated failures extend the cooldown up to a cap, and a successful request clears the node's error streak. This keeps an unstable Mac from being chosen first over and over while still letting it recover automatically.

Provider reputation is also part of routing. The coordinator builds an explainable score from healthy nodes, cooldowns, error streaks, settlement history, SLA penalties, and challenge summaries, then passes per-provider scores to the scheduler. The scheduler also records coordinator-observed latency, TTFT, estimated tokens/sec, and failure rate for each node. Lower scores and poor observed performance add routing cost, so challenge failures and slow or unreliable nodes affect future traffic instead of living only in the admin dashboard.

Privacy tiers are enforced before scheduling. A node announces what it accepts, but the coordinator intersects that claim with the provider account policy before registration. A provider account marked `public` can serve `public` requests, but it is never selected for `private` or `community` requests. If no eligible node exists, the coordinator returns no-node availability instead of silently lowering privacy.

For quota-limited consumers, the coordinator reserves an estimated token budget before dispatch and releases or reconciles it when the request fails or completes. Usage accounting is based on coordinator-estimated prompt and completion tokens, not node-reported counts. This prevents concurrent requests from spending the same remaining quota and avoids the simplest provider-side token inflation attack.

Responses include dispatch metadata:

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

For streaming responses, those values are sent as HTTP trailers.

Admin node snapshots expose:

- `error_streak`
- `cooldown_until`
- `last_error`
- `in_cooldown`
- `backend`
- `device_kind`
- `device_vendor`
- `soc`
- `accelerators`
- `completed_requests`
- `failed_requests`
- `failure_rate_bps`
- `observed_latency_ms`
- `observed_ttft_ms`
- `observed_tokens_per_second`

## Backend And Hardware Metadata

Nodes advertise their inference backend and hardware profile during registration. The coordinator can already filter dispatch by requested backend, device kind, SoC, and accelerator hints. This gives future policies enough information to:

- route Apple Silicon nodes to MLX or Metal-backed models
- route Snapdragon/Xiaomi nodes to QNN-backed models
- prefer Android nodes only for charging/Wi-Fi opportunistic public workloads
- score challenge jobs by backend and accelerator, not only by provider

## Next milestones

- Persistent node identity and API-key scoped node enrollment.
- TLS or mTLS for LAN/VPN deployments.
- MLX backend.
- Android agent and QNN/LiteRT backend experiments.
- Model aliases and model registry config.
- Dashboard with node health, loaded models, queue depth, TTFT, and tokens/s.
- LaunchAgent installer for macOS nodes.
- Prometheus metrics.
- Retry on a second node when failure happens before first token.
