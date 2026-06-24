# Roadmap

This roadmap is intentionally practical. `mi` should become the simplest way to pool local Apple Silicon and ARM edge inference, run private team AI, and rent non-sensitive local compute inside trusted communities.

## Now

- OpenAI-compatible chat completions.
- Coordinator and outbound node-agent architecture.
- Ollama backend.
- Model aliases and model catalog.
- City accounts for consumers and providers.
- API keys, provider tokens, quotas, rotation, and revocation.
- Quota reservations for concurrent requests.
- Hash-chained settlement events for consumer debits and provider rewards.
- Provider reputation from node health, cooldowns, completed events, tokens, and rewards.
- Reputation-aware provider routing.
- Coordinator-observed latency, TTFT, tokens/sec, and failure-rate routing signals.
- Tamper-evident benchmark challenge events feeding provider reputation.
- Optional synthetic benchmark runner for automatic provider evidence.
- Coordinator-estimated usage accounting instead of worker-reported token billing.
- Per-provider challenge rotation so quiet nodes are tested regularly.
- Combined settlement and challenge integrity manifest for external anchoring.
- Persistent local usage ledger.
- HTTPS/WSS and node mTLS.
- WebSocket protocol version fields for gradual node upgrades.
- Scheduler failover before first token.
- Cooldowns for unstable nodes.
- Coordinator-enforced privacy tiers for private, community, and public rented compute.
- Node backend abstraction and hardware metadata for future Android, Snapdragon, Xiaomi, MLX, QNN, and LiteRT support.
- Optional backend, device, SoC, and accelerator hints for heterogeneous routing.

## Next

- Prometheus metrics for requests, tokens, nodes, errors, cooldowns, and provider usage.
- P95 latency windows and uptime history in scheduler scoring.
- Pricing rules per model, provider, and privacy tier.
- Model-family tokenizers for exact coordinator-side accounting.
- Provider payout reports and invoice exports.
- Optional on-chain anchoring transaction helper.
- Benchmark-driven reputation, challenge jobs, and slashing/dispute flows.
- Challenge pass/fail dispute review workflow.
- One-command provider enrollment.
- macOS LaunchAgent installer for always-on nodes.
- Better admin and operator documentation.
- SQLite storage for larger city networks.
- Android/Xiaomi node-agent design and prototype.

## Later

- MLX-native backend for better Apple Silicon performance.
- QNN/LiteRT/llama.cpp-Vulkan Android backends for Snapdragon and Xiaomi devices.
- Dashboard for node health, model availability, usage, and earnings.
- Request admission control and fair queueing.
- Multi-model routing policies.
- Signed provider attestations.
- Optional prompt redaction pipeline.
- Stronger private-compute options using isolation, confidential compute where available, or split/encrypted inference research.
- Federation between trusted city networks.

## Open Questions

- What is the right default pricing model: token-based, time-based, or hybrid?
- How should provider payouts account for model size, latency, energy cost, and reliability?
- What trust model is acceptable for schools, coworking spaces, and small businesses?
- Which local inference backend should become the preferred production backend after Ollama?
- How much of the marketplace should stay local-first versus becoming a hosted control plane?
