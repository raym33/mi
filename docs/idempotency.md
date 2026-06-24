# Request Idempotency

`mi` supports request idempotency keys on the OpenAI-compatible non-streaming chat endpoint:

```bash
curl http://localhost:8080/v1/chat/completions \
  -H 'Authorization: Bearer sk-mi-studio-a-dev' \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: retry-2026-06-24-001' \
  -d '{
    "model": "fast",
    "stream": false,
    "messages": [{"role": "user", "content": "Summarize this note"}]
  }'
```

The key is scoped by consumer account. The same raw key from two different consumers is treated as two independent idempotency records.

## Scope

Idempotency applies only to `POST /v1/chat/completions` with `stream=false` or no `stream` field.

Streaming requests are not idempotency-safe in this implementation. A request with both `stream=true` and an `Idempotency-Key` header returns HTTP 400:

```json
{
  "error": {
    "message": "idempotency keys require stream=false",
    "type": "invalid_request"
  }
}
```

## Replay Behavior

For a new key, the coordinator claims the key before reserving quota or dispatching to a node. If dispatch fails, the key is aborted so the client can retry.

After a successful non-streaming response, the coordinator stores the exact response bytes. A later request with the same consumer and `Idempotency-Key` returns the stored response with:

```http
X-Mi-Idempotent-Replay: true
```

Replay does not reserve quota, dispatch to a node, record usage, or create a second settlement event. This prevents double charge on client retries for the same key.

If another request with the same key is still running, the coordinator returns HTTP 409:

```json
{
  "error": {
    "message": "a request with this Idempotency-Key is already in progress",
    "type": "idempotency_conflict"
  }
}
```

In-progress records older than the configured TTL are treated as stale and may be claimed by a retry.

## Configuration

Idempotency is disabled by default. Enable it with a SQLite/WAL store:

```yaml
idempotency:
  sqlite_path: "data/mi-idempotency.db"
  ttl: "24h"
```

`ttl` uses the same duration syntax as other coordinator durations. If `ttl` is omitted or non-positive, the coordinator uses `24h`.

## Guarantees

With idempotency enabled, a successful charge happens at most once per `(consumer, Idempotency-Key)` for non-streaming chat requests.

This stores the completion response separately from the city usage and settlement ledgers. Full crash-atomicity across idempotency response storage, usage accounting, and settlement recording is separate future work.
