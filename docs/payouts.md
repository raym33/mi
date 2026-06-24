# Provider Payout CSV

`GET /admin/payouts.csv` exports all-time provider payout accounting as CSV.

This endpoint is protected by the admin bearer token and is intended for operator review, invoices, or off-platform payouts. It is cooperative accounting, not a trustless payment rail. The coordinator records local settlement events and exports balances derived from those events; operators still need backups, review, dispute handling, and external payment processes for real payouts.

There are no settlement periods yet, so the export is all-time.

## Columns

| Column | Meaning |
| --- | --- |
| `provider_id` | Provider account id from the settlement balance. |
| `display_name` | Provider display name from city state, when available. |
| `events` | Number of settlement events credited to the provider. |
| `total_tokens` | Total accounted tokens across credited events. |
| `avg_latency_ms` | Average recorded latency in milliseconds. |
| `reward_micros` | Provider reward in settlement micros. |
| `penalty_micros` | SLA penalty in settlement micros. |

Micros are the settlement unit. For example, `1000000` micros equals one whole accounting unit in whatever off-platform currency or credit system the operator maps settlement to.

## Example

```bash
curl http://localhost:8080/admin/payouts.csv \
  -H 'Authorization: Bearer admin-dev-token' \
  -o mi-payouts.csv
```
