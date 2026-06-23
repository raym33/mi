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

## Failover

The scheduler retries another eligible node only while no chunk has been sent to the client. This covers node disconnects, capacity races, and startup failures without duplicating generated text.

Once the first chunk is emitted, the request is pinned to that node. If the node fails mid-generation, the error is surfaced instead of replaying the prompt on another node.

Nodes that fail before the first token enter a short cooldown. Repeated failures extend the cooldown up to a cap, and a successful request clears the node's error streak. This keeps an unstable Mac from being chosen first over and over while still letting it recover automatically.

Privacy tiers are enforced before scheduling. A node announces what it accepts, but the coordinator intersects that claim with the provider account policy before registration. A provider account marked `public` can serve `public` requests, but it is never selected for `private` or `community` requests. If no eligible node exists, the coordinator returns no-node availability instead of silently lowering privacy.

For quota-limited consumers, the coordinator reserves an estimated token budget before dispatch and releases or reconciles it when the request fails or completes. This prevents concurrent requests from spending the same remaining quota.

Responses include dispatch metadata:

- `X-Mi-Privacy-Tier`
- `X-Mi-Dispatch-Attempts`
- `X-Mi-Node-Id`
- `X-Mi-Provider-Id`

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

## Backend And Hardware Metadata

Nodes advertise their inference backend and hardware profile during registration. This does not change scheduling yet, but it gives the coordinator enough information to support future policies such as:

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
