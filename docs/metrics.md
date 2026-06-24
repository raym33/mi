# Metrics

`GET /admin/metrics` exports coordinator metrics in the Prometheus text exposition format.

The endpoint is protected by the admin bearer token. Prometheus scrapes must include that token, for example with `bearer_token` or `bearer_token_file`.

```bash
curl http://localhost:8080/admin/metrics \
  -H 'Authorization: Bearer admin-dev-token'
```

Example Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: mi-coordinator
    metrics_path: /admin/metrics
    bearer_token: admin-dev-token
    static_configs:
      - targets: ["localhost:8080"]
```

Label values are escaped for the Prometheus text format. The endpoint uses conservative labels only: `node_id`, `provider_id`, `backend`, and `device_kind`. Empty `backend` and `device_kind` labels are omitted.

## Metrics

| Metric | Type | Labels | Description |
| --- | --- | --- | --- |
| `mi_nodes` | gauge | none | Registered nodes. |
| `mi_healthy_nodes` | gauge | none | Healthy nodes available for routing. |
| `mi_cooldown_nodes` | gauge | none | Nodes currently in cooldown. |
| `mi_active_requests` | gauge | none | Active inference requests across healthy nodes. |
| `mi_available_slots` | gauge | none | Available request slots across healthy nodes. |
| `mi_max_concurrent` | gauge | none | Maximum concurrent request capacity across healthy nodes. |
| `mi_total_memory_free_mb` | gauge | none | Total free memory advertised by healthy nodes in megabytes. |
| `mi_average_latency_ms` | gauge | none | Average observed latency across healthy nodes in milliseconds. |
| `mi_average_ttft_ms` | gauge | none | Average observed time to first token across healthy nodes in milliseconds. |
| `mi_average_tokens_per_second` | gauge | none | Average observed output tokens per second across healthy nodes. |
| `mi_node_healthy` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Node health state, `1` for healthy and `0` for unhealthy. |
| `mi_node_active` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Active requests on the node. |
| `mi_node_max_concurrent` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Maximum concurrent requests accepted by the node. |
| `mi_node_completed_requests_total` | counter | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Completed requests observed for the node. |
| `mi_node_failed_requests_total` | counter | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Failed requests observed for the node. |
| `mi_node_observed_latency_ms` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Node observed latency in milliseconds. |
| `mi_node_observed_ttft_ms` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Node observed time to first token in milliseconds. |
| `mi_node_observed_tokens_per_second` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Node observed output tokens per second. |
| `mi_node_provider_score` | gauge | `node_id`, `provider_id`, optional `backend`, optional `device_kind` | Provider reputation score applied to the node. |
| `mi_settlement_events_total` | counter | none | Settlement events recorded by the coordinator. |
| `mi_provider_reward_micros` | gauge | `provider_id` | Provider reward balance in micros. |
| `mi_provider_penalty_micros` | gauge | `provider_id` | Provider penalty balance in micros. |
| `mi_provider_total_tokens` | gauge | `provider_id` | Provider token balance from settlement accounting. |
| `mi_provider_events_total` | counter | `provider_id` | Settlement events recorded for the provider. |
| `mi_provider_challenges_total` | counter | `provider_id` | Challenges recorded for the provider. |
| `mi_provider_challenges_passed_total` | counter | `provider_id` | Challenges passed by the provider. |
| `mi_provider_challenges_failed_total` | counter | `provider_id` | Challenges failed by the provider. |
| `mi_provider_challenge_pass_rate_bps` | gauge | `provider_id` | Provider challenge pass rate in basis points. |
