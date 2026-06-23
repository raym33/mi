# mi design

`mi` is a local-first inference control plane for Mac fleets.

## Principles

- Local network first, public network later.
- OpenAI-compatible API from day one.
- Node agents connect outbound so Macs do not need open inbound ports.
- Ollama first for speed, MLX next for Apple Silicon performance.
- Scheduler decisions should be based on observed behavior, not static specs.
- Prompt logging is off by design; logs should use request IDs, model IDs, and timing.
- Users call stable aliases like `fast` or `code`; the coordinator maps those to concrete node models.

## Request flow

1. A client calls `POST /v1/chat/completions` on the coordinator.
2. The coordinator normalizes the OpenAI request into an internal inference request.
3. The scheduler reserves a healthy node that advertises the requested model.
4. The coordinator sends the request to that node over the node WebSocket.
5. The node streams chunks from Ollama back to the coordinator.
6. The coordinator emits OpenAI-style SSE chunks to the client.

## Failover

The scheduler retries another eligible node only while no chunk has been sent to the client. This covers node disconnects, capacity races, and startup failures without duplicating generated text.

Once the first chunk is emitted, the request is pinned to that node. If the node fails mid-generation, the error is surfaced instead of replaying the prompt on another node.

Nodes that fail before the first token enter a short cooldown. Repeated failures extend the cooldown up to a cap, and a successful request clears the node's error streak. This keeps an unstable Mac from being chosen first over and over while still letting it recover automatically.

Responses include dispatch metadata:

- `X-Mi-Dispatch-Attempts`
- `X-Mi-Node-Id`
- `X-Mi-Provider-Id`

For streaming responses, those values are sent as HTTP trailers.

Admin node snapshots expose:

- `error_streak`
- `cooldown_until`
- `last_error`
- `in_cooldown`

## Next milestones

- Persistent node identity and API-key scoped node enrollment.
- TLS or mTLS for LAN/VPN deployments.
- MLX backend.
- Model aliases and model registry config.
- Dashboard with node health, loaded models, queue depth, TTFT, and tokens/s.
- LaunchAgent installer for macOS nodes.
- Prometheus metrics.
- Retry on a second node when failure happens before first token.
