# Embeddings

`mi` exposes an OpenAI-compatible embeddings endpoint:

```http
POST /v1/embeddings
Authorization: Bearer <consumer-api-key>
Content-Type: application/json
```

Requests use a model ID from `/v1/models` or a configured model alias. `input`
may be either one JSON string or an array of strings:

```json
{
  "model": "nomic-embed-text",
  "input": ["first document", "second document"],
  "privacy_tier": "private"
}
```

```json
{
  "model": "nomic-embed-text",
  "input": "one document"
}
```

The response follows the OpenAI list shape, with one `embedding` item per input:

```json
{
  "object": "list",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.12, -0.04, 0.33]
    }
  ],
  "model": "nomic-embed-text",
  "usage": {
    "prompt_tokens": 3,
    "completion_tokens": 0,
    "total_tokens": 3
  }
}
```

Embeddings use the same consumer accounts, quotas, provider accounts, privacy
tiers, model aliases, scheduler routing, and settlement ledger as chat. Usage is
coordinator-estimated from input text length; node-reported token counts are not
trusted for quota or settlement accounting.

For Ollama-backed nodes, the node-agent calls Ollama's `/api/embed` endpoint
with the selected model and input strings. The echo backend also implements
deterministic 8-dimensional embeddings for demos and tests, so the full
coordinator and WebSocket path can be exercised without Ollama.
