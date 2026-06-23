# Roadmap

This roadmap is intentionally practical. `mi` should become the simplest way to pool local Mac inference, run private team AI, and rent non-sensitive local compute inside trusted communities.

## Now

- OpenAI-compatible chat completions.
- Coordinator and outbound node-agent architecture.
- Ollama backend.
- Model aliases and model catalog.
- City accounts for consumers and providers.
- API keys, provider tokens, quotas, rotation, and revocation.
- Quota reservations for concurrent requests.
- Hash-chained settlement events for consumer debits and provider rewards.
- Persistent local usage ledger.
- HTTPS/WSS and node mTLS.
- Scheduler failover before first token.
- Cooldowns for unstable nodes.
- Coordinator-enforced privacy tiers for private, community, and public rented compute.

## Next

- Prometheus metrics for requests, tokens, nodes, errors, cooldowns, and provider usage.
- Pricing rules per model, provider, and privacy tier.
- Provider payout reports and invoice exports.
- Optional on-chain anchoring of settlement hashes.
- Provider reputation based on uptime, error rate, latency, and completed tokens.
- One-command provider enrollment.
- macOS LaunchAgent installer for always-on nodes.
- Better admin and operator documentation.
- SQLite storage for larger city networks.

## Later

- MLX-native backend for better Apple Silicon performance.
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
