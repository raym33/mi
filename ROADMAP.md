# Roadmap

`mi` is moving from a local Mac inference demo toward a practical local compute network for teams, communities, and eventually mixed edge fleets.

The roadmap is intentionally split into what works now, what should come next for adoption, and what belongs later for larger or more adversarial networks.

## Working Now

API and control plane:

- OpenAI-compatible chat completions.
- Streaming responses.
- `/v1/models`, `/v1/models/catalog`, and `/v1/me`.
- Coordinator plus outbound node-agent architecture.
- Ollama backend.
- Model aliases and model catalog.
- Node WebSocket protocol with version fields.

Scheduling:

- Model-aware routing.
- Capacity and queue-aware routing.
- Privacy-tier routing.
- Optional backend, device, SoC, and accelerator hints.
- Failover before first token.
- Cooldowns for unstable nodes.
- Coordinator-observed latency, TTFT, tokens/sec, and failure-rate routing signals.
- Provider reputation-aware routing.

City network:

- Consumer and provider accounts.
- API keys and provider tokens.
- Dynamic enrollment.
- Key and token rotation.
- Account disable operations.
- Consumer quotas.
- Quota reservations for concurrent requests.
- Coordinator-estimated usage accounting.
- Default output caps when clients omit `max_tokens`.
- SQLite/WAL-backed city state.
- Legacy JSON state support.

Settlement and integrity:

- Hash-chained settlement events for consumer debits and provider rewards.
- SQLite/WAL-backed settlement ledger.
- Legacy JSONL settlement chain support.
- SLA latency penalties.
- Tamper-evident benchmark challenge events.
- Optional synthetic challenge runner.
- Per-provider challenge rotation.
- Combined settlement and challenge integrity manifest.
- External anchoring path through `/admin/integrity`.
- Minimal built-in admin dashboard for operators.

Security and privacy:

- Admin bearer token.
- HTTPS/WSS example.
- Node mTLS example.
- Coordinator-enforced privacy tiers: `private`, `community`, `public`.
- Public rented providers cannot self-promote into private routing.
- Prompt bodies are excluded from settlement and challenge records.

Heterogeneous groundwork:

- Node backend abstraction.
- Hardware metadata.
- Request capability hints.
- Design docs for Android, Xiaomi, Snapdragon, QNN, LiteRT, CUDA, Linux, Windows, and MLX paths.

## Next: Make It Operable

These are the highest-leverage product steps for a real pilot:

- Deeper admin dashboard workflows for enrollment, payout review, and challenge operations.
- Prometheus metrics for requests, tokens, latency, TTFT, throughput, errors, cooldowns, and provider usage.
- Uptime windows and p95 latency windows in scheduler scoring.
- One-command provider enrollment links or QR codes.
- macOS LaunchAgent installer for always-on nodes.
- Operator docs for Tailscale, LAN, and coworking deployments.
- Provider payout reports and CSV invoice exports.
- Pricing rules per model, provider, privacy tier, and latency class.
- Exact model-family tokenizers for accounting.
- Better challenge sets and challenge dispute review.

## Next: Make It Safer For Money

Before real payouts or broad rented capacity:

- Periodic external anchoring helper for `/admin/integrity`.
- Signed provider receipts.
- Settlement periods with open/closed states.
- Dispute windows.
- Provider reliability thresholds before payout eligibility.
- Wallet-address metadata without automatic payment execution.
- Audit export bundles.
- Backup and restore documentation.

## Later: More Backends And Fleets

- MLX-native backend for Apple Silicon.
- llama.cpp backend.
- Linux CUDA/vLLM node profile.
- Windows Snapdragon node profile.
- Android node app.
- Snapdragon QNN backend.
- LiteRT backend.
- XRing/Vulkan experiments.
- Platform-aware model aliases.
- Backend-specific challenge scoring.

## Later: Larger Networks

- Postgres storage option for larger multi-coordinator deployments.
- Request admission control and fair queueing.
- Federation between trusted city networks.
- Hosted coordinator option.
- Staking, slashing, and automated payouts.
- Stronger private-compute options using TEEs, isolation, redaction, or split/encrypted inference research.

## Open Questions

- What is the right pricing model: token-based, time-based, latency-based, or hybrid?
- How should payouts account for model size, energy cost, latency, and reliability?
- Which backend should become the preferred production backend after Ollama?
- What trust model is acceptable for schools, coworking spaces, agencies, clinics, and local governments?
- How much should stay local-first versus becoming a hosted control plane?
- What minimum security bar is required before public rented capacity is enabled by default?
