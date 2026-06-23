# Contributing

Thanks for your interest in `mi`. The project is early, and thoughtful contributions can shape the product.

## Principles

- Keep the system local-first.
- Prefer simple deployment over clever infrastructure.
- Do not log prompt bodies by default.
- Treat privacy claims carefully and honestly.
- Keep OpenAI-compatible APIs easy to use.
- Make provider and consumer operations understandable to non-experts.

## Good First Contributions

- Improve setup docs for a second Mac.
- Add examples for Tailscale, LAN, or coworking deployments.
- Add more smoke tests.
- Improve error messages.
- Add dashboard mockups or API designs.
- Test with more Ollama models.
- Document real-world performance on different Macs.

## Larger Contributions

- Prometheus metrics.
- SQLite-backed city state.
- Pricing and payout ledger.
- macOS LaunchAgent installer.
- MLX backend.
- Provider reputation scoring.
- Admin dashboard.
- Security hardening.

## Development Setup

Install dependencies:

```bash
brew install go ollama
ollama serve
ollama pull llama3.1:8b
```

Run tests:

```bash
make test
```

Build:

```bash
make build
```

Run local smoke tests:

```bash
make smoke
make city-smoke
```

## Pull Requests

Before opening a pull request:

- Run `go fmt ./...`.
- Run `go test ./...`.
- Keep the change focused.
- Add tests for scheduler, security, accounting, or protocol behavior.
- Update docs when behavior or configuration changes.

In the pull request, include:

- What changed.
- Why it matters.
- How it was tested.
- Any privacy, security, or compatibility implications.

## Security

Please do not open public issues for exploitable vulnerabilities. If you find a serious security issue, contact the maintainer privately first.

Security-sensitive areas include:

- Provider authentication.
- Consumer API keys.
- TLS and mTLS behavior.
- Privacy tier enforcement.
- Usage accounting and quotas.
- Any future payment or payout logic.

## Code Style

- Follow the existing Go style.
- Keep package boundaries small and boring.
- Prefer deterministic tests.
- Avoid adding dependencies unless they clearly reduce operational complexity.
- Keep logs free of prompt bodies, API keys, provider tokens, and private data.
