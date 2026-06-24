# Contributing

Thanks for your interest in `mi`. The project is early enough that focused contributions can still shape both the product and the architecture.

## Project Direction

`mi` should become the simplest way to pool local inference capacity across trusted machines first, then rented community capacity later.

The near-term target is not a public token network. It is a usable local inference network for teams, small businesses, schools, coworking spaces, and city groups.

## Principles

- Keep the system local-first.
- Prefer simple deployment over clever infrastructure.
- Preserve OpenAI-compatible client ergonomics.
- Do not log prompt bodies by default.
- Treat privacy claims carefully and narrowly.
- Make provider and consumer operations understandable to non-experts.
- Separate implemented behavior from roadmap language.
- Add tests for accounting, scheduling, security, and protocol behavior.

## Good First Contributions

- Improve setup docs for a second Mac.
- Add deployment examples for Tailscale, LAN, or coworking spaces.
- Add smoke tests.
- Improve error messages.
- Improve admin endpoint examples.
- Test with more Ollama models.
- Document real-world performance on different Macs.
- Add sample scripts for backups and integrity anchoring.

## High-Impact Contributions

- Admin dashboard.
- Prometheus metrics.
- Provider enrollment links.
- macOS LaunchAgent installer.
- Pricing rules.
- Provider payout and invoice exports.
- Model-family tokenizers.
- Better challenge suites.
- Backup and restore tooling.
- MLX backend.
- llama.cpp backend.
- Android agent prototype.
- Security reviews.

## Development Setup

Install dependencies:

```bash
brew install go ollama
ollama serve
ollama pull llama3.1:8b
```

Use Go 1.25 or newer.

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

Run the race detector before larger concurrency or scheduler changes:

```bash
go test -race ./...
```

## Pull Requests

Before opening a pull request:

- Run `go fmt ./...`.
- Run `go test ./...`.
- Run targeted tests for the packages you changed.
- Keep the change focused.
- Add or update tests when behavior changes.
- Update docs when configuration, security posture, API shape, or operator behavior changes.

In the pull request, include:

- What changed.
- Why it matters.
- How it was tested.
- Any privacy, security, accounting, or compatibility implications.

## Security

Please do not open public issues for exploitable vulnerabilities. If you find a serious security issue, contact the maintainer privately first.

Security-sensitive areas include:

- Provider authentication.
- Consumer API keys.
- Admin authorization.
- TLS and mTLS behavior.
- Privacy tier enforcement.
- Usage accounting and quotas.
- Settlement integrity.
- Challenge reputation.
- Any future payment or payout logic.

## Code Style

- Follow the existing Go style.
- Keep package boundaries small and clear.
- Prefer deterministic tests.
- Avoid adding dependencies unless they clearly reduce operational complexity.
- Keep logs free of prompt bodies, API keys, provider tokens, and private data.
- Use structured parsers and APIs for structured data.
